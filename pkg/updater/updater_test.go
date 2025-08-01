package updater

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/shini4i/argo-watcher/pkg/updater/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newTestRepo creates a valid GitRepo instance for testing purposes.
func newTestRepo(t *testing.T, handler GitHandler) *GitRepo {
	t.Helper()
	t.Setenv("SSH_KEY_PATH", "/dev/null")

	repo, err := NewGitRepo("fake-url", "main", "apps", "values.yaml", t.TempDir(), handler)
	require.NoError(t, err)
	require.NotNil(t, repo)
	return repo
}

func TestNewGitRepo(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/dev/null")
		repo, err := NewGitRepo("url", "branch", "path", "file", "cache", &GitClient{})
		assert.NoError(t, err)
		assert.NotNil(t, repo)
	})

	t.Run("Failure", func(t *testing.T) {
		repo, err := NewGitRepo("url", "branch", "path", "file", "cache", &GitClient{})
		assert.Error(t, err)
		assert.Nil(t, repo)
	})
}

func TestGetRepoCachePath(t *testing.T) {
	repo := newTestRepo(t, nil)
	path1 := repo.getRepoCachePath()
	path2 := repo.getRepoCachePath()
	assert.Equal(t, path1, path2, "Cache path should be deterministic")
	assert.NotEmpty(t, path1)

	repo.RepoURL = "another-url"
	path3 := repo.getRepoCachePath()
	assert.NotEqual(t, path1, path3, "Different repo URL should produce a different path")
}

func TestGenerateOverrideFileName(t *testing.T) {
	repo := newTestRepo(t, nil)
	repo.FileName = "custom.yaml"
	assert.Equal(t, "apps/custom.yaml", repo.generateOverrideFileName("my-app"))

	repo.FileName = ""
	assert.Equal(t, "apps/.argocd-source-my-app.yaml", repo.generateOverrideFileName("my-app"))
}

func TestGenerateCommitMessage(t *testing.T) {
	repo := newTestRepo(t, nil)
	tmplData := struct{ AppName string }{AppName: "test-app"}

	repo.gitConfig.CommitMessageFormat = ""
	msg, err := repo.generateCommitMessage("test-app", tmplData)
	assert.NoError(t, err)
	assert.Equal(t, "argo-watcher(test-app): update image tag", msg)

	repo.gitConfig.CommitMessageFormat = "ci: bump {{ .AppName }}"
	msg, err = repo.generateCommitMessage("test-app", tmplData)
	assert.NoError(t, err)
	assert.Equal(t, "ci: bump test-app", msg)

	repo.gitConfig.CommitMessageFormat = "ci: bump {{ .AppName "
	msg, err = repo.generateCommitMessage("test-app", tmplData)
	assert.Error(t, err)
	assert.Equal(t, "argo-watcher(test-app): update image tag", msg, "Should fallback to default on parse error")

	repo.gitConfig.CommitMessageFormat = "ci: bump {{ .MissingKey }}"
	msg, err = repo.generateCommitMessage("test-app", tmplData)
	assert.Error(t, err)
	assert.Equal(t, "argo-watcher(test-app): update image tag", msg, "Should fallback to default on execute error")
}

func TestMergeParameters(t *testing.T) {
	existing := &ArgoOverrideFile{}
	existing.Helm.Parameters = []ArgoParameterOverride{
		{Name: "image.tag", Value: "v1.0.0"},
		{Name: "replicaCount", Value: "1"},
	}

	newContent := &ArgoOverrideFile{}
	newContent.Helm.Parameters = []ArgoParameterOverride{
		{Name: "image.tag", Value: "v2.0.0"},
		{Name: "debug", Value: "true"},
	}

	mergeParameters(existing, newContent)

	assert.Len(t, existing.Helm.Parameters, 3)
	assert.Contains(t, existing.Helm.Parameters, ArgoParameterOverride{Name: "image.tag", Value: "v2.0.0"})
	assert.Contains(t, existing.Helm.Parameters, ArgoParameterOverride{Name: "replicaCount", Value: "1"})
	assert.Contains(t, existing.Helm.Parameters, ArgoParameterOverride{Name: "debug", Value: "true"})
}

func TestClone(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockHandler := mock.NewMockGitHandler(ctrl)
	repo := newTestRepo(t, mockHandler)

	mockHandler.EXPECT().AddSSHKey(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	t.Run("Cache Not Exists", func(t *testing.T) {
		mockHandler.EXPECT().PlainOpen(gomock.Any()).Return(nil, git.ErrRepositoryNotExists)
		mockHandler.EXPECT().PlainClone(gomock.Any(), false, gomock.Any()).Return(nil, nil)
		err := repo.Clone()
		assert.NoError(t, err)
	})

	t.Run("Cache Invalid", func(t *testing.T) {
		repo.localRepoPath = repo.getRepoCachePath()
		require.NoError(t, os.WriteFile(repo.localRepoPath, []byte("garbage"), 0600))

		mockHandler.EXPECT().PlainOpen(gomock.Any()).Return(nil, errors.New("invalid repo"))
		mockHandler.EXPECT().PlainClone(gomock.Any(), false, gomock.Any()).Return(nil, nil)
		err := repo.Clone()
		assert.NoError(t, err)
		_, err = os.Stat(repo.localRepoPath)
		assert.True(t, os.IsNotExist(err), "Corrupted cache should have been removed")
	})

	t.Run("Cache Exists and is Valid", func(t *testing.T) {
		memStore := memory.NewStorage()
		r, err := git.Init(memStore, nil)
		require.NoError(t, err)
		_, err = r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{"dummy-url"}})
		require.NoError(t, err)

		mockHandler.EXPECT().PlainOpen(gomock.Any()).Return(r, nil)

		err = repo.Clone()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch repo: repository not found")
	})
}

func TestNukeAndReclone(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockHandler := mock.NewMockGitHandler(ctrl)
	repo := newTestRepo(t, mockHandler)
	repo.localRepoPath = repo.getRepoCachePath()

	require.NoError(t, os.MkdirAll(repo.localRepoPath, 0755))
	dummyFile := filepath.Join(repo.localRepoPath, "test.txt")
	require.NoError(t, os.WriteFile(dummyFile, []byte("test"), 0644))

	mockHandler.EXPECT().AddSSHKey(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	mockHandler.EXPECT().PlainOpen(gomock.Any()).Return(nil, git.ErrRepositoryNotExists)
	mockHandler.EXPECT().PlainClone(gomock.Any(), false, gomock.Any()).Return(nil, nil)

	err := repo.NukeAndReclone()
	assert.NoError(t, err)

	_, err = os.Stat(repo.localRepoPath)
	assert.True(t, os.IsNotExist(err), "Cache directory should be removed")
}

// setupGitForTest is a helper to create a clean git environment for each sub-test.
func setupGitForTest(t *testing.T) (sourceRepo, remoteRepo, localRepo *git.Repository, remotePath, localPath string) {
	t.Helper()

	// 1. Create a NON-BARE repository first, which will be the source.
	sourcePath := t.TempDir()
	sourceRepo, err := git.PlainInit(sourcePath, false)
	require.NoError(t, err)

	// 2. Create an initial commit in the source repository so it's not empty.
	sourceWorktree, err := sourceRepo.Worktree()
	require.NoError(t, err)
	_, err = sourceWorktree.Commit("initial commit", &git.CommitOptions{
		Author:            &object.Signature{Name: "Initial", Email: "initial@test.com", When: time.Now()},
		AllowEmptyCommits: true,
	})
	require.NoError(t, err)

	// 3. Create a BARE repository to act as the "origin" remote, by cloning the source.
	remotePath = t.TempDir()
	remoteRepo, err = git.PlainClone(remotePath, true, &git.CloneOptions{
		URL: sourcePath,
	})
	require.NoError(t, err)

	// 4. Create the final "local" clone that our code will operate on.
	localPath = t.TempDir()
	localRepo, err = git.PlainClone(localPath, false, &git.CloneOptions{
		URL: remotePath,
	})
	require.NoError(t, err)

	return sourceRepo, remoteRepo, localRepo, remotePath, localPath
}

func TestFullUpdateAppCycle(t *testing.T) {
	t.Run("Success - With Changes", func(t *testing.T) {
		_, remoteRepo, localRepo, _, localPath := setupGitForTest(t)

		repo := newTestRepo(t, &GitClient{})
		repo.localRepo = localRepo
		repo.localRepoPath = localPath
		appDir := filepath.Join(localPath, "apps")
		require.NoError(t, os.Mkdir(appDir, 0755))
		valuesFile := filepath.Join(appDir, "values.yaml")

		newParams := &ArgoOverrideFile{}
		newParams.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "v2.0.0"}}

		err := repo.UpdateApp("my-app", newParams, nil)
		require.NoError(t, err)

		content, err := os.ReadFile(valuesFile)
		require.NoError(t, err)
		assert.Contains(t, string(content), "v2.0.0")

		head, err := remoteRepo.Head()
		require.NoError(t, err)
		commit, err := remoteRepo.CommitObject(head.Hash())
		require.NoError(t, err)
		assert.Contains(t, commit.Message, "argo-watcher(my-app): update image tag")
	})

	t.Run("Success - No Changes", func(t *testing.T) {
		_, _, localRepo, _, localPath := setupGitForTest(t)

		repo := newTestRepo(t, &GitClient{})
		repo.localRepo = localRepo
		repo.localRepoPath = localPath
		appDir := filepath.Join(localPath, "apps")
		require.NoError(t, os.Mkdir(appDir, 0755))

		// First, perform a successful change.
		initialParams := &ArgoOverrideFile{}
		initialParams.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "v2.0.0"}}
		err := repo.UpdateApp("my-app", initialParams, nil)
		require.NoError(t, err)

		// Now, try again with the same content.
		headBefore, err := localRepo.Head()
		require.NoError(t, err)

		err = repo.UpdateApp("my-app", initialParams, nil)
		require.NoError(t, err)

		headAfter, err := localRepo.Head()
		require.NoError(t, err)
		assert.Equal(t, headBefore.Hash(), headAfter.Hash())
	})

	t.Run("Failure - Push Fails due to Non-Fast-Forward", func(t *testing.T) {
		sourceRepo, _, localRepo, remotePath, localPath := setupGitForTest(t)

		repo := newTestRepo(t, &GitClient{})
		repo.localRepo = localRepo
		repo.localRepoPath = localPath
		appDir := filepath.Join(localPath, "apps")
		require.NoError(t, os.Mkdir(appDir, 0755))

		// Get the worktree from the non-bare source repo.
		sourceWorktree, err := sourceRepo.Worktree()
		require.NoError(t, err)

		// Add the bare repository as a remote to the source repo.
		_, err = sourceRepo.CreateRemote(&config.RemoteConfig{
			Name: "origin",
			URLs: []string{remotePath},
		})
		require.NoError(t, err)

		// Commit a conflicting change to the source repo.
		_, err = sourceWorktree.Commit("a conflicting commit", &git.CommitOptions{
			Author:            &object.Signature{Name: "Other", Email: "other@test.com", When: time.Now()},
			AllowEmptyCommits: true,
		})
		require.NoError(t, err)

		// Push the conflicting commit from source to the bare remote.
		err = sourceRepo.Push(&git.PushOptions{})
		require.NoError(t, err)

		// Now, try to update our local repo, which will fail on push.
		newParams := &ArgoOverrideFile{}
		newParams.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "v3.0.0"}}

		err = repo.UpdateApp("my-app", newParams, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "non-fast-forward update")
	})
}

func TestMergeOverrideFileContent(t *testing.T) {
	repo := newTestRepo(t, nil)
	newContent := &ArgoOverrideFile{}
	newContent.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "new"}}

	t.Run("File Not Exist", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "values.yaml")
		finalContent, err := repo.mergeOverrideFileContent(tmpFile, newContent)
		require.NoError(t, err)
		assert.Equal(t, newContent, finalContent)
	})

	t.Run("File Read Fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := repo.mergeOverrideFileContent(tmpDir, newContent)
		assert.Error(t, err)
	})

	t.Run("File Unmarshal Fails", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "values.yaml")
		require.NoError(t, os.WriteFile(tmpFile, []byte("key: value: invalid"), 0644))
		_, err := repo.mergeOverrideFileContent(tmpFile, newContent)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal")
	})
}

func TestCommitAndPush_WriteFileError(t *testing.T) {
	localPath := t.TempDir()
	r, err := git.PlainInit(localPath, false)
	require.NoError(t, err)

	repo := newTestRepo(t, nil)
	repo.localRepo = r
	repo.localRepoPath = localPath

	fullPath := filepath.Join(localPath, "apps")
	require.NoError(t, os.Mkdir(fullPath, 0755))

	err = repo.commitAndPush(fullPath, "msg", &ArgoOverrideFile{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write override file")
}

func TestGitClient_Coverage(t *testing.T) {
	// 1. Create a source repository with a commit, so it's not empty.
	sourcePath := t.TempDir()
	sourceRepo, err := git.PlainInit(sourcePath, false)
	require.NoError(t, err)
	wt, err := sourceRepo.Worktree()
	require.NoError(t, err)
	_, err = wt.Commit("init", &git.CommitOptions{
		Author:            &object.Signature{Name: "test", Email: "test", When: time.Now()},
		AllowEmptyCommits: true,
	})
	require.NoError(t, err)

	// 2. Create an instance of the concrete client.
	client := GitClient{}

	// 3. Execute and cover PlainClone.
	clonePath := t.TempDir()
	_, err = client.PlainClone(clonePath, false, &git.CloneOptions{URL: sourcePath})
	require.NoError(t, err, "PlainClone method failed")

	// 4. Execute and cover PlainOpen on the new clone.
	_, err = client.PlainOpen(clonePath)
	require.NoError(t, err, "PlainOpen method failed")

	// 5. Execute and cover AddSSHKey.
	// We expect an error because it's not a real key, but the call will cover the line.
	keyFile := filepath.Join(t.TempDir(), "dummy_key")
	err = os.WriteFile(keyFile, []byte("not-a-real-key"), 0600)
	require.NoError(t, err)
	_, err = client.AddSSHKey("git", keyFile, "")
	assert.Error(t, err, "AddSSHKey should error on an invalid key, but it must be called for coverage")
}

func TestUpdateApp_ErrorHandling(t *testing.T) {
	_, _, localRepo, _, localPath := setupGitForTest(t)
	repo := newTestRepo(t, &GitClient{})
	repo.localRepo = localRepo
	repo.localRepoPath = localPath

	t.Run("Failure on generating commit message", func(t *testing.T) {
		repo.gitConfig.CommitMessageFormat = "{{ .Invalid "
		newParams := &ArgoOverrideFile{}

		err := repo.UpdateApp("my-app", newParams, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "template: commitMsg:1: unclosed action")
	})
}

// TestCorruptedGitRepo_ErrorPaths covers failures on an invalid git repo.
func TestCorruptedGitRepo_ErrorPaths(t *testing.T) {
	corruptRepoPath := t.TempDir()
	gitDir := filepath.Join(corruptRepoPath, ".git")
	require.NoError(t, os.Mkdir(gitDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/master"), 0644))

	corruptRepo, err := git.PlainOpen(corruptRepoPath)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockHandler := mock.NewMockGitHandler(ctrl)
	repo := newTestRepo(t, mockHandler)

	mockHandler.EXPECT().AddSSHKey(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	mockHandler.EXPECT().PlainOpen(gomock.Any()).Return(corruptRepo, nil)
	mockHandler.EXPECT().PlainClone(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	// The clone should succeed because the corruption triggers a successful re-clone.
	err = repo.Clone()
	assert.NoError(t, err)
}
