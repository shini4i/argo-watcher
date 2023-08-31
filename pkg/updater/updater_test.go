package updater

import (
	"errors"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/assert"
	"testing"

	"github.com/shini4i/argo-watcher/pkg/updater/mock"
	"go.uber.org/mock/gomock"
)

func TestGitRepoClone(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockGitHandler := mock.NewMockGitHandler(ctrl)

	tests := []struct {
		name     string
		mockSSH  func()
		expected error
	}{
		{
			name: "successful clone",
			mockSSH: func() {
				mockGitHandler.EXPECT().NewPublicKeysFromFile("git", sshKeyPath, sshKeyPass).Return(&ssh.PublicKeys{}, nil)
				mockGitHandler.EXPECT().Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
					URL:           "mockRepoURL",
					ReferenceName: "refs/heads/mockBranch",
					SingleBranch:  true,
					Auth:          &ssh.PublicKeys{},
				}).Return(&git.Repository{}, nil)
			},
			expected: nil,
		},
		{
			name: "failed NewPublicKeysFromFile",
			mockSSH: func() {
				mockGitHandler.EXPECT().NewPublicKeysFromFile("git", sshKeyPath, sshKeyPass).Return(nil, errors.New("failed to fetch keys"))
			},
			expected: errors.New("failed to fetch keys"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSSH()

			gitRepo := GitRepo{
				RepoURL:    "mockRepoURL",
				BranchName: "mockBranch",
				GitHandler: mockGitHandler,
			}

			err := gitRepo.Clone()

			if tt.expected == nil {
				assert.NoError(t, err, "Expected no error")
			} else {
				assert.EqualError(t, err, tt.expected.Error(), "Error mismatch")
			}
		})
	}
}

func TestMergeParameters(t *testing.T) {
	tests := []struct {
		name     string
		existing ArgoOverrideFile
		new      ArgoOverrideFile
		expected ArgoOverrideFile
	}{
		{
			name:     "Merge with empty existing",
			existing: ArgoOverrideFile{},
			new: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "value1",
						},
					},
				},
			},
			expected: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "value1",
						},
					},
				},
			},
		},
		{
			name: "Overwrite parameter from newContent",
			existing: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "oldValue",
						},
					},
				},
			},
			new: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "newValue",
						},
					},
				},
			},
			expected: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "newValue",
						},
					},
				},
			},
		},
		{
			name: "Append parameter from newContent",
			existing: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "value1",
						},
					},
				},
			},
			new: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param2",
							Value: "value2",
						},
					},
				},
			},
			expected: ArgoOverrideFile{
				Helm: struct {
					Parameters []ArgoParameterOverride `yaml:"parameters"`
				}{
					Parameters: []ArgoParameterOverride{
						{
							Name:  "param1",
							Value: "value1",
						},
						{
							Name:  "param2",
							Value: "value2",
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mergeParameters(&test.existing, &test.new)
			assert.Equal(t, test.expected, test.existing)
		})
	}
}

func TestOverrideFileExists(t *testing.T) {
	tests := []struct {
		name     string
		setupFs  func(fs billy.Filesystem)
		filename string
		expected bool
	}{
		{
			name: "File exists",
			setupFs: func(fs billy.Filesystem) {
				if _, err := fs.Create("/path/to/existing/file.yaml"); err != nil {
					t.Error(err)
				}
			},
			filename: "/path/to/existing/file.yaml",
			expected: true,
		},
		{
			name:     "File does not exist",
			setupFs:  func(fs billy.Filesystem) {},
			filename: "/path/to/nonexistent/file.yaml",
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Setup mock filesystem
			mockFs := memfs.New()
			test.setupFs(mockFs)

			// Initialize GitRepo with mock filesystem
			repo := &GitRepo{
				fs: mockFs,
			}

			got := repo.overrideFileExists(test.filename)
			assert.Equal(t, test.expected, got)
		})
	}
}

func TestGitRepo_Close(t *testing.T) {
	mockFs := memfs.New()

	// Mock a local repo. In a real-world scenario, this would be a valid git.Repository
	mockLocalRepo := &git.Repository{}

	// Initialize an example GitRepo
	repo := &GitRepo{
		fs:        mockFs,
		localRepo: mockLocalRepo,
	}

	// Check if the fs and localRepo fields are initialized
	assert.NotNil(t, repo.fs)
	assert.NotNil(t, repo.localRepo)

	repo.close()

	// Check if the fs and localRepo fields are nil after calling close
	assert.Nil(t, repo.fs)
	assert.Nil(t, repo.localRepo)
}
