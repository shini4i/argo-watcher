// Package updater provides the core logic for interacting with Git repositories.
// This includes cloning, caching, updating, and pushing changes to application manifests.
// It is designed to be safe for concurrent use, with locking mechanisms handled upstream
// and a robust caching strategy to ensure efficiency and resilience.
package updater

import (
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"text/template"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/rs/zerolog/log"
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
func (repo *GitRepo) Clone() error {
	var err error

	repo.localRepoPath = repo.getRepoCachePath()

	if repo.sshAuth, err = repo.GitHandler.AddSSHKey("git", repo.gitConfig.SshKeyPath, repo.gitConfig.SshKeyPass); err != nil {
		return err
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
			log.Debug().Msgf("No cache found for repo %s at %s. Cloning fresh.", repo.RepoURL, repo.localRepoPath)
		} else {
			log.Warn().Msgf("Cached repo at %s is invalid or missing remote (%s). Re-cloning.", repo.localRepoPath, err)
			if err := os.RemoveAll(repo.localRepoPath); err != nil {
				return fmt.Errorf("failed to remove invalid cache directory: %w", err)
			}
		}

		// Perform the fresh clone.
		repo.localRepo, err = repo.GitHandler.PlainClone(repo.localRepoPath, false, &git.CloneOptions{
			URL:           repo.RepoURL,
			ReferenceName: plumbing.ReferenceName("refs/heads/" + repo.BranchName),
			SingleBranch:  true,
			Auth:          repo.sshAuth,
		})
		return err
	}

	// If we get here, the cache is valid and has an 'origin' remote.
	log.Debug().Msgf("Successfully opened cached repository at %s", repo.localRepoPath)
	err = repo.localRepo.Fetch(&git.FetchOptions{
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

// NukeAndReclone provides a recovery mechanism by completely deleting the local
// cache directory and then re-cloning the repository from scratch. This is
// typically called after a non-fast-forward push error.
func (repo *GitRepo) NukeAndReclone() error {
	log.Debug().Msgf("Nuking cache for %s at %s", repo.RepoURL, repo.localRepoPath)
	if err := os.RemoveAll(repo.localRepoPath); err != nil {
		return fmt.Errorf("failed to remove local cache directory: %w", err)
	}
	return repo.Clone()
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
// user-configurable Go template if provided; otherwise, it falls back to a default format.
func (repo *GitRepo) generateCommitMessage(appName string, tmplData any) (string, error) {
	commitMsg := fmt.Sprintf("argo-watcher(%s): update image tag", appName)

	if repo.gitConfig.CommitMessageFormat == "" {
		return commitMsg, nil
	}

	tmpl, err := template.New("commitMsg").Parse(repo.gitConfig.CommitMessageFormat)
	if err != nil {
		// Fallback to default message on template parse error.
		return commitMsg, err
	}

	var message bytes.Buffer
	if err = tmpl.Execute(&message, tmplData); err != nil {
		// Fallback to default message on template execute error.
		return commitMsg, err
	}

	return message.String(), nil
}

// UpdateApp is the main entry point for updating an application's manifest file.
// It orchestrates merging the new content with any existing override file,
// and then handles the git commit and push operations.
func (repo *GitRepo) UpdateApp(appName string, overrideContent *ArgoOverrideFile, tmplData any) error {
	overrideFileName := repo.generateOverrideFileName(appName)
	fullPath := filepath.Join(repo.localRepoPath, overrideFileName)

	commitMsg, err := repo.generateCommitMessage(appName, tmplData)
	if err != nil {
		return err
	}

	log.Debug().Msgf("Updating override file: %s", fullPath)

	finalContent, err := repo.mergeOverrideFileContent(fullPath, overrideContent)
	if err != nil {
		return err
	}

	if err := repo.commitAndPush(fullPath, commitMsg, finalContent); err != nil {
		return err
	}

	return nil
}

// mergeOverrideFileContent reads an existing override file (if one exists) and
// merges the new parameter overrides into it. If no file exists, it returns the
// new content directly.
func (repo *GitRepo) mergeOverrideFileContent(fullPath string, overrideContent *ArgoOverrideFile) (*ArgoOverrideFile, error) {
	// If the file doesn't exist, there's nothing to merge.
	existingContent, err := os.ReadFile(fullPath)
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
// committing, and pushing to the remote. If the working tree is clean after
// writing the file, it skips the commit and push.
func (repo *GitRepo) commitAndPush(fullPath, commitMsg string, overrideContent *ArgoOverrideFile) error {
	worktree, err := repo.localRepo.Worktree()
	if err != nil {
		return err
	}

	// Write the final merged content to the override file.
	contentBytes, err := yaml.Marshal(overrideContent)
	if err != nil {
		return err
	}
	if err := os.WriteFile(fullPath, contentBytes, 0644); err != nil { // #nosec G306
		return fmt.Errorf("failed to write override file: %w", err)
	}

	// Check if writing the file actually resulted in changes.
	status, err := worktree.Status()
	if err != nil {
		return fmt.Errorf("failed to get worktree status: %w", err)
	}

	// If the image tag is already correct, there will be no changes.
	if status.IsClean() {
		log.Debug().Msg("No changes detected. Skipping commit.")
		return nil
	}

	// Add the file to the staging area.
	relativePath, err := filepath.Rel(repo.localRepoPath, fullPath)
	if err != nil {
		// This is a programmatic error, should not happen in practice.
		return fmt.Errorf("could not determine relative path: %w", err)
	}
	if _, err := worktree.Add(relativePath); err != nil {
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

	// Push the changes to the remote repository.
	pushOpts := &git.PushOptions{
		Auth:       repo.sshAuth,
		RemoteName: "origin",
	}
	if err = repo.localRepo.Push(pushOpts); err != nil {
		// The error is intentionally returned to be handled by the caller,
		// which will trigger the recovery path (nuke and re-clone).
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
