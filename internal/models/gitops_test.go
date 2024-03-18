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
