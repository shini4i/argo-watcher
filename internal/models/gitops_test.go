package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractGitOverrides(t *testing.T) {
	testCases := []struct {
		name          string
		annotations   map[string]string
		app           *Application
		isSourceNil   bool
		expected      GitopsRepo
		expectedError bool
	}{
		{
			name:        "Source is nil, Annotations are empty",
			annotations: map[string]string{},
			app: &Application{
				Spec: ApplicationSpec{},
			},
			isSourceNil: true,
			expected: GitopsRepo{
				RepoUrl:    "",
				BranchName: "",
				Path:       "",
				Filename:   "",
			},
			expectedError: true,
		},
		{
			name:        "Source is not nil, Annotations are empty",
			annotations: map[string]string{},
			app: &Application{
				Spec: ApplicationSpec{
					Source: ApplicationSource{
						RepoURL:        "https://github.com/repo2.git",
						TargetRevision: "dev",
						Path:           "/other/path",
					},
				},
			},
			isSourceNil: false,
			expected: GitopsRepo{
				RepoUrl:    "https://github.com/repo2.git",
				BranchName: "dev",
				Path:       "/other/path",
				Filename:   "",
			},
			expectedError: false,
		},
		{
			name: "Annotations are present - Source is nil",
			annotations: map[string]string{
				managedGitRepo:   "https://github.com/test/repo.git",
				managedGitBranch: "main",
				managedGitPath:   "path/to/code",
				managedGitFile:   "file.yaml",
			},
			app: &Application{
				Spec: ApplicationSpec{},
			},
			isSourceNil: true,
			expected: GitopsRepo{
				RepoUrl:    "",
				BranchName: "",
				Path:       "",
				Filename:   "file.yaml",
			},
			expectedError: true,
		},
		{
			name: "Annotations are present - Source is not nil",
			annotations: map[string]string{
				managedGitRepo:   "https://github.com/test/repo.git",
				managedGitBranch: "main",
				managedGitPath:   "path/to/code",
				managedGitFile:   "file.yaml",
			},
			app: &Application{
				Spec: ApplicationSpec{
					Source: ApplicationSource{
						RepoURL:        "https://github.com/repo2.git",
						TargetRevision: "dev",
						Path:           "/other/path",
					},
				},
			},
			isSourceNil: false,
			expected: GitopsRepo{
				RepoUrl:    "https://github.com/test/repo.git",
				BranchName: "main",
				Path:       "path/to/code",
				Filename:   "file.yaml",
			},
			expectedError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := extractGitOverrides(tc.annotations, tc.app, tc.isSourceNil)
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}

func TestNewGitopsRepo_WriteBackMode(t *testing.T) {
	singleSource := ApplicationSpec{Source: ApplicationSource{RepoURL: "https://charts.example.com", TargetRevision: "1.0.0", Path: "chart"}}
	inlineAnnotations := func() map[string]string {
		return map[string]string{
			writeBackMode:    "inline",
			managedGitRepo:   "git@example.com:org/apps.git",
			managedGitBranch: "main",
			managedGitPath:   "apps",
			managedGitFile:   "demo.yaml",
		}
	}

	t.Run("inline mode reads the location from annotations on a single-source app", func(t *testing.T) {
		app := &Application{Metadata: ApplicationMetadata{Annotations: inlineAnnotations()}, Spec: singleSource}
		repo, err := NewGitopsRepo(app, "/cache")
		assert.NoError(t, err)
		assert.Equal(t, GitopsRepo{
			RepoUrl:       "git@example.com:org/apps.git",
			BranchName:    "main",
			Path:          "apps",
			Filename:      "demo.yaml",
			RepoCachePath: "/cache",
			Inline:        true,
		}, repo)
	})

	for _, missing := range []string{managedGitRepo, managedGitBranch, managedGitPath, managedGitFile} {
		t.Run("inline mode requires "+missing, func(t *testing.T) {
			annotations := inlineAnnotations()
			delete(annotations, missing)
			app := &Application{Metadata: ApplicationMetadata{Annotations: annotations}, Spec: singleSource}
			_, err := NewGitopsRepo(app, "/cache")
			assert.ErrorContains(t, err, missing)
		})
	}

	t.Run("override mode is the default and stays on spec.source", func(t *testing.T) {
		annotations := inlineAnnotations()
		annotations[writeBackMode] = "override"
		app := &Application{Metadata: ApplicationMetadata{Annotations: annotations}, Spec: singleSource}
		repo, err := NewGitopsRepo(app, "/cache")
		assert.NoError(t, err)
		assert.False(t, repo.Inline)
		assert.Equal(t, "https://charts.example.com", repo.RepoUrl)
	})

	t.Run("an unknown mode is an error", func(t *testing.T) {
		annotations := inlineAnnotations()
		annotations[writeBackMode] = "inlined"
		app := &Application{Metadata: ApplicationMetadata{Annotations: annotations}, Spec: singleSource}
		_, err := NewGitopsRepo(app, "/cache")
		assert.ErrorContains(t, err, `unknown argo-watcher/write-back-mode "inlined"`)
	})
}
