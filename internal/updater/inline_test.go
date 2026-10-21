package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applicationManifest is a realistic app-of-apps file: two Application documents,
// hand-made formatting and comments that a re-marshal would disturb.
const applicationManifest = `# Managed by the platform team.
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: demo
  annotations:
    argo-watcher/managed: "true"
spec:
  source:
    repoURL: https://charts.example.com
    chart: demo
    helm:
      valuesObject:
        image:
          repository: registry.example.com/demo
          tag: 1.0.0  # {"$argo-watcher": "demo:app"}
        sidecar:
          tag: "s1" # {"$argo-watcher": "demo:sidecar"}
        replicas:   3   # odd spacing kept
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: other
spec:
  source:
    helm:
      valuesObject:
        image:
          tag: 1.0.0 # {"$argo-watcher": "other:app"}
`

func TestSpliceMarkedTags_ReplacesOnlyTheMarkedToken(t *testing.T) {
	got, err := spliceMarkedTags([]byte(applicationManifest), "demo", []InlineTagUpdate{{Alias: "app", Tag: "2.0.0"}})
	require.NoError(t, err)

	want := strings.Replace(applicationManifest,
		`tag: 1.0.0  # {"$argo-watcher": "demo:app"}`,
		`tag: 2.0.0  # {"$argo-watcher": "demo:app"}`, 1)
	assert.Equal(t, want, string(got), "every byte outside the marked token must be preserved")
}

func TestSpliceMarkedTags_SeveralAliases(t *testing.T) {
	got, err := spliceMarkedTags([]byte(applicationManifest), "demo", []InlineTagUpdate{
		{Alias: "app", Tag: "2.0.0"},
		{Alias: "sidecar", Tag: "s2"},
	})
	require.NoError(t, err)

	want := strings.Replace(applicationManifest, `tag: 1.0.0  # {"$argo-watcher": "demo:app"}`, `tag: 2.0.0  # {"$argo-watcher": "demo:app"}`, 1)
	want = strings.Replace(want, `tag: "s1" # {"$argo-watcher": "demo:sidecar"}`, `tag: "s2" # {"$argo-watcher": "demo:sidecar"}`, 1)
	assert.Equal(t, want, string(got))
}

func TestSpliceMarkedTags_LeavesOtherApplicationsAlone(t *testing.T) {
	got, err := spliceMarkedTags([]byte(applicationManifest), "other", []InlineTagUpdate{{Alias: "app", Tag: "9.9.9"}})
	require.NoError(t, err)

	want := strings.Replace(applicationManifest,
		`tag: 1.0.0 # {"$argo-watcher": "other:app"}`,
		`tag: 9.9.9 # {"$argo-watcher": "other:app"}`, 1)
	assert.Equal(t, want, string(got))
}

func TestSpliceMarkedTags_UpdatesEveryFieldMarkedForTheAlias(t *testing.T) {
	src := "a: x # {\"$argo-watcher\": \"demo:app\"}\nb: y # {\"$argo-watcher\": \"demo:app\"}\n"
	got, err := spliceMarkedTags([]byte(src), "demo", []InlineTagUpdate{{Alias: "app", Tag: "v2"}})
	require.NoError(t, err)
	assert.Equal(t, "a: v2 # {\"$argo-watcher\": \"demo:app\"}\nb: v2 # {\"$argo-watcher\": \"demo:app\"}\n", string(got))
}

func TestSpliceMarkedTags_ScalarStyles(t *testing.T) {
	marker := ` # {"$argo-watcher": "demo:app"}`
	testCases := []struct {
		name string
		src  string
		tag  string
		want string
	}{
		{"plain", "tag: v1" + marker + "\n", "v2", "tag: v2" + marker + "\n"},
		{"double quoted keeps its quotes", `tag: "v1"` + marker + "\n", "v2", `tag: "v2"` + marker + "\n"},
		{"single quoted keeps its quotes", `tag: 'v1'` + marker + "\n", "v2", `tag: 'v2'` + marker + "\n"},
		{"plain numeric-looking tag is quoted so it stays a string", "tag: v1" + marker + "\n", "1.10", `tag: "1.10"` + marker + "\n"},
		{"plain integer-looking tag is quoted", "tag: v1" + marker + "\n", "1234567", `tag: "1234567"` + marker + "\n"},
		{"plain boolean-looking tag is quoted", "tag: v1" + marker + "\n", "true", `tag: "true"` + marker + "\n"},
		{"numeric existing value becomes a string", "tag: 1.2" + marker + "\n", "abc", "tag: abc" + marker + "\n"},
		{"sequence item", "tags:\n  - v1" + marker + "\n", "v2", "tags:\n  - v2" + marker + "\n"},
		{"non-ASCII earlier on the line", "é: v1" + marker + "\n", "v2", "é: v2" + marker + "\n"},
		{"CRLF line endings", "x: 1\r\ntag: v1" + marker + "\r\ny: 2\r\n", "v2", "x: 1\r\ntag: v2" + marker + "\r\ny: 2\r\n"},
		{"tag already current is a no-op", "tag: v1" + marker + "\n", "v1", "tag: v1" + marker + "\n"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := spliceMarkedTags([]byte(tc.src), "demo", []InlineTagUpdate{{Alias: "app", Tag: tc.tag}})
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func TestSpliceMarkedTags_Refusals(t *testing.T) {
	marker := ` # {"$argo-watcher": "demo:app"}`
	testCases := []struct {
		name    string
		src     string
		tag     string
		wantErr string
	}{
		{"no marker for the alias", "tag: v1 # {\"$argo-watcher\": \"demo:other\"}\n", "v2", `no field is marked {"$argo-watcher": "demo:app"}`},
		{"marker for another application", "tag: v1 # {\"$argo-watcher\": \"other:app\"}\n", "v2", "no field is marked"},
		{"marker as a head comment does not count", "# {\"$argo-watcher\": \"demo:app\"}\ntag: v1\n", "v2", "no field is marked"},
		{"malformed marker", "tag: v1 # {\"$argo-watcher\": }\n", "v2", "malformed"},
		{"tag with a newline", "tag: v1" + marker + "\n", "v2\ninjected: true", "not a valid image tag"},
		{"tag with a comment char", "tag: v1" + marker + "\n", "v2 #x", "not a valid image tag"},
		{"tag starting with a dash", "tag: v1" + marker + "\n", "-v2", "not a valid image tag"},
		{"empty tag", "tag: v1" + marker + "\n", "", "not a valid image tag"},
		{"tag over 128 characters", "tag: v1" + marker + "\n", strings.Repeat("a", 129), "not a valid image tag"},
		{"anchored value", "tag: &t v1" + marker + "\nother: *t\n", "v2", "anchor"},
		{"alias value", "base: &t v1\ntag: *t" + marker + "\n", "v2", "alias"},
		{"explicitly tagged value", "tag: !!str v1" + marker + "\n", "v2", "explicit YAML tag"},
		{"quoted value with an escape", `tag: "v\u0031"` + marker + "\n", "v2", "cannot be edited in place"},
		{"marker on a mapping", "image:" + marker + "\n  tag: v1\n", "v2", "must follow a scalar value"},
		{"invalid YAML", "tag: [v1" + marker + "\n", "v2", "failed to parse"},
		{"single quoted value with a doubled quote", `tag: 'it''s'` + marker + "\n", "v2", "cannot be edited in place"},
		{"value inside an anchored mapping", "defaults: &d\n  image:\n    tag: v1" + marker + "\nother:\n  <<: *d\n", "v2", "inside an anchored"},
		{"value inside an anchored sequence", "tags: &t\n  - v1" + marker + "\ncopy: *t\n", "v2", "inside an anchored"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := spliceMarkedTags([]byte(tc.src), "demo", []InlineTagUpdate{{Alias: "app", Tag: tc.tag}})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestSpliceMarkedTags_BlockScalarRefused(t *testing.T) {
	src := "tag: | # {\"$argo-watcher\": \"demo:app\"}\n  v1\n"
	_, err := spliceMarkedTags([]byte(src), "demo", []InlineTagUpdate{{Alias: "app", Tag: "v2"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be edited in place")
}

func TestValidateImageTag(t *testing.T) {
	for _, tag := range []string{"v1", "1.2.3", "_x", "abc-def.1_2", strings.Repeat("a", 128)} {
		assert.NoError(t, ValidateImageTag(tag), tag)
	}
	for _, tag := range []string{"", ".v1", "-v1", "v 1", "v1\n", "v1:x", "v1@sha", strings.Repeat("a", 129)} {
		assert.Error(t, ValidateImageTag(tag), tag)
	}
}

func TestCommitAppInline(t *testing.T) {
	_, remoteRepo, localRepo, _, localPath := setupGitForTest(t)

	repo := newTestRepo(t, &GitClient{})
	repo.localRepo = localRepo
	repo.localRepoPath = localPath

	require.NoError(t, os.MkdirAll(filepath.Join(localPath, "apps"), 0755))
	manifestPath := filepath.Join(localPath, "apps", "demo.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(applicationManifest), 0644))

	updates := []InlineTagUpdate{{Alias: "app", Tag: "2.0.0"}}

	committed, err := repo.CommitAppInline("demo", "apps", "demo.yaml", updates, nil)
	require.NoError(t, err)
	assert.True(t, committed)
	require.NoError(t, repo.Push(t.Context()))

	head, err := remoteRepo.Head()
	require.NoError(t, err)
	commit, err := remoteRepo.CommitObject(head.Hash())
	require.NoError(t, err)
	file, err := commit.File("apps/demo.yaml")
	require.NoError(t, err)
	contents, err := file.Contents()
	require.NoError(t, err)
	assert.Contains(t, contents, `tag: 2.0.0  # {"$argo-watcher": "demo:app"}`)

	committed, err = repo.CommitAppInline("demo", "apps", "demo.yaml", updates, nil)
	require.NoError(t, err)
	assert.False(t, committed, "an unchanged tag must report not committed so the push is skipped")
}

func TestCommitAppInline_MissingFile(t *testing.T) {
	repo := newTestRepo(t, nil)
	repo.localRepoPath = t.TempDir()

	_, err := repo.CommitAppInline("demo", "apps", "demo.yaml", []InlineTagUpdate{{Alias: "app", Tag: "v1"}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read")
}

func TestCommitAppInline_SymlinkOutsideRepoRejected(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "victim.yaml")
	original := "tag: v1 # {\"$argo-watcher\": \"demo:app\"}\n"
	require.NoError(t, os.WriteFile(outside, []byte(original), 0644))

	for name, link := range map[string]string{"file": "demo.yaml", "directory": "linked"} {
		t.Run(name, func(t *testing.T) {
			repo := newTestRepo(t, nil)
			repo.localRepoPath = t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(repo.localRepoPath, "apps"), 0755))
			target := outside
			if name == "directory" {
				target = filepath.Dir(outside)
			}
			require.NoError(t, os.Symlink(target, filepath.Join(repo.localRepoPath, "apps", link)))

			fileName := "demo.yaml"
			if name == "directory" {
				fileName = "linked/victim.yaml"
			}
			_, err := repo.CommitAppInline("demo", "apps", fileName, []InlineTagUpdate{{Alias: "app", Tag: "v2"}}, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not inside repository root")

			content, err := os.ReadFile(outside)
			require.NoError(t, err)
			assert.Equal(t, original, string(content), "a file outside the clone must never be written")
		})
	}
}

func TestCommitAppInline_SymlinkedCloneRoot(t *testing.T) {
	_, remoteRepo, localRepo, _, localPath := setupGitForTest(t)
	linkedRoot := filepath.Join(t.TempDir(), "cache")
	require.NoError(t, os.Symlink(localPath, linkedRoot))

	repo := newTestRepo(t, &GitClient{})
	repo.localRepo = localRepo
	repo.localRepoPath = linkedRoot
	require.NoError(t, os.MkdirAll(filepath.Join(linkedRoot, "apps"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(linkedRoot, "apps", "demo.yaml"), []byte(applicationManifest), 0644))

	committed, err := repo.CommitAppInline("demo", "apps", "demo.yaml", []InlineTagUpdate{{Alias: "app", Tag: "2.0.0"}}, nil)
	require.NoError(t, err)
	assert.True(t, committed)
	require.NoError(t, repo.Push(t.Context()))

	head, err := remoteRepo.Head()
	require.NoError(t, err)
	commit, err := remoteRepo.CommitObject(head.Hash())
	require.NoError(t, err)
	file, err := commit.File("apps/demo.yaml")
	require.NoError(t, err)
	contents, err := file.Contents()
	require.NoError(t, err)
	assert.Contains(t, contents, `tag: 2.0.0  # {"$argo-watcher": "demo:app"}`)
}

func TestCommitAppInline_SymlinkInsideRepoFollowed(t *testing.T) {
	_, _, localRepo, _, localPath := setupGitForTest(t)
	repo := newTestRepo(t, &GitClient{})
	repo.localRepo = localRepo
	repo.localRepoPath = localPath

	require.NoError(t, os.MkdirAll(filepath.Join(localPath, "shared"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(localPath, "apps"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(localPath, "shared", "demo.yaml"), []byte(applicationManifest), 0644))
	link := filepath.Join(localPath, "apps", "demo.yaml")
	require.NoError(t, os.Symlink("../shared/demo.yaml", link))

	committed, err := repo.CommitAppInline("demo", "apps", "demo.yaml", []InlineTagUpdate{{Alias: "app", Tag: "2.0.0"}}, nil)
	require.NoError(t, err)
	assert.True(t, committed)

	content, err := os.ReadFile(filepath.Join(localPath, "shared", "demo.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(content), `tag: 2.0.0  # {"$argo-watcher": "demo:app"}`)
	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link itself must stay a link")
}

func TestPlainTagNeedsQuotes(t *testing.T) {
	for _, tag := range []string{"on", "On", "yes", "no", "off", "y", "n", "1_000", "0x1F", "0o17", "0b101", "1e3", "null", "1.10", "123"} {
		assert.True(t, plainTagNeedsQuotes(tag), tag)
	}
	for _, tag := range []string{"v1", "1.2.3", "abc-def", "latest", "main_1", "abc1234"} {
		assert.False(t, plainTagNeedsQuotes(tag), tag)
	}
}

func TestCommitAppInline_PathTraversalRejected(t *testing.T) {
	repo := newTestRepo(t, nil)
	repo.localRepoPath = t.TempDir()

	_, err := repo.CommitAppInline("demo", "apps", "../../../../etc/passwd", []InlineTagUpdate{{Alias: "app", Tag: "v1"}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not inside repository root")
}

func TestVerifySplice_RejectsAnyOtherChange(t *testing.T) {
	marker := ` # {"$argo-watcher": "demo:app"}`
	original := "tag: v1" + marker + "\nother: x\n"
	updates := []InlineTagUpdate{{Alias: "app", Tag: "v2"}}

	require.NoError(t, verifySplice([]byte(original), []byte("tag: v2"+marker+"\nother: x\n"), "demo", updates))

	err := verifySplice([]byte(original), []byte("tag: v2"+marker+"\nother: y\n"), "demo", updates)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed more than the marked fields")

	err = verifySplice([]byte(original), []byte("tag: 2"+marker+"\nother: x\n"), "demo", []InlineTagUpdate{{Alias: "app", Tag: "2"}})
	require.Error(t, err, "a tag read back as a number must be rejected")

	err = verifySplice([]byte(original), []byte("tag: v2"+marker+"\nother: x\n---\nextra: 1\n"), "demo", updates)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "number of YAML documents")
}

func TestSpliceMarkedTags_AnotherAppsBadMarkerDoesNotBlock(t *testing.T) {
	ours := "tag: v1 # {\"$argo-watcher\": \"demo:app\"}\n"
	for name, theirs := range map[string]string{
		"anchored":  "defaults: &d\n  tag: x # {\"$argo-watcher\": \"other:app\"}\ncopy: *d\n",
		"on a key":  "image: # {\"$argo-watcher\": \"other:app\"}\n  tag: x\n",
		"malformed": "tag2: x # {\"$argo-watcher\": }\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := spliceMarkedTags([]byte(ours+theirs), "demo", []InlineTagUpdate{{Alias: "app", Tag: "v2"}})
			require.NoError(t, err)
			assert.Equal(t, "tag: v2 # {\"$argo-watcher\": \"demo:app\"}\n"+theirs, string(got))
		})
	}
}

func TestCommitAppInline_UnreadableManifest(t *testing.T) {
	repo := newTestRepo(t, nil)
	repo.localRepoPath = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo.localRepoPath, "apps", "demo.yaml"), 0755))

	_, err := repo.CommitAppInline("demo", "apps", "demo.yaml", []InlineTagUpdate{{Alias: "app", Tag: "v1"}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read application manifest")
}

func TestCommitAppInline_UnmarkedAlias(t *testing.T) {
	repo := newTestRepo(t, nil)
	repo.localRepoPath = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo.localRepoPath, "apps"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repo.localRepoPath, "apps", "demo.yaml"), []byte(applicationManifest), 0644))

	_, err := repo.CommitAppInline("demo", "apps", "demo.yaml", []InlineTagUpdate{{Alias: "missing", Tag: "v1"}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot update apps/demo.yaml")
}

func TestResolveInsideRoot_MissingRoot(t *testing.T) {
	_, err := resolveInsideRoot(filepath.Join(t.TempDir(), "gone"), "demo.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve repository root")
}

func TestByteOffset(t *testing.T) {
	content := []byte("ab\n\xffc\n")
	lineStarts := lineOffsets(content)

	offset, ok := byteOffset(content, lineStarts, 1, 2)
	assert.True(t, ok)
	assert.Equal(t, 1, offset)

	for name, pos := range map[string][2]int{
		"line before the file": {0, 1},
		"line past the file":   {9, 1},
		"column zero":          {1, 0},
		"column past the line": {1, 5},
		"invalid UTF-8":        {2, 2},
	} {
		_, ok := byteOffset(content, lineStarts, pos[0], pos[1])
		assert.False(t, ok, name)
	}
}

func TestVerifySplice_UnparsableInput(t *testing.T) {
	updates := []InlineTagUpdate{{Alias: "app", Tag: "v2"}}

	require.Error(t, verifySplice([]byte("tag: [v1\n"), []byte("tag: v2\n"), "demo", updates))

	err := verifySplice([]byte("tag: v1\n"), []byte("tag: [v2\n"), "demo", updates)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no longer parses")
}
