package argocd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/updater"
)

// inlineAppsManifest holds two Application documents sharing one file, as an
// app-of-apps repository usually does.
const inlineAppsManifest = `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-a
spec:
  source:
    helm:
      valuesObject:
        image:
          tag: v0.0.1 # {"$argo-watcher": "app-a:app"}
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-b
spec:
  source:
    helm:
      valuesObject:
        image:
          tag: v0.0.1 # {"$argo-watcher": "app-b:app"}
`

const inlineRepoURL = "git@example.com:test/inline.git"

// newInlineApp is an app in inline mode; unlike override mode it needs no
// <alias>.helm.image-tag annotation.
func newInlineApp(name string) *models.Application {
	return &models.Application{
		Metadata: models.ApplicationMetadata{
			Name:        name,
			Annotations: map[string]string{"argo-watcher/managed-images": "app=myimage"},
		},
	}
}

func inlineGitopsRepo(cachePath string) *models.GitopsRepo {
	return &models.GitopsRepo{
		RepoUrl: inlineRepoURL, BranchName: "master", Path: "apps", Filename: "apps.yaml",
		RepoCachePath: cachePath, Inline: true,
	}
}

// newInlineRemote creates a bare remote whose master holds apps/apps.yaml.
func newInlineRemote(t *testing.T) string {
	t.Helper()
	sourcePath := t.TempDir()
	sourceRepo, err := gogit.PlainInit(sourcePath, false)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(sourcePath, "apps"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sourcePath, "apps", "apps.yaml"), []byte(inlineAppsManifest), 0644))
	worktree, err := sourceRepo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add("apps/apps.yaml")
	require.NoError(t, err)
	_, err = worktree.Commit("initial", &gogit.CommitOptions{Author: &object.Signature{Name: "init", Email: "init@test.com", When: time.Now()}})
	require.NoError(t, err)

	remotePath := t.TempDir()
	_, err = gogit.PlainClone(remotePath, true, &gogit.CloneOptions{URL: sourcePath})
	require.NoError(t, err)
	return remotePath
}

// cloneHandler serves a fresh clone of remotePath at the cache path the updater uses.
func cloneHandler(t *testing.T, remotePath string) updater.GitHandler {
	t.Helper()
	handler := mocks.NewMockGitHandler(gomock.NewController(t))
	handler.EXPECT().AddSSHKey(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	handler.EXPECT().PlainOpen(gomock.Any()).DoAndReturn(gogit.PlainOpen).AnyTimes()
	handler.EXPECT().PlainClone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, path string, isBare bool, o *gogit.CloneOptions) (*gogit.Repository, error) {
			return gogit.PlainCloneContext(ctx, path, isBare, &gogit.CloneOptions{
				URL: remotePath, ReferenceName: o.ReferenceName, SingleBranch: true,
			})
		}).AnyTimes()
	return handler
}

// remoteFile returns a file's contents at the tip of the remote's master.
func remoteFile(t *testing.T, remotePath, name string) string {
	t.Helper()
	remote, err := gogit.PlainOpen(remotePath)
	require.NoError(t, err)
	head, err := remote.Head()
	require.NoError(t, err)
	commit, err := remote.CommitObject(head.Hash())
	require.NoError(t, err)
	file, err := commit.File(name)
	require.NoError(t, err)
	contents, err := file.Contents()
	require.NoError(t, err)
	return contents
}

// publishExternalEdit commits an edit of another line of apps/apps.yaml to the
// remote, as an external writer would.
func publishExternalEdit(t *testing.T, remotePath, oldLine, newLine string) {
	t.Helper()
	path := t.TempDir()
	competitor, err := gogit.PlainClone(path, false, &gogit.CloneOptions{URL: remotePath})
	require.NoError(t, err)
	file := filepath.Join(path, "apps", "apps.yaml")
	content, err := os.ReadFile(file)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, []byte(strings.Replace(string(content), oldLine, newLine, 1)), 0644))
	worktree, err := competitor.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add("apps/apps.yaml")
	require.NoError(t, err)
	_, err = worktree.Commit("external edit", &gogit.CommitOptions{Author: &object.Signature{Name: "human", Email: "h@test.com", When: time.Now()}})
	require.NoError(t, err)
	require.NoError(t, competitor.Push(&gogit.PushOptions{}))
}

func TestGenerateInlineTagUpdates(t *testing.T) {
	t.Run("builds one update per managed image without a helm.image-tag annotation", func(t *testing.T) {
		updates, err := generateInlineTagUpdates(newInlineApp("app-a").Metadata.Annotations, newImageTask())
		require.NoError(t, err)
		assert.Equal(t, []updater.InlineTagUpdate{{Alias: "app", Tag: "v1.0.0"}}, updates)
	})

	t.Run("rejects a tag outside the OCI grammar before any clone", func(t *testing.T) {
		task := &models.Task{Id: "id", Images: []models.Image{{Image: "myimage", Tag: "v1\ninjected: true"}}}
		_, err := generateInlineTagUpdates(newInlineApp("app-a").Metadata.Annotations, task)
		assert.ErrorContains(t, err, "not a valid image tag")
	})

	t.Run("collapses an image listed twice with the same tag", func(t *testing.T) {
		task := &models.Task{Id: "id", Images: []models.Image{{Image: "myimage", Tag: "v1"}, {Image: "myimage", Tag: "v1"}}}
		updates, err := generateInlineTagUpdates(newInlineApp("app-a").Metadata.Annotations, task)
		require.NoError(t, err)
		assert.Equal(t, []updater.InlineTagUpdate{{Alias: "app", Tag: "v1"}}, updates)
	})

	t.Run("rejects an image listed twice with different tags", func(t *testing.T) {
		task := &models.Task{Id: "id", Images: []models.Image{{Image: "myimage", Tag: "v1"}, {Image: "myimage", Tag: "v2"}}}
		_, err := generateInlineTagUpdates(newInlineApp("app-a").Metadata.Annotations, task)
		assert.ErrorContains(t, err, `image "myimage" is given twice with different tags`)
	})

	t.Run("reports nothing to write when no image is managed", func(t *testing.T) {
		task := &models.Task{Id: "id", Images: []models.Image{{Image: "unmanaged", Tag: "v1"}}}
		updates, err := generateInlineTagUpdates(newInlineApp("app-a").Metadata.Annotations, task)
		require.NoError(t, err)
		assert.Nil(t, updates)
	})
}

func TestUpdateGitImageTag_Inline(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/dev/null")
	t.Setenv("GIT_OP_TIMEOUT", "30s")
	t.Setenv("GIT_MAX_ATTEMPTS", "3")

	t.Run("writes only the marked field of its own application", func(t *testing.T) {
		remotePath := newInlineRemote(t)

		err := UpdateGitImageTag(context.Background(), newInlineApp("app-a"), newImageTask(), inlineGitopsRepo(t.TempDir()), cloneHandler(t, remotePath))
		require.NoError(t, err)

		want := strings.Replace(inlineAppsManifest,
			`tag: v0.0.1 # {"$argo-watcher": "app-a:app"}`, `tag: v1.0.0 # {"$argo-watcher": "app-a:app"}`, 1)
		assert.Equal(t, want, remoteFile(t, remotePath, "apps/apps.yaml"))
	})

	t.Run("keeps an external edit to the same file that won the push race", func(t *testing.T) {
		remotePath := newInlineRemote(t)
		cachePath := t.TempDir()

		// The first attempt works on a clone taken before the external edit, so its
		// push is rejected; the retry must re-fetch and re-apply on the new tip.
		localPath := computeRepoCachePath(cachePath, inlineRepoURL, "master")
		staleClone, err := gogit.PlainClone(localPath, false, &gogit.CloneOptions{URL: remotePath, ReferenceName: "refs/heads/master", SingleBranch: true})
		require.NoError(t, err)
		publishExternalEdit(t, remotePath,
			`tag: v0.0.1 # {"$argo-watcher": "app-b:app"}`, `tag: v9.9.9 # {"$argo-watcher": "app-b:app"}`)

		handler := mocks.NewMockGitHandler(gomock.NewController(t))
		handler.EXPECT().AddSSHKey(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		first := handler.EXPECT().PlainOpen(gomock.Any()).Return(nil, gogit.ErrRepositoryNotExists)
		handler.EXPECT().PlainClone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(staleClone, nil)
		handler.EXPECT().PlainOpen(gomock.Any()).Return(staleClone, nil).After(first)

		err = UpdateGitImageTag(context.Background(), newInlineApp("app-a"), newImageTask(), inlineGitopsRepo(cachePath), handler)
		require.NoError(t, err)

		got := remoteFile(t, remotePath, "apps/apps.yaml")
		assert.Contains(t, got, `tag: v1.0.0 # {"$argo-watcher": "app-a:app"}`)
		assert.Contains(t, got, `tag: v9.9.9 # {"$argo-watcher": "app-b:app"}`, "the external commit must survive")
	})

	t.Run("fails loudly when the file has no marker for the app", func(t *testing.T) {
		remotePath := newInlineRemote(t)
		t.Setenv("GIT_MAX_ATTEMPTS", "1")

		err := UpdateGitImageTag(context.Background(), newInlineApp("app-c"), newImageTask(), inlineGitopsRepo(t.TempDir()), cloneHandler(t, remotePath))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `no field is marked {"$argo-watcher": "app-c:app"}`)
	})
}

func TestRunBatchWriteBack_InlineAppsShareOneFile(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/dev/null")
	t.Setenv("GIT_OP_TIMEOUT", "30s")
	t.Setenv("GIT_MAX_ATTEMPTS", "2")

	remotePath := newInlineRemote(t)
	cachePath := t.TempDir()
	repo, err := updater.NewGitRepo(inlineRepoURL, "master", "", "", cachePath, cloneHandler(t, remotePath))
	require.NoError(t, err)

	reqFor := func(name, tag string) *batchWriteRequest {
		return &batchWriteRequest{
			app:        newInlineApp(name),
			task:       &models.Task{Id: name, Images: []models.Image{{Image: "myimage", Tag: tag}}},
			gitopsRepo: inlineGitopsRepo(cachePath),
			resultCh:   make(chan error, 1),
		}
	}
	batch := []*batchWriteRequest{reqFor("app-a", "v2.0.0"), reqFor("app-b", "v3.0.0")}

	outcomes := runBatchWriteBack(context.Background(), repo, batch, nil)

	for _, req := range batch {
		assert.NoError(t, outcomes[req])
	}
	got := remoteFile(t, remotePath, "apps/apps.yaml")
	assert.Contains(t, got, `tag: v2.0.0 # {"$argo-watcher": "app-a:app"}`)
	assert.Contains(t, got, `tag: v3.0.0 # {"$argo-watcher": "app-b:app"}`)
}

func TestRunBatchWriteBack_InlineRefusalDoesNotBlockOthers(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/dev/null")
	t.Setenv("GIT_OP_TIMEOUT", "30s")
	t.Setenv("GIT_MAX_ATTEMPTS", "2")

	remotePath := newInlineRemote(t)
	cachePath := t.TempDir()
	repo, err := updater.NewGitRepo(inlineRepoURL, "master", "", "", cachePath, cloneHandler(t, remotePath))
	require.NoError(t, err)

	reqFor := func(name, tag string) *batchWriteRequest {
		return &batchWriteRequest{
			app:        newInlineApp(name),
			task:       &models.Task{Id: name, Images: []models.Image{{Image: "myimage", Tag: tag}}},
			gitopsRepo: inlineGitopsRepo(cachePath),
			resultCh:   make(chan error, 1),
		}
	}
	valid, unmarked := reqFor("app-a", "v2.0.0"), reqFor("app-c", "v3.0.0")

	outcomes := runBatchWriteBack(context.Background(), repo, []*batchWriteRequest{valid, unmarked}, nil)

	assert.NoError(t, outcomes[valid])
	require.Error(t, outcomes[unmarked])
	assert.Contains(t, outcomes[unmarked].Error(), `no field is marked {"$argo-watcher": "app-c:app"}`)

	want := strings.Replace(inlineAppsManifest,
		`tag: v0.0.1 # {"$argo-watcher": "app-a:app"}`, `tag: v2.0.0 # {"$argo-watcher": "app-a:app"}`, 1)
	assert.Equal(t, want, remoteFile(t, remotePath, "apps/apps.yaml"))
}

func TestGenerateInlineTagUpdates_SeveralAliases(t *testing.T) {
	annotations := map[string]string{"argo-watcher/managed-images": "app=myimage,sidecar=side"}
	task := &models.Task{Id: "id", Images: []models.Image{{Image: "myimage", Tag: "v1"}, {Image: "side", Tag: "s1"}}}

	updates, err := generateInlineTagUpdates(annotations, task)
	require.NoError(t, err)
	assert.ElementsMatch(t, []updater.InlineTagUpdate{{Alias: "app", Tag: "v1"}, {Alias: "sidecar", Tag: "s1"}}, updates)
}
