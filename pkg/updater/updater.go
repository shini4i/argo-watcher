package updater

import (
	"fmt"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	"gopkg.in/yaml.v2"
	"io"
	"time"
)

type ArgoOverrideFile struct {
	Helm struct {
		Parameters []ArgoParameterOverride `yaml:"parameters"`
	} `yaml:"helm"`
}

type ArgoParameterOverride struct {
	Name        string `yaml:"name"`
	Value       string `yaml:"value"`
	ForceString bool   `yaml:"forceString"`
}

type GitRepo struct {
	RepoURL    string
	BranchName string
	Path       string
	fs         billy.Filesystem
	localRepo  *git.Repository
	sshAuth    *ssh.PublicKeys
}

func (repo *GitRepo) Clone() error {
	var err error

	repo.fs = memfs.New()

	if repo.sshAuth, err = ssh.NewPublicKeysFromFile("git", "/tmp/id_rsa", ""); err != nil {
		return err
	}

	repo.localRepo, err = git.Clone(memory.NewStorage(), repo.fs, &git.CloneOptions{
		URL:           repo.RepoURL,
		ReferenceName: plumbing.ReferenceName("refs/heads/" + repo.BranchName),
		SingleBranch:  true,
		Auth:          repo.sshAuth,
	})

	return err
}

func (repo *GitRepo) UpdateApp(appName string, overrideContent *ArgoOverrideFile) error {
	overrideFileName := fmt.Sprintf("%s/.argocd-source-%s.yaml", repo.Path, appName)
	commitMsg := fmt.Sprintf("argo-watcher(%s): update image tag", appName)

	overrideContent, err := repo.mergeOverrideFileContent(overrideFileName, overrideContent)
	if err != nil {
		return err
	}

	if err := repo.commit(overrideFileName, commitMsg, overrideContent); err != nil {
		return err
	}

	repo.close()

	return nil
}

func (repo *GitRepo) mergeOverrideFileContent(overrideFileName string, overrideContent *ArgoOverrideFile) (*ArgoOverrideFile, error) {
	if !repo.overrideFileExists(overrideFileName) {
		return overrideContent, nil
	}

	existingOverrideFile := ArgoOverrideFile{}
	tmp, err := repo.fs.Open(overrideFileName)
	if err != nil {
		return nil, err
	}

	defer func(tmp billy.File) {
		err := tmp.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(tmp)

	content, err := io.ReadAll(tmp)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(content, &existingOverrideFile); err != nil {
		return nil, err
	}

	for _, newParam := range overrideContent.Helm.Parameters {
		found := false
		for idx, existingParam := range existingOverrideFile.Helm.Parameters {
			if existingParam.Name == newParam.Name {
				// Update existing parameter
				existingOverrideFile.Helm.Parameters[idx] = newParam
				found = true
				break
			}
		}
		// If parameter with the same name doesn't exist, append it
		if !found {
			existingOverrideFile.Helm.Parameters = append(existingOverrideFile.Helm.Parameters, newParam)
		}
	}

	return &existingOverrideFile, nil
}

// overrideFileExists checks if the override file exists in the repository.
func (repo *GitRepo) overrideFileExists(filename string) bool {
	_, err := repo.fs.Stat(filename)
	return err == nil
}

// commit commits the override file to the repository.
func (repo *GitRepo) commit(fileName, commitMsg string, overrideContent *ArgoOverrideFile) error {
	file, err := repo.fs.Create(fileName)
	if err != nil {
		return err
	}

	content, err := yaml.Marshal(overrideContent)
	if err != nil {
		return err
	}

	if _, err = file.Write(content); err != nil {
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	worktree, err := repo.localRepo.Worktree()
	if err != nil {
		return err
	}

	if _, err = worktree.Add(fileName); err != nil {
		return err
	}

	commitOpts := &git.CommitOptions{
		Author: &object.Signature{
			Name:  "argo-watcher",
			Email: "automation@linux-tech.io",
			When:  time.Now(),
		},
	}

	if _, err = worktree.Commit(commitMsg, commitOpts); err != nil {
		return err
	}

	pushOpts := &git.PushOptions{
		Auth:       repo.sshAuth,
		RemoteName: "origin",
	}

	if err = repo.localRepo.Push(pushOpts); err != nil {
		return err
	}

	return nil
}

// close sets both the filesystem and the local repository to nil.
// This will allow the garbage collector to free the memory.
func (repo *GitRepo) close() {
	repo.fs = nil
	repo.localRepo = nil
}
