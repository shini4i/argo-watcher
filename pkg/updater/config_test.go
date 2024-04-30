package updater

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewGitRepo(t *testing.T) {
	repoURL := "https://github.com/test/repo.git"
	branchName := "main"
	path := "/path/to/repo"
	fileName := "override.yaml"

	dummyClient := GitClient{}

	repo := NewGitRepo(repoURL, branchName, path, fileName, dummyClient)

	assert.Equal(t, repoURL, repo.RepoURL)
	assert.Equal(t, branchName, repo.BranchName)
	assert.Equal(t, path, repo.Path)
	assert.Equal(t, fileName, repo.FileName)
	assert.Equal(t, dummyClient, repo.GitHandler)
}
