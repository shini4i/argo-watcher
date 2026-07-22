// Package updater provides the core logic for interacting with Git repositories.
// This includes cloning, caching, updating, and pushing changes to application manifests.
// It is designed to be safe for concurrent use, with locking mechanisms handled upstream
// and a robust caching strategy to ensure efficiency and resilience.
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

// GitRepo encapsulates all the necessary information and state for performing
// operations on a single Git repository branch.
type GitRepo struct {
	// RepoURL is the SSH URL of the repository to be cloned.
	RepoURL string
	// BranchName is the target branch for all operations.
	BranchName string
	// Path is the directory within the repository where the manifest file is located.
	Path string
	// FileName is the name of the override file to be updated.
	FileName string
	// repoCachePath is the base directory on the local filesystem for storing cached repositories.
	repoCachePath string
	// localRepoPath is the full path to the cached clone of this specific repository and branch.
	localRepoPath string
	// localRepo is the go-git object representing the repository on disk.
	localRepo *git.Repository
	// sshAuth holds the SSH authentication method.
	sshAuth *ssh.PublicKeys
	// gitConfig contains user-configurable git settings like commit author and email.
	gitConfig *GitConfig
	// GitHandler is an interface for git operations, allowing for easier testing and mocking.
	GitHandler GitHandler
}

// getRepoCachePath generates a unique, deterministic local path for the repository cache.
// It uses an FNV-1a hash of the combined repository URL and branch name to ensure that
// concurrent operations on different branches of the same repository do not conflict.
// This approach provides filesystem-level isolation.
func (repo *GitRepo) getRepoCachePath() string {
	hasher := fnv.New64a()
	// The Write method on hash.Hash is documented to never return an error.
	_, _ = io.WriteString(hasher, fmt.Sprintf("%s-%s", repo.RepoURL, repo.BranchName))
	hashUint64 := hasher.Sum64()
	return filepath.Join(repo.repoCachePath, strconv.FormatUint(hashUint64, 16))
}

// Clone handles the initial setup of the local repository cache.
//
// The logic is designed to be resilient against incomplete or corrupt clones. It first attempts
// to open an existing repository at the expected cache path.
//   - If opening succeeds, it means a valid cache exists. The function then proceeds to fetch the
//     latest changes from the remote and performs a hard reset to ensure the working directory
//     is clean and up-to-date with the remote branch's HEAD.
//   - If opening fails (for any reason, including the directory not existing or being corrupt),
//     the function assumes the cache is invalid. It safely removes the directory and then
//     performs a fresh, single-branch clone from the remote.
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
		// If open succeeded, ensure the 'origin' remote exists.
		_, err = repo.localRepo.Remote("origin")
	}

	// Handle the case where the cache is invalid or does not exist.
	if err != nil {
		// Differentiate between a simple "not found" and a real corruption issue for logging.
		if errors.Is(err, git.ErrRepositoryNotExists) {
			slog.Debug("No cache found for repo, cloning fresh", "repo", repo.RepoURL, "path", repo.localRepoPath)
		} else {
			slog.Warn("Cached repo is invalid or missing remote, re-cloning", "path", repo.localRepoPath, "error", err)
			if err := os.RemoveAll(repo.localRepoPath); err != nil {
				return fmt.Errorf("failed to remove invalid cache directory: %w", err)
			}
		}

		// Shallow, single-branch, no tags — see the Clone doc comment for why.
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

	// If we get here, the cache is valid and has an 'origin' remote.
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

// mergeOverrideFileContent reads an existing override file (if one exists) and
// merges the new parameter overrides into it. If no file exists, it returns the
// new content directly.
func (repo *GitRepo) mergeOverrideFileContent(fullPath string, overrideContent *ArgoOverrideFile) (*ArgoOverrideFile, error) {
	existingContent, err := os.ReadFile(fullPath) // #nosec G304 -- path already validated by assertInsideRoot in UpdateApp
	if err != nil {
		if os.IsNotExist(err) {
			return overrideContent, nil
		}
		return nil, fmt.Errorf("failed to read existing override file: %w", err)
	}

	existingOverrideFile := ArgoOverrideFile{}
	if err := yaml.Unmarshal(existingContent, &existingOverrideFile); err != nil {
		return nil, fmt.Errorf("failed to unmarshal existing override file: %w", err)
	}

	mergeParameters(&existingOverrideFile, overrideContent)

	return &existingOverrideFile, nil
}

// commitLocal writes the override file, stages it, and creates a local commit.
// It reports whether a commit was actually created: if the file already contains
// exactly the content to be written, it skips the commit and returns (false, nil).
func (repo *GitRepo) commitLocal(fullPath, commitMsg string, overrideContent *ArgoOverrideFile) (bool, error) {
	worktree, err := repo.localRepo.Worktree()
	if err != nil {
		return false, err
	}

	contentBytes, err := yaml.Marshal(overrideContent)
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
		// This is a programmatic error, should not happen in practice.
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

// mergeParameters updates the `existing` parameters with values from `newContent`.
// If a parameter from `newContent` already exists by name in `existing`, it is overwritten.
// If it does not exist, it is appended.
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

// NewGitRepo is the constructor for the GitRepo struct. It initializes the struct
// with all necessary information for git operations, including loading the git configuration.
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
