package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewGitopsRepo(t *testing.T) {
	testCases := []struct {
		name      string
		app       Application
		expected  GitopsRepo
		expectErr bool
	}{
		{
			name: "Sources and annotations are set",
			app: Application{
				Metadata: ApplicationMetadata{
					Annotations: map[string]string{
						managedGitRepo:   "https://github.com/test/repo.git",
						managedGitBranch: "main",
						managedGitPath:   "/path/to/code",
					},
				},
				Spec: ApplicationSpec{
					Sources: []ApplicationSource{
						{
							RepoURL:        "SomeOtherRepo",
							TargetRevision: "SomeOtherBranch",
							Path:           "/some/other/path",
						},
					},
				},
			},
			expected: GitopsRepo{
				RepoUrl:    "https://github.com/test/repo.git",
				BranchName: "main",
				Path:       "/path/to/code",
			},
			expectErr: false,
		},
		{
			name: "Sources are set and annotations are missing",
			app: Application{
				Metadata: ApplicationMetadata{
					Annotations: map[string]string{},
				},
				Spec: ApplicationSpec{
					Sources: []ApplicationSource{
						{
							RepoURL:        "SomeOtherRepo",
							TargetRevision: "SomeOtherBranch",
							Path:           "/some/other/path",
						},
					},
				},
			},
			expected: GitopsRepo{
				RepoUrl:    "https://github.com/test/repo.git",
				BranchName: "main",
				Path:       "/path/to/code",
			},
			expectErr: true,
		},
		{
			name: "Single Source and annotations ignored",
			app: Application{
				Metadata: ApplicationMetadata{
					Annotations: map[string]string{
						managedGitRepo:   "SomeOtherRepo",
						managedGitBranch: "SomeOtherBranch",
						managedGitPath:   "/some/other/path",
					},
				},
				Spec: ApplicationSpec{
					Source: ApplicationSource{
						RepoURL:        "https://github.com/test/repo.git",
						TargetRevision: "main",
						Path:           "/path/to/code",
					},
				},
			},
			expected: GitopsRepo{
				RepoUrl:    "https://github.com/test/repo.git",
				BranchName: "main",
				Path:       "/path/to/code",
			},
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := NewGitopsRepo(&tc.app)
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})

	}
}

func TestExtractGitOverrides(t *testing.T) {
	testCases := []struct {
		name        string
		annotations map[string]string
		expected    GitopsRepo
		expectErr   bool
	}{
		{
			name: "All fields present",
			annotations: map[string]string{
				managedGitRepo:   "https://github.com/test/repo.git",
				managedGitBranch: "main",
				managedGitPath:   "/path/to/code",
			},
			expected: GitopsRepo{
				RepoUrl:    "https://github.com/test/repo.git",
				BranchName: "main",
				Path:       "/path/to/code",
			},
			expectErr: false,
		},
		{
			name: "Missing field",
			annotations: map[string]string{
				managedGitRepo:   "https://github.com/test/repo.git",
				managedGitBranch: "main",
			},
			expected:  GitopsRepo{},
			expectErr: true,
		},
		{
			name:        "No fields",
			annotations: map[string]string{},
			expected:    GitopsRepo{},
			expectErr:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := extractGitOverrides(tc.annotations)
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}
