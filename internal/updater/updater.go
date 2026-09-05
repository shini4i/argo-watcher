// Package updater clones, caches, updates and pushes application manifests in Git.
// Concurrent write-back to one repository must be serialised by the caller; that
// locking lives upstream, not in this package.
package updater

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"gopkg.in/yaml.v3"
)

// ArgoOverrideFile represents the structure of the Helm parameter override file
// that is committed to the GitOps repository.
type ArgoOverrideFile struct {
	Helm struct {
		Parameters []ArgoParameterOverride `yaml:"parameters"`
	} `yaml:"helm"`
}

// ArgoParameterOverride defines a single Helm parameter to be overridden.
type ArgoParameterOverride struct {
	Name        string `yaml:"name"`
	Value       string `yaml:"value"`
	ForceString bool   `yaml:"forceString"`
}

// GitRepo holds the state for operations on a single Git repository branch.
type GitRepo struct {
	// RepoURL is the SSH URL of the repository to be cloned.
	RepoURL    string
	BranchName string
	// Path is the directory within the repository where the manifest file is located.
	Path          string
	FileName      string
	repoCachePath string
	// localRepoPath is the full path to the cached clone of this specific repository and branch.
	localRepoPath string
	localRepo     *git.Repository
	sshAuth       *ssh.PublicKeys
	// gitConfig contains user-configurable git settings like commit author and email.
	gitConfig  *GitConfig
	GitHandler GitHandler
}

// getRepoCachePath generates a unique, deterministic local path for the repository cache.
// Hashing URL+branch gives filesystem-level isolation, so concurrent operations on
// different branches of the same repository do not conflict.
func (repo *GitRepo) getRepoCachePath() string {
	hasher := fnv.New64a()
	// The Write method on hash.Hash is documented to never return an error.
	_, _ = io.WriteString(hasher, fmt.Sprintf("%s-%s", repo.RepoURL, repo.BranchName))
	hashUint64 := hasher.Sum64()
	return filepath.Join(repo.repoCachePath, strconv.FormatUint(hashUint64, 16))
}

// Clone handles the initial setup of the local repository cache. A cache that opens
// cleanly and has an "origin" remote is fetched and hard-reset to the remote branch
// HEAD, so the worktree is always clean and at origin on return; anything else
// (missing, corrupt, no "origin") is re-cloned from scratch rather than repaired.
//
// commitLocal's byte-compare skip depends on that reset: it treats equal bytes as
// "no change", which only holds because this function leaves the worktree at origin.
//
// Both the fresh clone and the warm-cache fetch are shallow (Depth:1, no tags):
// argo-watcher only reads the branch tip and commits one file on top of it, so
// the repository's history is never needed. This keeps the clone/fetch cost off
// the deep-history path, which matters because the whole operation runs under
// the distributed per-repo advisory lock.
//
// The provided ctx bounds all network I/O; callers typically derive ctx from a
// total-budget context for the whole update flow so that one stuck operation
// cannot hold the per-repo lock past that budget.
func (repo *GitRepo) Clone(ctx context.Context) error {
	var err error

	repo.localRepoPath = repo.getRepoCachePath()

	// Only load the SSH key once per GitRepo lifetime; the key does not change
	// between calls and re-reading it on the race-recovery Clone() wastes budget.
	if repo.sshAuth == nil {
		if repo.sshAuth, err = repo.GitHandler.AddSSHKey("git", repo.gitConfig.SshKeyPath, repo.gitConfig.SshKeyPass); err != nil {
			return err
		}
	}

	repo.localRepo, err = repo.GitHandler.PlainOpen(repo.localRepoPath)
	if err == nil {
		_, err = repo.localRepo.Remote("origin")
	}

	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			slog.Debug("No cache found for repo, cloning fresh", "repo", repo.RepoURL, "path", repo.localRepoPath)
		} else {
			slog.Warn("Cached repo is invalid or missing remote, re-cloning", "path", repo.localRepoPath, "error", err)
			if err := os.RemoveAll(repo.localRepoPath); err != nil {
				return fmt.Errorf("failed to remove invalid cache directory: %w", err)
			}
		}

		repo.localRepo, err = repo.GitHandler.PlainClone(ctx, repo.localRepoPath, false, &git.CloneOptions{
			URL:           repo.RepoURL,
			ReferenceName: plumbing.ReferenceName("refs/heads/" + repo.BranchName),
			SingleBranch:  true,
			Depth:         1,
			Tags:          git.NoTags,
			Auth:          repo.sshAuth,
		})
		return err
	}

	slog.Debug("Successfully opened cached repository", "path", repo.localRepoPath)
	// Keep the fetch shallow too; otherwise go-git deepens the clone toward full
	// history on the first fetch, undoing the shallow clone's win.
	err = repo.localRepo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		Auth:       repo.sshAuth,
		Force:      true,
		Depth:      1,
		Tags:       git.NoTags,
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("failed to fetch repo: %w", err)
	}

	worktree, err := repo.localRepo.Worktree()
	if err != nil {
		return err
	}

	remoteRef, err := repo.localRepo.Reference(plumbing.NewRemoteReferenceName("origin", repo.BranchName), true)
	if err != nil {
		return fmt.Errorf("failed to get remote reference: %w", err)
	}

	return worktree.Reset(&git.ResetOptions{
		Commit: remoteRef.Hash(),
		Mode:   git.HardReset,
	})
}

// generateOverrideFileNameForApp builds the override file path from an explicit
// path and fileName. If fileName is empty a default name is derived from the
// application name; otherwise fileName is used verbatim. The path and fileName
// are passed explicitly (rather than read from the GitRepo) because in batch
// write-back many apps share one clone yet each has its own write-back location.
func generateOverrideFileNameForApp(path, fileName, appName string) string {
	if fileName == "" {
		return fmt.Sprintf("%s/.argocd-source-%s.yaml", path, appName)
	}
	return fmt.Sprintf("%s/%s", path, fileName)
}

// generateCommitMessage creates the commit message for the update. It uses a
// user-configurable Go template if provided; otherwise, it falls back to a
// default format. Template errors (parse or execute) are logged and the default
// message is used so a malformed COMMIT_MESSAGE_FORMAT does not abort the
// deployment update — availability takes precedence over a custom commit message.
func (repo *GitRepo) generateCommitMessage(appName string, tmplData any) string {
	commitMsg := fmt.Sprintf("argo-watcher(%s): update image tag", appName)

	if repo.gitConfig.CommitMessageFormat == "" {
		return commitMsg
	}

	tmpl, err := template.New("commitMsg").Parse(repo.gitConfig.CommitMessageFormat)
	if err != nil {
		slog.Warn("COMMIT_MESSAGE_FORMAT parse error; using default commit message", "error", err)
		return commitMsg
	}

	var message bytes.Buffer
	if err = tmpl.Execute(&message, tmplData); err != nil {
		slog.Warn("COMMIT_MESSAGE_FORMAT execute error; using default commit message", "error", err)
		return commitMsg
	}

	return message.String()
}

// UpdateApp is the main entry point for updating an application's manifest file.
// It merges the new content with any existing override file, commits the change
// locally, and pushes it. The provided ctx bounds the push. It is the single-app
// path; batch write-back instead calls CommitAppLocal for each app and Push once.
func (repo *GitRepo) UpdateApp(ctx context.Context, appName string, overrideContent *ArgoOverrideFile, tmplData any) error {
	committed, err := repo.CommitAppLocal(appName, repo.Path, repo.FileName, overrideContent, tmplData)
	if err != nil {
		return err
	}
	if !committed {
		return nil
	}
	return repo.push(ctx)
}

// CommitAppLocal merges an app's override file with the new content and commits
// the change into the local clone WITHOUT pushing. It reports whether a commit
// was actually created (false when the on-disk content already matches, so there
// is nothing to push). path and fileName are the app's write-back location; they
// are passed explicitly because in batch write-back many apps share one clone yet
// each has its own location. tmplData is forwarded to the commit-message template.
func (repo *GitRepo) CommitAppLocal(appName, path, fileName string, overrideContent *ArgoOverrideFile, tmplData any) (bool, error) {
	overrideFileName := generateOverrideFileNameForApp(path, fileName, appName)
	fullPath := filepath.Join(repo.localRepoPath, overrideFileName)

	if err := assertInsideRoot(repo.localRepoPath, fullPath); err != nil {
		return false, err
	}

	commitMsg := repo.generateCommitMessage(appName, tmplData)

	slog.Debug("Updating override file", "path", fullPath)

	finalContent, err := repo.mergeOverrideFileContent(fullPath, overrideContent)
	if err != nil {
		return false, err
	}

	return repo.commitLocal(fullPath, commitMsg, finalContent)
}

// Push publishes all locally-committed changes to the remote, bounded by ctx.
// In batch write-back it is called once after every app in the batch has been
// committed via CommitAppLocal, collapsing N pushes into one.
func (repo *GitRepo) Push(ctx context.Context) error {
	return repo.push(ctx)
}

// assertInsideRoot returns an error when path is not within root. It protects
// against path-traversal attacks where an operator-supplied annotation value
// (write-back-path, write-back-filename) contains ".." segments that would
// escape the cloned repository directory.
func assertInsideRoot(root, path string) error {
	rel, err := filepath.Rel(root, path)
	// rel == "." means path equals root exactly — the override file must live
	// inside the root, not at the root itself.
	// rel == ".." or rel beginning with ".."+separator indicates a parent-directory
	// escape. A plain HasPrefix(rel, "..") would falsely flag legitimate paths whose
	// first component happens to start with ".." (e.g. "..foo/file.yaml").
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q is not inside repository root %q", path, root)
	}
	return nil
}

// mergeOverrideFileContent renders the bytes to write for an app's override file,
// merging overrideContent into whatever is already there. The document is edited
// as a yaml.Node so that sibling keys and comments survive: the target may be
// Argo CD's own .argocd-source-<app>.yaml, or a file write-back-filename names.
func (repo *GitRepo) mergeOverrideFileContent(fullPath string, overrideContent *ArgoOverrideFile) ([]byte, error) {
	existingContent, err := os.ReadFile(fullPath) // #nosec G304 -- path already validated by assertInsideRoot in UpdateApp
	if err != nil {
		if os.IsNotExist(err) {
			return yaml.Marshal(overrideContent)
		}
		return nil, fmt.Errorf("failed to read existing override file: %w", err)
	}

	document, err := singleDocument(existingContent)
	if err != nil {
		return nil, fmt.Errorf("cannot update override file %s: %w", fullPath, err)
	}

	// An empty, comment-only or null file decodes to nothing worth preserving.
	if document == nil || len(document.Content) == 0 || isNull(document.Content[0]) {
		return yaml.Marshal(overrideContent)
	}

	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("existing override file %s is not a YAML mapping", fullPath)
	}

	parameters, err := parametersNode(root)
	if err != nil {
		return nil, err
	}

	existingOverrideFile := ArgoOverrideFile{}
	if err := parameters.Decode(&existingOverrideFile.Helm.Parameters); err != nil {
		return nil, fmt.Errorf("failed to unmarshal existing override file: %w", err)
	}

	mergeParameters(&existingOverrideFile, overrideContent)

	if err := parameters.Encode(existingOverrideFile.Helm.Parameters); err != nil {
		return nil, fmt.Errorf("failed to render helm parameters: %w", err)
	}

	return yaml.Marshal(document)
}

// singleDocument decodes the one YAML document an override file may hold, or nil
// when it holds none. A stream of several is refused: marshalling a Node emits
// only the first, so editing such a file would drop the rest of it.
func singleDocument(content []byte) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))

	var first yaml.Node
	if err := decoder.Decode(&first); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to unmarshal existing override file: %w", err)
	}

	var second yaml.Node
	if err := decoder.Decode(&second); err == nil {
		return nil, fmt.Errorf("it holds more than one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to unmarshal existing override file: %w", err)
	}

	return &first, nil
}

// isNull reports whether a node is an explicit or implicit YAML null.
func isNull(node *yaml.Node) bool {
	return node == nil || node.Tag == "!!null"
}

// parametersNode returns the helm.parameters node of an override document,
// creating the helm mapping and the parameters key when the file has neither.
func parametersNode(root *yaml.Node) (*yaml.Node, error) {
	helm, err := childMapping(root, "helm")
	if err != nil {
		return nil, err
	}

	parameters := childValue(helm, "parameters")
	if parameters == nil {
		helm.Content = append(helm.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "parameters"},
			&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"},
		)
		parameters = helm.Content[len(helm.Content)-1]
	}

	// A null "parameters:" decodes to an empty slice and re-encodes as a sequence.
	if parameters.Kind != yaml.SequenceNode && parameters.Tag != "!!null" {
		return nil, fmt.Errorf("helm.parameters is not a YAML sequence")
	}

	return parameters, nil
}

// childMapping returns the mapping stored under key, adding an empty one when the
// key is absent. A key holding anything else is an error: overwriting it would
// discard whatever the operator put there.
func childMapping(parent *yaml.Node, key string) (*yaml.Node, error) {
	if existing := childValue(parent, key); existing != nil {
		// A bare "helm:" carries no value yet; it becomes the mapping it was
		// always going to hold.
		if isNull(existing) {
			*existing = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			return existing, nil
		}
		if existing.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s is not a YAML mapping", key)
		}
		return existing, nil
	}

	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"},
	)

	return parent.Content[len(parent.Content)-1], nil
}

// childValue returns the value node stored under key in a mapping, or nil. A
// mapping's Content alternates key, value.
func childValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}

	return nil
}

func (repo *GitRepo) commitLocal(fullPath, commitMsg string, contentBytes []byte) (bool, error) {
	worktree, err := repo.localRepo.Worktree()
	if err != nil {
		return false, err
	}

	// Detect "nothing to commit" with a single-file byte compare instead of
	// worktree.Status(). Clone() hard-resets to origin HEAD and this override file is
	// the only path we write, so equal bytes mean a clean worktree — but O(1 file)
	// instead of scanning the whole repo, which dominates the cost on a large repo.
	// #nosec G304 -- path already validated by assertInsideRoot in CommitAppLocal
	if existing, readErr := os.ReadFile(fullPath); readErr == nil && bytes.Equal(existing, contentBytes) {
		slog.Debug("No changes detected. Skipping commit.")
		return false, nil
	}

	if err := os.WriteFile(fullPath, contentBytes, 0600); err != nil {
		return false, fmt.Errorf("failed to write override file: %w", err)
	}

	// Add the file to the staging area. SkipStatus avoids the full-worktree Status()
	// scan that worktree.Add performs internally; with an explicit single-file path
	// go-git hashes and stages only that file (new or modified).
	relativePath, err := filepath.Rel(repo.localRepoPath, fullPath)
	if err != nil {
		return false, fmt.Errorf("could not determine relative path: %w", err)
	}
	if err := worktree.AddWithOptions(&git.AddOptions{Path: relativePath, SkipStatus: true}); err != nil {
		return false, err
	}

	commitOpts := &git.CommitOptions{
		Author: &object.Signature{
			Name:  repo.gitConfig.SshCommitUser,
			Email: repo.gitConfig.SshCommitMail,
			When:  time.Now(),
		},
	}
	if _, err = worktree.Commit(commitMsg, commitOpts); err != nil {
		return false, err
	}

	return true, nil
}

// push publishes local commits to the remote, bounded by ctx. Any error is
// returned as-is; the retry loop in the caller decides whether to retry. It does
// not classify push-race vs other failures because the retry loop treats all
// transient errors uniformly.
//
// Note: a local commit created before this call becomes an orphan if the budget
// check below fails. That is intentional and safe — the next Clone hard-resets to
// origin, discarding the orphan before the retry attempt builds a fresh commit on
// top of the refreshed tip. In batch write-back the whole batch's commits are
// discarded and re-applied together on retry.
func (repo *GitRepo) push(ctx context.Context) error {
	// Bail early if the budget is already exhausted — no point issuing a push
	// that is guaranteed to fail with "context deadline exceeded".
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("budget exhausted before push: %w", err)
	}

	pushOpts := &git.PushOptions{
		Auth:       repo.sshAuth,
		RemoteName: "origin",
	}
	return repo.localRepo.PushContext(ctx, pushOpts)
}

func mergeParameters(existing, newContent *ArgoOverrideFile) {
	for _, newParam := range newContent.Helm.Parameters {
		found := false
		for idx, existingParam := range existing.Helm.Parameters {
			if existingParam.Name == newParam.Name {
				existing.Helm.Parameters[idx] = newParam
				found = true
				break
			}
		}
		if !found {
			existing.Helm.Parameters = append(existing.Helm.Parameters, newParam)
		}
	}
}

// NewGitRepo constructs a GitRepo, loading the git configuration from the environment.
func NewGitRepo(repoURL, branchName, path, fileName, repoCachePath string, gitHandler GitHandler) (*GitRepo, error) {
	gitConfig, err := NewGitConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load git config: %w", err)
	}

	return &GitRepo{
		RepoURL:       repoURL,
		BranchName:    branchName,
		Path:          path,
		FileName:      fileName,
		gitConfig:     gitConfig,
		GitHandler:    gitHandler,
		repoCachePath: repoCachePath,
	}, nil
}

// GitOpTimeout returns the per-attempt wall-clock budget for one clone+update
// cycle so callers can build bounded contexts without seeing credentials in
// the rest of GitConfig. The full retry loop's worst-case wall clock is
// GitOpTimeout * GitMaxAttempts.
func (repo *GitRepo) GitOpTimeout() time.Duration {
	return repo.gitConfig.GitOpTimeout
}

// GitMaxAttempts returns the total number of attempts the caller's retry loop
// should make before giving up. The final attempt is expected to invalidate
// the cache via InvalidateCache so a poisoned cache self-heals.
func (repo *GitRepo) GitMaxAttempts() uint {
	return repo.gitConfig.GitMaxAttempts
}

// InvalidateCache removes the on-disk cache for this repository and clears
// the in-memory git handle. The next call to Clone will fall through to a
// fresh PlainClone because PlainOpen will fail with ErrRepositoryNotExists.
//
// Intended use: called by the retry loop before its final attempt so a
// poisoned cache (a partial commit left from a prior failure, a stale ref,
// a mid-write filesystem state) cannot keep failing forever. Calling this
// when localRepoPath is empty (i.e. before the first Clone) is a safe no-op.
//
// Note: sshAuth is NOT cleared because the SSH key file is not expected to
// change during the lifetime of a GitRepo. A key rotation requires a restart.
func (repo *GitRepo) InvalidateCache() error {
	repo.localRepo = nil
	if repo.localRepoPath == "" {
		return nil
	}
	// Guard against removing something outside the designated cache base. This
	// should not happen in normal operation (localRepoPath is always set by
	// getRepoCachePath under repoCachePath), but an explicit check prevents
	// catastrophic damage if the struct is somehow misused. Use assertInsideRoot
	// so a trailing separator on repoCachePath (operator-supplied via env var)
	// does not produce a false rejection from naive string-prefix matching.
	if err := assertInsideRoot(repo.repoCachePath, repo.localRepoPath); err != nil {
		return fmt.Errorf("localRepoPath %q is not inside repoCachePath %q; refusing to remove", repo.localRepoPath, repo.repoCachePath)
	}
	return os.RemoveAll(repo.localRepoPath)
}
