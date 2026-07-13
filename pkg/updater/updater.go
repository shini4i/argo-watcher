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
			slog.Debug(fmt.Sprintf("No cache found for repo %s at %s. Cloning fresh.", repo.RepoURL, repo.localRepoPath))
		} else {
			slog.Warn(fmt.Sprintf("Cached repo at %s is invalid or missing remote (%s). Re-cloning.", repo.localRepoPath, err))
			if err := os.RemoveAll(repo.localRepoPath); err != nil {
				return fmt.Errorf("failed to remove invalid cache directory: %w", err)
			}
		}

		repo.localRepo, err = repo.GitHandler.PlainClone(ctx, repo.localRepoPath, false, &git.CloneOptions{
			URL:           repo.RepoURL,
			ReferenceName: plumbing.ReferenceName("refs/heads/" + repo.BranchName),
			SingleBranch:  true,
			Auth:          repo.sshAuth,
		})
		return err
	}

	// If we get here, the cache is valid and has an 'origin' remote.
	slog.Debug(fmt.Sprintf("Successfully opened cached repository at %s", repo.localRepoPath))
	err = repo.localRepo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		Auth:       repo.sshAuth,
		Force:      true,
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

// generateOverrideFileName determines the name of the override file. If a specific
// FileName is provided in the configuration, it is used; otherwise, a default name
// is generated based on the application name.
func (repo *GitRepo) generateOverrideFileName(appName string) string {
	if repo.FileName == "" {
		return fmt.Sprintf("%s/.argocd-source-%s.yaml", repo.Path, appName)
	}
	return fmt.Sprintf("%s/%s", repo.Path, repo.FileName)
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
// It orchestrates merging the new content with any existing override file,
// and then handles the git commit and push operations. The provided ctx bounds
// the push.
func (repo *GitRepo) UpdateApp(ctx context.Context, appName string, overrideContent *ArgoOverrideFile, tmplData any) error {
	overrideFileName := repo.generateOverrideFileName(appName)
	fullPath := filepath.Join(repo.localRepoPath, overrideFileName)

	if err := assertInsideRoot(repo.localRepoPath, fullPath); err != nil {
		return err
	}

	commitMsg := repo.generateCommitMessage(appName, tmplData)

	slog.Debug(fmt.Sprintf("Updating override file: %s", fullPath))

	finalContent, err := repo.mergeOverrideFileContent(fullPath, overrideContent)
	if err != nil {
		return err
	}

	if err := repo.commitAndPush(ctx, fullPath, commitMsg, finalContent); err != nil {
		return err
	}

	return nil
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
	// If the file doesn't exist, there's nothing to merge.
	existingContent, err := os.ReadFile(fullPath) // #nosec G304 -- path already validated by assertInsideRoot in UpdateApp
	if err != nil {
		if os.IsNotExist(err) {
			return overrideContent, nil
		}
		return nil, fmt.Errorf("failed to read existing override file: %w", err)
	}

	// Unmarshal the YAML content into a struct.
	existingOverrideFile := ArgoOverrideFile{}
	if err := yaml.Unmarshal(existingContent, &existingOverrideFile); err != nil {
		return nil, fmt.Errorf("failed to unmarshal existing override file: %w", err)
	}

	// Merge the new parameters into the existing ones.
	mergeParameters(&existingOverrideFile, overrideContent)

	return &existingOverrideFile, nil
}

// commitAndPush handles the git workflow: writing changes, adding to stage,
// committing, and pushing to the remote. If the override file already contains
// exactly the content to be written, it skips the commit and push. ctx bounds
// the push.
func (repo *GitRepo) commitAndPush(ctx context.Context, fullPath, commitMsg string, overrideContent *ArgoOverrideFile) error {
	worktree, err := repo.localRepo.Worktree()
	if err != nil {
		return err
	}

	// Marshal the final merged content.
	contentBytes, err := yaml.Marshal(overrideContent)
	if err != nil {
		return err
	}

	// Detect "nothing to commit" with a single-file byte compare instead of a
	// full-worktree scan. Clone() hard-resets the worktree to origin HEAD before every
	// attempt, and this override file is the only path argo-watcher ever writes, so the
	// on-disk bytes equalling what we are about to write is equivalent to
	// worktree.Status().IsClean() for our purposes — but O(1 file) instead of O(entire
	// repo). On a large GitOps repo that whole-tree scan dominates the per-task cost, and
	// it is paid serially for every concurrent task holding the per-repo lock in turn.
	// #nosec G304 -- path already validated by assertInsideRoot in UpdateApp
	if existing, readErr := os.ReadFile(fullPath); readErr == nil && bytes.Equal(existing, contentBytes) {
		slog.Debug("No changes detected. Skipping commit.")
		return nil
	}

	// Write the final merged content to the override file.
	if err := os.WriteFile(fullPath, contentBytes, 0600); err != nil {
		return fmt.Errorf("failed to write override file: %w", err)
	}

	// Add the file to the staging area. SkipStatus avoids the full-worktree Status()
	// scan that worktree.Add performs internally; with an explicit single-file path
	// go-git hashes and stages only that file (new or modified).
	relativePath, err := filepath.Rel(repo.localRepoPath, fullPath)
	if err != nil {
		// This is a programmatic error, should not happen in practice.
		return fmt.Errorf("could not determine relative path: %w", err)
	}
	if err := worktree.AddWithOptions(&git.AddOptions{Path: relativePath, SkipStatus: true}); err != nil {
		return err
	}

	// Commit the changes with the configured author details.
	commitOpts := &git.CommitOptions{
		Author: &object.Signature{
			Name:  repo.gitConfig.SshCommitUser,
			Email: repo.gitConfig.SshCommitMail,
			When:  time.Now(),
		},
	}
	if _, err = worktree.Commit(commitMsg, commitOpts); err != nil {
		return err
	}

	// Bail early if the budget is already exhausted — no point issuing a push
	// that is guaranteed to fail with "context deadline exceeded".
	// Note: the local commit created above becomes an orphan if we return here.
	// That is intentional and safe: the next Clone call hard-resets to origin,
	// discarding the orphan before the retry attempt builds a new commit on top
	// of the refreshed tip.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("budget exhausted before push: %w", err)
	}

	// Push the changes to the remote repository, bounded by the caller's ctx.
	// Any error is returned as-is; the retry loop in the caller decides whether
	// to retry. We deliberately do not classify push-race vs other failures here
	// because the retry loop treats all transient errors uniformly.
	pushOpts := &git.PushOptions{
		Auth:       repo.sshAuth,
		RemoteName: "origin",
	}
	if err = repo.localRepo.PushContext(ctx, pushOpts); err != nil {
		return err
	}

	return nil
}

// mergeParameters updates the `existing` parameters with values from `newContent`.
// If a parameter from `newContent` already exists by name in `existing`, it is overwritten.
// If it does not exist, it is appended.
func mergeParameters(existing *ArgoOverrideFile, newContent *ArgoOverrideFile) {
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
