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

type ArgoOverrideFile struct {
	Helm struct {
		Parameters []ArgoParameterOverride `yaml:"parameters"`
	} `yaml:"helm"`
}

type ArgoParameterOverride struct {
	Name        string `yaml:"name"`
	Value       string `yaml:"value"`
	ForceString bool   `yaml:"forceString"`
}

type GitRepo struct {
	RepoURL       string
	BranchName    string
	Path          string
	FileName      string
	localRepo     *git.Repository
	sshAuth       *ssh.PublicKeys
	gitConfig     *GitConfig
	GitHandler    GitHandler
	repoCachePath string
	localRepoPath string
}

// getRepoCachePath generates a unique, deterministic local path for the repository cache using FNV-1a.
func (repo *GitRepo) getRepoCachePath() string {
	hasher := fnv.New64a()
	// The Write method on hash.Hash never returns an error.
	_, _ = io.WriteString(hasher, fmt.Sprintf("%s-%s", repo.RepoURL, repo.BranchName))
	hashUint64 := hasher.Sum64()
	return filepath.Join(repo.repoCachePath, strconv.FormatUint(hashUint64, 16))
}

// Clone handles the git clone operation with caching.
// If the repository is already cached locally, it fetches the latest changes.
// Otherwise, it clones the repository from the remote URL.
func (repo *GitRepo) Clone() error {
	var err error

	// Generate a unique path for the repository in the cache
	repo.localRepoPath = repo.getRepoCachePath()

	// Prepare SSH authentication
	if repo.sshAuth, err = repo.GitHandler.AddSSHKey("git", repo.gitConfig.SshKeyPath, repo.gitConfig.SshKeyPass); err != nil {
		return err
	}

	// Check if the repository is already cached
	if _, err = os.Stat(repo.localRepoPath); os.IsNotExist(err) {
		// Not cached, clone it
		log.Debug().Msgf("Cloning repository %s into %s", repo.RepoURL, repo.localRepoPath)
		repo.localRepo, err = repo.GitHandler.PlainClone(repo.localRepoPath, false, &git.CloneOptions{
			URL:           repo.RepoURL,
			ReferenceName: plumbing.ReferenceName("refs/heads/" + repo.BranchName),
			SingleBranch:  true,
			Auth:          repo.sshAuth,
		})
		return err
	}

	// Cached, open it
	log.Debug().Msgf("Repository %s already cached at %s", repo.RepoURL, repo.localRepoPath)
	repo.localRepo, err = git.PlainOpen(repo.localRepoPath)
	if err != nil {
		return fmt.Errorf("failed to open cached repo: %w", err)
	}

	// Fetch the latest changes
	err = repo.localRepo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		Auth:       repo.sshAuth,
		Force:      true,
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("failed to fetch repo: %w", err)
	}

	// Reset to the latest version of the branch
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

// NukeAndReclone removes the local cache and clones the repository again.
func (repo *GitRepo) NukeAndReclone() error {
	log.Debug().Msgf("Nuking cache for %s at %s", repo.RepoURL, repo.localRepoPath)
	if err := os.RemoveAll(repo.localRepoPath); err != nil {
		return fmt.Errorf("failed to remove local cache directory: %w", err)
	}
	return repo.Clone()
}

func (repo *GitRepo) generateOverrideFileName(appName string) string {
	if repo.FileName == "" {
		return fmt.Sprintf("%s/.argocd-source-%s.yaml", repo.Path, appName)
	}
	return fmt.Sprintf("%s/%s", repo.Path, repo.FileName)
}

func (repo *GitRepo) generateCommitMessage(appName string, tmplData any) (string, error) {
	commitMsg := fmt.Sprintf("argo-watcher(%s): update image tag", appName)

	if repo.gitConfig.CommitMessageFormat == "" {
		return commitMsg, nil
	}

	tmpl, err := template.New("commitMsg").Parse(repo.gitConfig.CommitMessageFormat)
	if err != nil {
		return commitMsg, err
	}

	var message bytes.Buffer
	if err = tmpl.Execute(&message, tmplData); err != nil {
		return commitMsg, err
	}

	return message.String(), nil
}

// UpdateApp is the main entry point for updating the application manifest.
// It orchestrates the process of merging, committing, and pushing changes.
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

func (repo *GitRepo) mergeOverrideFileContent(fullPath string, overrideContent *ArgoOverrideFile) (*ArgoOverrideFile, error) {
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return overrideContent, nil
	}

	existingContent, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read existing override file: %w", err)
	}

	existingOverrideFile := ArgoOverrideFile{}
	if err := yaml.Unmarshal(existingContent, &existingOverrideFile); err != nil {
		return nil, fmt.Errorf("failed to unmarshal existing override file: %w", err)
	}

	mergeParameters(&existingOverrideFile, overrideContent)

	return &existingOverrideFile, nil
}

// commitAndPush handles the git commit and push operations.
func (repo *GitRepo) commitAndPush(fullPath, commitMsg string, overrideContent *ArgoOverrideFile) error {
	worktree, err := repo.localRepo.Worktree()
	if err != nil {
		return err
	}

	// Write the final content to the override file
	contentBytes, err := yaml.Marshal(overrideContent)
	if err != nil {
		return err
	}
	if err := os.WriteFile(fullPath, contentBytes, 0644); err != nil {
		return fmt.Errorf("failed to write override file: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return fmt.Errorf("failed to get worktree status: %w", err)
	}

	if status.IsClean() {
		log.Debug().Msg("No changes detected. Skipping commit.")
		return nil
	}

	// Add the file to the worktree
	relativePath, err := filepath.Rel(repo.localRepoPath, fullPath)
	if err != nil {
		return err
	}
	if _, err := worktree.Add(relativePath); err != nil {
		return err
	}

	// Commit the changes
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

	// Push the changes
	pushOpts := &git.PushOptions{
		Auth:       repo.sshAuth,
		RemoteName: "origin",
	}
	if err = repo.localRepo.Push(pushOpts); err != nil {
		// The error is returned to be handled by the caller, which will trigger the recovery path.
		return err
	}

	return nil
}

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

func NewGitRepo(repoURL, branchName, path, fileName, repoCachePath string, gitHandler GitHandler) *GitRepo {
	gitConfig, err := NewGitConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load git config")
	}

	return &GitRepo{
		RepoURL:       repoURL,
		BranchName:    branchName,
		Path:          path,
		FileName:      fileName,
		gitConfig:     gitConfig,
		GitHandler:    gitHandler,
		repoCachePath: repoCachePath,
	}
}
