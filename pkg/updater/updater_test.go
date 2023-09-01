package updater

import (
	"errors"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/shini4i/argo-watcher/pkg/updater/mock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"gopkg.in/yaml.v2"
	"strings"
	"testing"
	"time"
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
					Depth:         1,
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

func TestGetFileContent(t *testing.T) {
	// 1. Setup an in-memory file system using billy
	fs := memfs.New()
	content := "Hello, World!"
	fileName := "test.txt"

	// 2. Create a test file in that filesystem
	file, err := fs.Create(fileName)
	assert.NoError(t, err)
	_, err = file.Write([]byte(content))
	assert.NoError(t, err)
	err = file.Close()
	assert.NoError(t, err)

	// 3. Create a GitRepo instance using the in-memory filesystem
	repo := &GitRepo{
		fs: fs,
	}

	t.Run("Successfully read content", func(t *testing.T) {
		readContent, err := repo.getFileContent(fileName)
		assert.NoError(t, err)
		assert.Equal(t, content, strings.TrimSpace(string(readContent)))
	})

	t.Run("Error on non-existent file", func(t *testing.T) {
		_, err := repo.getFileContent("non-existent.txt")
		assert.Error(t, err)
	})
}

func TestMergeOverrideFileContent(t *testing.T) {
	fs := memfs.New()
	repo := &GitRepo{
		fs: fs,
	}

	// Test when the override file doesn't exist
	t.Run("no existing file", func(t *testing.T) {
		overrideContent := &ArgoOverrideFile{
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
		}
		result, err := repo.mergeOverrideFileContent("nonexistent.yaml", overrideContent)
		assert.NoError(t, err)
		assert.Equal(t, overrideContent, result)
	})

	// Test when the override file does exist
	t.Run("existing file", func(t *testing.T) {
		// Creating a dummy existing file
		existingContent := ArgoOverrideFile{
			Helm: struct {
				Parameters []ArgoParameterOverride `yaml:"parameters"`
			}{
				Parameters: []ArgoParameterOverride{
					{
						Name:  "param1",
						Value: "oldValue1",
					},
					{
						Name:  "param2",
						Value: "value2",
					},
				},
			},
		}

		fileName := "existing.yaml"
		contentBytes, _ := yaml.Marshal(existingContent)
		file, _ := fs.Create(fileName)
		_, err := file.Write(contentBytes)
		assert.NoError(t, err)
		err = file.Close()
		assert.NoError(t, err)

		// Merge with this content
		overrideContent := &ArgoOverrideFile{
			Helm: struct {
				Parameters []ArgoParameterOverride `yaml:"parameters"`
			}{
				Parameters: []ArgoParameterOverride{
					{
						Name:  "param1",
						Value: "newValue1",
					},
				},
			},
		}

		expectedMergedContent := &ArgoOverrideFile{
			Helm: struct {
				Parameters []ArgoParameterOverride `yaml:"parameters"`
			}{
				Parameters: []ArgoParameterOverride{
					{
						Name:  "param1",
						Value: "newValue1", // This assumes newValue1 overwrites oldValue1
					},
					{
						Name:  "param2",
						Value: "value2",
					},
				},
			},
		}

		result, err := repo.mergeOverrideFileContent(fileName, overrideContent)
		assert.NoError(t, err)
		assert.Equal(t, expectedMergedContent, result)
	})
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

func TestVersionChanged(t *testing.T) {
	t.Run("Repo without changes", func(t *testing.T) {
		fs := memfs.New()
		storer := memory.NewStorage()

		repo, err := git.Init(storer, fs)
		assert.NoError(t, err)

		w, err := repo.Worktree()
		assert.NoError(t, err)

		changed, err := versionChanged(w)
		assert.NoError(t, err)
		assert.False(t, changed, "Expected no changes in a newly initialized repo")
	})

	t.Run("Repo with changes", func(t *testing.T) {
		fs := memfs.New()
		storer := memory.NewStorage()

		repo, err := git.Init(storer, fs)
		assert.NoError(t, err)

		w, err := repo.Worktree()
		assert.NoError(t, err)

		// Create and commit a file
		file, err := fs.Create("test.txt")
		assert.NoError(t, err)
		_, err = file.Write([]byte("Initial content"))
		assert.NoError(t, err)
		err = file.Close()
		assert.NoError(t, err)

		_, err = w.Add("test.txt")
		assert.NoError(t, err)

		_, err = w.Commit("Initial commit", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "John Doe",
				Email: "johndoe@example.com",
				When:  time.Now(),
			},
		})
		assert.NoError(t, err)

		// Make changes to the file
		file, err = fs.Create("test.txt")
		assert.NoError(t, err)
		_, err = file.Write([]byte("Updated content"))
		assert.NoError(t, err)
		err = file.Close()
		assert.NoError(t, err)

		_, err = w.Add("test.txt")
		assert.NoError(t, err)

		// Test versionChanged function
		changed, err := versionChanged(w)
		assert.NoError(t, err)
		assert.True(t, changed, "Expected changes after modifying the file")
	})
}
