package updater

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/mocks"
)

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

func TestGitOpTimeoutAccessor(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/dev/null")
	t.Setenv("GIT_OP_TIMEOUT", "42s")

	repo, err := NewGitRepo("u", "b", "p", "f", t.TempDir(), &GitClient{})
	require.NoError(t, err)

	assert.Equal(t, 42*time.Second, repo.GitOpTimeout())
}

func TestGitMaxAttemptsAccessor(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/dev/null")
	t.Setenv("GIT_MAX_ATTEMPTS", "7")

	repo, err := NewGitRepo("u", "b", "p", "f", t.TempDir(), &GitClient{})
	require.NoError(t, err)

	assert.Equal(t, uint(7), repo.GitMaxAttempts())
}

func TestInvalidateCache(t *testing.T) {
	t.Run("Removes cache directory and clears localRepo", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/dev/null")
		cacheBase := t.TempDir()

		repo, err := NewGitRepo("url", "branch", "path", "file", cacheBase, &GitClient{})
		require.NoError(t, err)

		repo.localRepoPath = filepath.Join(cacheBase, "cached-repo")
		require.NoError(t, os.MkdirAll(repo.localRepoPath, 0755))
		marker := filepath.Join(repo.localRepoPath, "marker")
		require.NoError(t, os.WriteFile(marker, []byte("x"), 0600))

		require.NoError(t, repo.InvalidateCache())

		_, err = os.Stat(repo.localRepoPath)
		assert.True(t, os.IsNotExist(err), "cache directory must be removed")
		assert.Nil(t, repo.localRepo, "localRepo handle must be cleared so next Clone re-resolves it")
	})

	t.Run("Does not error when cache directory does not exist", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/dev/null")

		cacheBase := t.TempDir()
		repo, err := NewGitRepo("url", "branch", "path", "file", cacheBase, &GitClient{})
		require.NoError(t, err)

		repo.localRepoPath = filepath.Join(cacheBase, "does-not-exist")
		assert.NoError(t, repo.InvalidateCache())
	})

	t.Run("No-op when localRepoPath is empty", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/dev/null")

		repo, err := NewGitRepo("url", "branch", "path", "file", t.TempDir(), &GitClient{})
		require.NoError(t, err)

		// localRepoPath is only set after the first Clone(); InvalidateCache before
		// the first Clone must be a safe no-op.
		repo.localRepoPath = ""
		assert.NoError(t, repo.InvalidateCache())
	})

	t.Run("Trailing separator on repoCachePath does not break removal", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/dev/null")
		cacheBase := t.TempDir()

		repo, err := NewGitRepo("url", "branch", "path", "file", cacheBase, &GitClient{})
		require.NoError(t, err)

		// Operator-supplied REPO_CACHE_PATH could legitimately have a trailing slash.
		// A raw string-prefix check would build "<base>//" and falsely refuse to remove.
		repo.repoCachePath = cacheBase + string(os.PathSeparator)
		repo.localRepoPath = filepath.Join(cacheBase, "cached-repo")
		require.NoError(t, os.MkdirAll(repo.localRepoPath, 0755))

		require.NoError(t, repo.InvalidateCache())
		_, err = os.Stat(repo.localRepoPath)
		assert.True(t, os.IsNotExist(err), "cache directory must be removed")
	})
}

func TestIsPermanent(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil is not permanent", nil, false},
		{"SSH key not provided is permanent", ErrSSHKeyNotProvided, true},
		{"SSH key not found is permanent", ErrSSHKeyNotFound, true},
		{"SSH key empty is permanent", ErrSSHKeyEmpty, true},
		{"wrapped SSH key not found is permanent", fmt.Errorf("clone: %w", ErrSSHKeyNotFound), true},
		{"transport auth required is permanent", transport.ErrAuthenticationRequired, true},
		{"transport auth failed is permanent", transport.ErrAuthorizationFailed, true},
		{"wrapped transport auth failed is permanent", fmt.Errorf("push: %w", transport.ErrAuthorizationFailed), true},
		{"network error is not permanent", errors.New("connection refused"), false},
		{"push race error is not permanent", errors.New("non-fast-forward update"), false},
		{"context deadline exceeded is retryable", context.DeadlineExceeded, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsPermanent(tc.err))
		})
	}
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
	assert.Equal(t, "apps/custom.yaml", generateOverrideFileNameForApp("apps", "custom.yaml", "my-app"))
	assert.Equal(t, "apps/.argocd-source-my-app.yaml", generateOverrideFileNameForApp("apps", "", "my-app"))
}

func TestGenerateCommitMessage(t *testing.T) {
	repo := newTestRepo(t, nil)
	tmplData := struct{ AppName string }{AppName: "test-app"}

	repo.gitConfig.CommitMessageFormat = ""
	assert.Equal(t, "argo-watcher(test-app): update image tag", repo.generateCommitMessage("test-app", tmplData))

	repo.gitConfig.CommitMessageFormat = "ci: bump {{ .AppName }}"
	assert.Equal(t, "ci: bump test-app", repo.generateCommitMessage("test-app", tmplData))

	// Template errors fall back to the default message silently (logged as warn)
	// so a malformed COMMIT_MESSAGE_FORMAT never aborts a deployment update.
	repo.gitConfig.CommitMessageFormat = "ci: bump {{ .AppName "
	assert.Equal(t, "argo-watcher(test-app): update image tag", repo.generateCommitMessage("test-app", tmplData),
		"parse error should fall back to default")

	repo.gitConfig.CommitMessageFormat = "ci: bump {{ .MissingKey }}"
	assert.Equal(t, "argo-watcher(test-app): update image tag", repo.generateCommitMessage("test-app", tmplData),
		"execute error should fall back to default")
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
	mockHandler := mocks.NewMockGitHandler(ctrl)
	repo := newTestRepo(t, mockHandler)

	mockHandler.EXPECT().AddSSHKey(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	t.Run("Cache Not Exists", func(t *testing.T) {
		mockHandler.EXPECT().PlainOpen(gomock.Any()).Return(nil, git.ErrRepositoryNotExists)
		// Capture the CloneOptions to assert the fresh clone is shallow. This is the
		// only cheap (non-integration) guard against a regression that drops Depth:1
		// or Tags:NoTags — `task test` excludes the integration suite, so without this
		// such a revert would ship green. The corrupt-cache reclone path shares this
		// call, so asserting once covers both.
		mockHandler.EXPECT().PlainClone(gomock.Any(), gomock.Any(), false, gomock.Any()).
			DoAndReturn(func(_ context.Context, _ string, _ bool, o *git.CloneOptions) (*git.Repository, error) {
				assert.Equal(t, 1, o.Depth, "fresh clone must be shallow (Depth:1)")
				assert.Equal(t, git.NoTags, o.Tags, "fresh clone must not fetch tags")
				assert.True(t, o.SingleBranch, "fresh clone must be single-branch")
				assert.Equal(t, plumbing.ReferenceName("refs/heads/"+repo.BranchName), o.ReferenceName)
				return nil, nil
			})
		err := repo.Clone(context.Background())
		assert.NoError(t, err)
	})

	t.Run("Cache Invalid", func(t *testing.T) {
		repo.localRepoPath = repo.getRepoCachePath()
		require.NoError(t, os.WriteFile(repo.localRepoPath, []byte("garbage"), 0600))

		mockHandler.EXPECT().PlainOpen(gomock.Any()).Return(nil, errors.New("invalid repo"))
		mockHandler.EXPECT().PlainClone(gomock.Any(), gomock.Any(), false, gomock.Any()).Return(nil, nil)
		err := repo.Clone(context.Background())
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

		err = repo.Clone(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch repo: repository not found")
	})

	t.Run("AddSSHKey is not called when sshAuth already set", func(t *testing.T) {
		// Simulate a second Clone call (e.g. race-recovery path) where the key
		// was loaded during the first Clone.  AddSSHKey must NOT be called again.
		repo.sshAuth = &ssh.PublicKeys{}

		mockHandler.EXPECT().PlainOpen(gomock.Any()).Return(nil, git.ErrRepositoryNotExists)
		mockHandler.EXPECT().PlainClone(gomock.Any(), gomock.Any(), false, gomock.Any()).Return(nil, nil)
		// No AddSSHKey expectation — strict controller fails the test if it is called.

		assert.NoError(t, repo.Clone(context.Background()))
		repo.sshAuth = nil
	})
}

func setupGitForTest(t *testing.T) (sourceRepo, remoteRepo, localRepo *git.Repository, remotePath, localPath string) {
	t.Helper()

	sourcePath := t.TempDir()
	sourceRepo, err := git.PlainInit(sourcePath, false)
	require.NoError(t, err)

	sourceWorktree, err := sourceRepo.Worktree()
	require.NoError(t, err)
	_, err = sourceWorktree.Commit("initial commit", &git.CommitOptions{
		Author:            &object.Signature{Name: "Initial", Email: "initial@test.com", When: time.Now()},
		AllowEmptyCommits: true,
	})
	require.NoError(t, err)

	remotePath = t.TempDir()
	remoteRepo, err = git.PlainClone(remotePath, true, &git.CloneOptions{
		URL: sourcePath,
	})
	require.NoError(t, err)

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

		err := repo.UpdateApp(context.Background(), "my-app", newParams, nil)
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

		initialParams := &ArgoOverrideFile{}
		initialParams.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "v2.0.0"}}
		err := repo.UpdateApp(context.Background(), "my-app", initialParams, nil)
		require.NoError(t, err)

		headBefore, err := localRepo.Head()
		require.NoError(t, err)

		err = repo.UpdateApp(context.Background(), "my-app", initialParams, nil)
		require.NoError(t, err)

		headAfter, err := localRepo.Head()
		require.NoError(t, err)
		assert.Equal(t, headBefore.Hash(), headAfter.Hash())
	})

	t.Run("Budget exhausted before push returns non-race error", func(t *testing.T) {
		_, _, localRepo, _, localPath := setupGitForTest(t)

		repo := newTestRepo(t, &GitClient{})
		repo.localRepo = localRepo
		repo.localRepoPath = localPath
		appDir := filepath.Join(localPath, "apps")
		require.NoError(t, os.Mkdir(appDir, 0755))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		newParams := &ArgoOverrideFile{}
		newParams.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "v2.0.0"}}

		err := repo.UpdateApp(ctx, "my-app", newParams, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "budget exhausted before push")
	})

	t.Run("Failure - Push Fails due to Non-Fast-Forward", func(t *testing.T) {
		sourceRepo, _, localRepo, remotePath, localPath := setupGitForTest(t)

		repo := newTestRepo(t, &GitClient{})
		repo.localRepo = localRepo
		repo.localRepoPath = localPath
		appDir := filepath.Join(localPath, "apps")
		require.NoError(t, os.Mkdir(appDir, 0755))

		sourceWorktree, err := sourceRepo.Worktree()
		require.NoError(t, err)

		_, err = sourceRepo.CreateRemote(&config.RemoteConfig{
			Name: "origin",
			URLs: []string{remotePath},
		})
		require.NoError(t, err)

		_, err = sourceWorktree.Commit("a conflicting commit", &git.CommitOptions{
			Author:            &object.Signature{Name: "Other", Email: "other@test.com", When: time.Now()},
			AllowEmptyCommits: true,
		})
		require.NoError(t, err)

		err = sourceRepo.Push(&git.PushOptions{})
		require.NoError(t, err)

		newParams := &ArgoOverrideFile{}
		newParams.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "v3.0.0"}}

		err = repo.UpdateApp(context.Background(), "my-app", newParams, nil)
		// We no longer classify push errors — UpdateApp just surfaces whatever go-git
		// returns. The retry loop in the caller decides whether to retry.
		assert.Error(t, err)
	})
}

func TestCommitAppLocalAndPush_MultipleAppsSinglePush(t *testing.T) {
	// Batch primitive: commit several apps' disjoint override files into one clone,
	// then push once. All files must land in a single push with one commit per app.
	_, remoteRepo, localRepo, _, localPath := setupGitForTest(t)

	repo := newTestRepo(t, &GitClient{})
	repo.localRepo = localRepo
	repo.localRepoPath = localPath
	// Path/FileName on the repo are unused in the batch path — each app supplies its own.
	repo.Path = ""
	repo.FileName = ""

	appDir := filepath.Join(localPath, "apps")
	require.NoError(t, os.MkdirAll(filepath.Join(appDir, "a"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(appDir, "b"), 0755))

	apps := []struct {
		name string
		path string
		tag  string
	}{
		{"app-a", "apps/a", "v1.0.0"},
		{"app-b", "apps/b", "v2.0.0"},
	}

	commitCount := 0
	for _, a := range apps {
		params := &ArgoOverrideFile{}
		params.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: a.tag}}
		committed, err := repo.CommitAppLocal(a.name, a.path, "", params, nil)
		require.NoError(t, err)
		assert.True(t, committed, "each new app override must produce a commit")
		commitCount++
	}
	assert.Equal(t, 2, commitCount)

	require.NoError(t, repo.Push(context.Background()))

	head, err := remoteRepo.Head()
	require.NoError(t, err)
	commit, err := remoteRepo.CommitObject(head.Hash())
	require.NoError(t, err)

	fileA, err := commit.File("apps/a/.argocd-source-app-a.yaml")
	require.NoError(t, err, "app-a override must be committed")
	contentsA, err := fileA.Contents()
	require.NoError(t, err)
	assert.Contains(t, contentsA, "v1.0.0")

	fileB, err := commit.File("apps/b/.argocd-source-app-b.yaml")
	require.NoError(t, err, "app-b override must be committed")
	contentsB, err := fileB.Contents()
	require.NoError(t, err)
	assert.Contains(t, contentsB, "v2.0.0")
}

func TestCommitAppLocal_NoChangeReportsNotCommitted(t *testing.T) {
	_, _, localRepo, _, localPath := setupGitForTest(t)

	repo := newTestRepo(t, &GitClient{})
	repo.localRepo = localRepo
	repo.localRepoPath = localPath
	require.NoError(t, os.MkdirAll(filepath.Join(localPath, "apps"), 0755))

	params := &ArgoOverrideFile{}
	params.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "v1.0.0"}}

	committed, err := repo.CommitAppLocal("app-a", "apps", "", params, nil)
	require.NoError(t, err)
	assert.True(t, committed)

	committed, err = repo.CommitAppLocal("app-a", "apps", "", params, nil)
	require.NoError(t, err)
	assert.False(t, committed, "unchanged content must report not committed so the push is skipped")
}

func TestCommitAppLocal_PathTraversalRejected(t *testing.T) {
	repo := newTestRepo(t, nil)
	repo.localRepoPath = t.TempDir()

	params := &ArgoOverrideFile{}
	// A malicious write-back-filename annotation attempting to escape the repo root.
	_, err := repo.CommitAppLocal("app-a", "apps", "../../../../etc/passwd", params, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not inside repository root")
}

func TestMergeOverrideFileContent(t *testing.T) {
	repo := newTestRepo(t, nil)
	newContent := &ArgoOverrideFile{}
	newContent.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "new"}}

	t.Run("File Not Exist", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "values.yaml")
		finalContent, err := repo.mergeOverrideFileContent(tmpFile, newContent)
		require.NoError(t, err)

		expected, err := yaml.Marshal(newContent)
		require.NoError(t, err)
		assert.Equal(t, string(expected), string(finalContent))
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

// The override file is not exclusively argo-watcher's: the default target is
// Argo CD's own .argocd-source-<app>.yaml, and write-back-filename can point at
// an operator's file. Only helm.parameters may be rewritten.
func TestMergeOverrideFileContentPreservesTheRestOfTheFile(t *testing.T) {
	repo := newTestRepo(t, nil)

	write := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "values-override.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))
		return path
	}

	newContent := &ArgoOverrideFile{}
	newContent.Helm.Parameters = []ArgoParameterOverride{
		{Name: "app.image.tag", Value: "v2", ForceString: true},
	}

	t.Run("keeps sibling helm keys and top-level keys", func(t *testing.T) {
		path := write(t, `helm:
  parameters:
    - name: app.image.tag
      value: v1
      forceString: true
  valueFiles:
    - values-prod.yaml
  values: |
    replicaCount: 3
kustomize:
  namePrefix: prod-
`)
		merged, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)

		out := string(merged)
		assert.Contains(t, out, "valueFiles")
		assert.Contains(t, out, "values-prod.yaml")
		assert.Contains(t, out, "replicaCount: 3")
		assert.Contains(t, out, "namePrefix: prod-")
		assert.Contains(t, out, "value: v2")
		assert.NotContains(t, out, "value: v1")
	})

	t.Run("keeps comments", func(t *testing.T) {
		path := write(t, `# pinned by the platform team
helm:
  parameters:
    - name: app.image.tag
      value: v1
  # the prod overlay must stay last
  valueFiles:
    - values-prod.yaml
`)
		merged, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)

		out := string(merged)
		assert.Contains(t, out, "# pinned by the platform team")
		assert.Contains(t, out, "# the prod overlay must stay last")
	})

	// commitLocal skips the commit on a byte-identical render, so a comment
	// layout that shifted on each round trip would commit on every deployment.
	t.Run("a commented file reaches a fixed point", func(t *testing.T) {
		path := write(t, `# pinned by the platform team

helm:
  # argo-watcher owns these
  parameters:
    - name: app.image.tag
      value: v1

  # the prod overlay must stay last
  valueFiles:
    - values-prod.yaml   # trailing comment

# footer comment
`)
		first, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, first, 0644))

		second, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)
		assert.Equal(t, string(first), string(second))
	})

	t.Run("adds the helm block to a file that has none", func(t *testing.T) {
		path := write(t, "replicaCount: 3\n")

		merged, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)

		out := string(merged)
		assert.Contains(t, out, "replicaCount: 3")
		assert.Contains(t, out, "app.image.tag")
		assert.Contains(t, out, "value: v2")
	})

	t.Run("appends a parameter the file does not carry", func(t *testing.T) {
		path := write(t, `helm:
  parameters:
    - name: other.tag
      value: keepme
`)
		merged, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)

		var parsed ArgoOverrideFile
		require.NoError(t, yaml.Unmarshal(merged, &parsed))
		assert.Equal(t, []ArgoParameterOverride{
			{Name: "other.tag", Value: "keepme"},
			{Name: "app.image.tag", Value: "v2", ForceString: true},
		}, parsed.Helm.Parameters)
	})

	t.Run("an empty file is written as a fresh override", func(t *testing.T) {
		path := write(t, "")

		merged, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)

		expected, err := yaml.Marshal(newContent)
		require.NoError(t, err)
		assert.Equal(t, string(expected), string(merged))
	})

	t.Run("a non-mapping document is refused rather than overwritten", func(t *testing.T) {
		path := write(t, "- just\n- a\n- list\n")

		_, err := repo.mergeOverrideFileContent(path, newContent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mapping")
	})

	// Only the first document survives a Node round-trip, so editing such a file
	// would drop the rest of it.
	t.Run("a multi-document file is refused rather than truncated", func(t *testing.T) {
		path := write(t, `helm:
  parameters:
    - name: app.image.tag
      value: v1
---
second: document
`)
		_, err := repo.mergeOverrideFileContent(path, newContent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "more than one YAML document")
	})

	t.Run("a bare helm key is filled in rather than refused", func(t *testing.T) {
		path := write(t, "helm:\nother: 1\n")

		merged, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)

		out := string(merged)
		assert.Contains(t, out, "other: 1")
		assert.Contains(t, out, "app.image.tag")
		assert.Contains(t, out, "value: v2")
	})

	t.Run("a null document is written as a fresh override", func(t *testing.T) {
		path := write(t, "~\n")

		merged, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)

		expected, err := yaml.Marshal(newContent)
		require.NoError(t, err)
		assert.Equal(t, string(expected), string(merged))
	})

	// The common case: the file Argo Watcher created on the first deployment, read
	// back on the second. Any drift here commits on every deployment forever.
	t.Run("a file Argo Watcher wrote round-trips unchanged", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".argocd-source-app.yaml")
		fresh, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, fresh, 0644))

		again, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)
		assert.Equal(t, string(fresh), string(again))
	})

	t.Run("rewriting with the same values is byte-stable", func(t *testing.T) {
		path := write(t, `helm:
  parameters:
    - name: app.image.tag
      value: v2
      forceString: true
  valueFiles:
    - values-prod.yaml
`)
		first, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, first, 0644))

		second, err := repo.mergeOverrideFileContent(path, newContent)
		require.NoError(t, err)
		assert.Equal(t, string(first), string(second),
			"a repeat write-back must not churn the file, or commitLocal commits on every deploy")
	})
}

// renderOverride is the bytes CommitAppLocal would hand commitLocal for a file
// that does not exist yet.
func renderOverride(t *testing.T, content *ArgoOverrideFile) []byte {
	t.Helper()
	rendered, err := yaml.Marshal(content)
	require.NoError(t, err)
	return rendered
}

// commitLocalAndPush is the single-app compose (commit locally, then push) that
// UpdateApp performs; tests that assert on remote state use it directly.
func commitLocalAndPush(t *testing.T, repo *GitRepo, fullPath, msg string, content *ArgoOverrideFile) {
	t.Helper()
	committed, err := repo.commitLocal(fullPath, msg, renderOverride(t, content))
	require.NoError(t, err)
	require.True(t, committed)
	require.NoError(t, repo.push(context.Background()))
}

func TestCommitLocal_SkipsWhenContentUnchanged(t *testing.T) {
	_, _, localRepo, _, localPath := setupGitForTest(t)

	repo := newTestRepo(t, &GitClient{})
	repo.localRepo = localRepo
	repo.localRepoPath = localPath
	appDir := filepath.Join(localPath, "apps")
	require.NoError(t, os.Mkdir(appDir, 0755))
	fullPath := filepath.Join(appDir, "values.yaml")

	params := &ArgoOverrideFile{}
	params.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "v2.0.0"}}

	commitLocalAndPush(t, repo, fullPath, "msg", params)

	headBefore, err := localRepo.Head()
	require.NoError(t, err)

	committed, err := repo.commitLocal(fullPath, "msg", renderOverride(t, params))
	require.NoError(t, err)
	assert.False(t, committed, "unchanged content must report not committed")

	headAfter, err := localRepo.Head()
	require.NoError(t, err)
	assert.Equal(t, headBefore.Hash(), headAfter.Hash(), "expected no new commit when content is unchanged")
}

func TestCommitLocalAndPush_RestagesModifiedTrackedFile(t *testing.T) {
	_, remoteRepo, localRepo, _, localPath := setupGitForTest(t)

	repo := newTestRepo(t, &GitClient{})
	repo.localRepo = localRepo
	repo.localRepoPath = localPath
	appDir := filepath.Join(localPath, "apps")
	require.NoError(t, os.Mkdir(appDir, 0755))
	fullPath := filepath.Join(appDir, "values.yaml")

	v1 := &ArgoOverrideFile{}
	v1.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "v1.0.0"}}
	commitLocalAndPush(t, repo, fullPath, "v1", v1)
	headV1, err := localRepo.Head()
	require.NoError(t, err)

	// Second commit modifies the already-tracked file to v2. SkipStatus must still
	// stage the modified content and advance HEAD with the new blob.
	v2 := &ArgoOverrideFile{}
	v2.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "v2.0.0"}}
	commitLocalAndPush(t, repo, fullPath, "v2", v2)

	headV2, err := localRepo.Head()
	require.NoError(t, err)
	assert.NotEqual(t, headV1.Hash(), headV2.Hash(), "modifying a tracked file must produce a new commit")

	commit, err := remoteRepo.CommitObject(headV2.Hash())
	require.NoError(t, err)
	file, err := commit.File("apps/values.yaml")
	require.NoError(t, err)
	contents, err := file.Contents()
	require.NoError(t, err)
	assert.Contains(t, contents, "v2.0.0")
	assert.NotContains(t, contents, "v1.0.0")
}

func TestCommitLocalAndPush_StagesOnlyTargetFile(t *testing.T) {
	_, remoteRepo, localRepo, _, localPath := setupGitForTest(t)

	repo := newTestRepo(t, &GitClient{})
	repo.localRepo = localRepo
	repo.localRepoPath = localPath
	appDir := filepath.Join(localPath, "apps")
	require.NoError(t, os.Mkdir(appDir, 0755))
	fullPath := filepath.Join(appDir, "values.yaml")

	// An unrelated, untracked file sitting in the worktree must never be swept into
	// our commit — we stage exactly the override file and nothing else.
	require.NoError(t, os.WriteFile(filepath.Join(localPath, "stray.txt"), []byte("junk"), 0600))

	params := &ArgoOverrideFile{}
	params.Helm.Parameters = []ArgoParameterOverride{{Name: "image.tag", Value: "v2.0.0"}}
	commitLocalAndPush(t, repo, fullPath, "msg", params)

	head, err := remoteRepo.Head()
	require.NoError(t, err)
	commit, err := remoteRepo.CommitObject(head.Hash())
	require.NoError(t, err)
	tree, err := commit.Tree()
	require.NoError(t, err)

	_, err = tree.File("apps/values.yaml")
	require.NoError(t, err, "override file should be committed")
	_, err = tree.File("stray.txt")
	assert.Error(t, err, "unrelated untracked file must not be committed")
}

func TestCommitLocal_WriteFileError(t *testing.T) {
	localPath := t.TempDir()
	r, err := git.PlainInit(localPath, false)
	require.NoError(t, err)

	repo := newTestRepo(t, nil)
	repo.localRepo = r
	repo.localRepoPath = localPath

	fullPath := filepath.Join(localPath, "apps")
	require.NoError(t, os.Mkdir(fullPath, 0755))

	_, err = repo.commitLocal(fullPath, "msg", renderOverride(t, &ArgoOverrideFile{}))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write override file")
}

func TestGitClient_Coverage(t *testing.T) {
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

	client := GitClient{}

	clonePath := t.TempDir()
	_, err = client.PlainClone(context.Background(), clonePath, false, &git.CloneOptions{URL: sourcePath})
	require.NoError(t, err, "PlainClone method failed")

	_, err = client.PlainOpen(clonePath)
	require.NoError(t, err, "PlainOpen method failed")

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

	t.Run("Malformed commit message template uses default and does not abort update", func(t *testing.T) {
		repo.gitConfig.CommitMessageFormat = "{{ .Invalid "
		newParams := &ArgoOverrideFile{}
		appDir := filepath.Join(localPath, "apps")
		require.NoError(t, os.Mkdir(appDir, 0755))

		err := repo.UpdateApp(context.Background(), "my-app", newParams, nil)
		assert.NoError(t, err)
	})
}

func TestAssertInsideRoot(t *testing.T) {
	root := t.TempDir()

	t.Run("Path inside root is accepted", func(t *testing.T) {
		assert.NoError(t, assertInsideRoot(root, filepath.Join(root, "apps", "values.yaml")))
	})

	t.Run("Path traversal is rejected", func(t *testing.T) {
		err := assertInsideRoot(root, filepath.Join(root, "..", "etc", "passwd"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not inside repository root")
	})

	t.Run("Deeply nested traversal is rejected", func(t *testing.T) {
		// Simulates FileName = "../../../../.ssh/authorized_keys"
		escaped := filepath.Join(root, "apps", "..", "..", "..", "..", ".ssh", "authorized_keys")
		err := assertInsideRoot(root, escaped)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not inside repository root")
	})

	t.Run("Root itself is rejected (not strictly inside)", func(t *testing.T) {
		err := assertInsideRoot(root, root)
		require.Error(t, err)
	})

	t.Run("Legitimate path whose first component starts with .. is accepted", func(t *testing.T) {
		// filepath.Rel returns "..foo/file.yaml" here; a naive strings.HasPrefix(rel, "..")
		// check would falsely flag it as an escape attempt. Only "..", ".."+separator, or
		// ".." with deeper separator-segmented prefixes count as parent traversal.
		assert.NoError(t, assertInsideRoot(root, filepath.Join(root, "..foo", "file.yaml")))
	})

	t.Run("Parent-directory escape (rel == \"..\") is rejected", func(t *testing.T) {
		// The parent dir of root, after Clean, yields rel == ".." — must be rejected.
		err := assertInsideRoot(root, filepath.Dir(root))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not inside repository root")
	})
}

func TestInvalidateCache_PathEscapeGuard(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/dev/null")

	cacheBase := t.TempDir()
	repo, err := NewGitRepo("url", "branch", "path", "file", cacheBase, &GitClient{})
	require.NoError(t, err)

	repo.localRepoPath = "/tmp/dangerous-removal-target"

	err = repo.InvalidateCache()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to remove")
}

func TestCorruptedGitRepo_ErrorPaths(t *testing.T) {
	corruptRepoPath := t.TempDir()
	gitDir := filepath.Join(corruptRepoPath, ".git")
	require.NoError(t, os.Mkdir(gitDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/master"), 0644))

	corruptRepo, err := git.PlainOpen(corruptRepoPath)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockHandler := mocks.NewMockGitHandler(ctrl)
	repo := newTestRepo(t, mockHandler)

	mockHandler.EXPECT().AddSSHKey(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	mockHandler.EXPECT().PlainOpen(gomock.Any()).Return(corruptRepo, nil)
	mockHandler.EXPECT().PlainClone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	// The clone should succeed because the corruption triggers a successful re-clone.
	err = repo.Clone(context.Background())
	assert.NoError(t, err)
}

func TestValidateSSHKeyFile(t *testing.T) {
	t.Run("Empty path returns error", func(t *testing.T) {
		err := validateSSHKeyFile("")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSSHKeyNotProvided)
	})

	t.Run("Non-existent file returns error", func(t *testing.T) {
		err := validateSSHKeyFile("/nonexistent/path/to/key")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSSHKeyNotFound)
	})

	t.Run("Empty file returns error", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "empty_key")
		err := os.WriteFile(tmpFile, []byte{}, 0600)
		require.NoError(t, err)

		err = validateSSHKeyFile(tmpFile)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSSHKeyEmpty)
	})

	t.Run("Invalid key format returns error", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "invalid_key")
		err := os.WriteFile(tmpFile, []byte("not-a-valid-ssh-key"), 0600)
		require.NoError(t, err)

		err = validateSSHKeyFile(tmpFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid SSH key format")
	})

	t.Run("Valid unencrypted key succeeds", func(t *testing.T) {
		// Valid Ed25519 SSH private key (generated for testing)
		validKey := `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACAC9T4h865mZBBtHdvtGIOAi0l8qogye+9+EKkrrNbVlAAAAJirs5HLq7OR
ywAAAAtzc2gtZWQyNTUxOQAAACAC9T4h865mZBBtHdvtGIOAi0l8qogye+9+EKkrrNbVlA
AAAECOtNdCM3z6ouKDjZgB3DjTiUBBfS8NmONZLkpV1EgeKAL1PiHzrmZkEG0d2+0Yg4CL
SXyqiDJ7734QqSus1tWUAAAAFHNoaW5pNGlAZnJhbWV3b3JrLTEzAQ==
-----END OPENSSH PRIVATE KEY-----`
		tmpFile := filepath.Join(t.TempDir(), "valid_key")
		err := os.WriteFile(tmpFile, []byte(validKey), 0600)
		require.NoError(t, err)

		err = validateSSHKeyFile(tmpFile)
		assert.NoError(t, err)
	})

	t.Run("Valid encrypted key succeeds", func(t *testing.T) {
		// Valid Ed25519 SSH private key encrypted with passphrase "testpass"
		encryptedKey := `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAACmFlczI1Ni1jdHIAAAAGYmNyeXB0AAAAGAAAABB7upjLwQ
AqkI4IJ+iES3CQAAAAGAAAAAEAAAAzAAAAC3NzaC1lZDI1NTE5AAAAICpBc9pX6uJ9yVUV
SQRskuz4jZCAfgtcqTi9nXzgJXskAAAAkNKmGjfrN8moWBtZTotUr9Dw+OcxErFtT+5FCE
+6TzFqWPcM820d4ZNHgz3HJ494RRmOcWRTjQbturOddTC0r5tf1kU2rIMke0FPGsivVv00
CasAIVVHpCHI6L70/csWGiHHxGUGntRpH61OyRXmlRLithA71mvrZpoec9fEzN6VypcFfB
OWSUtQ6YwycX/LFw==
-----END OPENSSH PRIVATE KEY-----`
		tmpFile := filepath.Join(t.TempDir(), "encrypted_key")
		err := os.WriteFile(tmpFile, []byte(encryptedKey), 0600)
		require.NoError(t, err)

		err = validateSSHKeyFile(tmpFile)
		assert.NoError(t, err)
	})

	t.Run("Directory path returns error", func(t *testing.T) {
		tmpDir := t.TempDir()

		err := validateSSHKeyFile(tmpDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SSH key path is a directory")
	})
}

// TestAddSSHKeyValidation tests that AddSSHKey validates keys before loading.
func TestAddSSHKeyValidation(t *testing.T) {
	client := GitClient{}

	t.Run("Empty path returns validation error", func(t *testing.T) {
		_, err := client.AddSSHKey("git", "", "")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSSHKeyNotProvided)
	})

	t.Run("Non-existent file returns validation error", func(t *testing.T) {
		_, err := client.AddSSHKey("git", "/nonexistent/key", "")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSSHKeyNotFound)
	})

	t.Run("Invalid key format returns validation error", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "invalid_key")
		err := os.WriteFile(tmpFile, []byte("invalid-key-content"), 0600)
		require.NoError(t, err)

		_, err = client.AddSSHKey("git", tmpFile, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid SSH key format")
	})
}
