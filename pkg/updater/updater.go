package updater

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"text/template"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

var (
	sshKeyPath          = os.Getenv("SSH_KEY_PATH")
	sshKeyPass          = os.Getenv("SSH_KEY_PASS")
	sshCommitUser       = os.Getenv("SSH_COMMIT_USER")
	sshCommitMail       = os.Getenv("SSH_COMMIT_MAIL")
	commitMessageFormat = os.Getenv("COMMIT_MESSAGE_FORMAT")
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
	FileName   string
	fs         billy.Filesystem
	localRepo  *git.Repository
	sshAuth    *ssh.PublicKeys

	GitHandler GitHandler
}

func (repo *GitRepo) Clone() error {
	var err error

	repo.fs = memfs.New()

	if repo.sshAuth, err = repo.GitHandler.AddSSHKey("git", sshKeyPath, sshKeyPass); err != nil {
		return err
	}

	repo.localRepo, err = repo.GitHandler.Clone(memory.NewStorage(), repo.fs, &git.CloneOptions{
		URL:           repo.RepoURL,
		ReferenceName: plumbing.ReferenceName("refs/heads/" + repo.BranchName),
		SingleBranch:  true,
		Depth:         1, // This is needed to avoid fetching the entire history, which is not needed in this case
		Auth:          repo.sshAuth,
	})

	return err
}

func (repo *GitRepo) generateOverrideFileName(appName string) string {
	if repo.FileName == "" {
		return fmt.Sprintf("%s/.argocd-source-%s.yaml", repo.Path, appName)
	}
	return fmt.Sprintf("%s/%s", repo.Path, repo.FileName)
}

func (repo *GitRepo) generateCommitMessage(appName string, tmplData any) (string, error) {
	commitMsg := fmt.Sprintf("argo-watcher(%s): update image tag", appName)

	if commitMessageFormat == "" {
		return commitMsg, nil
	}

	tmpl, err := template.New("commitMsg").Parse(commitMessageFormat)
	if err != nil {
		return commitMsg, err
	}

	var message bytes.Buffer
	if err = tmpl.Execute(&message, tmplData); err != nil {
		return commitMsg, err
	}

	return message.String(), nil
}

func (repo *GitRepo) UpdateApp(appName string, overrideContent *ArgoOverrideFile, tmplData any) error {
	overrideFileName := repo.generateOverrideFileName(appName)

	commitMsg, err := repo.generateCommitMessage(appName, tmplData)
	if err != nil {
		return err
	}

	log.Debug().Msgf("Updating override file: %s", overrideFileName)

	overrideContent, err = repo.mergeOverrideFileContent(overrideFileName, overrideContent)
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

	content, err := repo.getFileContent(overrideFileName)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(content, &existingOverrideFile); err != nil {
		return nil, err
	}

	mergeParameters(&existingOverrideFile, overrideContent)

	return &existingOverrideFile, nil
}

func (repo *GitRepo) getFileContent(filename string) ([]byte, error) {
	tmp, err := repo.fs.Open(filename)
	if err != nil {
		return nil, err
	}

	defer func(tmp billy.File) {
		err := tmp.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(tmp)

	return io.ReadAll(tmp)
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

	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)

	if err := encoder.Encode(overrideContent); err != nil {
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	worktree, err := repo.localRepo.Worktree()
	if err != nil {
		return err
	}

	if changed, err := versionChanged(worktree); err != nil {
		return err
	} else if !changed {
		return nil
	}

	if _, err = worktree.Add(fileName); err != nil {
		return err
	}

	commitOpts := &git.CommitOptions{
		Author: &object.Signature{
			Name:  sshCommitUser,
			Email: sshCommitMail,
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

// versionChanged checks if the override file has changed.
func versionChanged(worktree *git.Worktree) (bool, error) {
	status, err := worktree.Status()
	if err != nil {
		return false, err
	}

	if status.IsClean() {
		log.Debug().Msg("No changes detected. Skipping commit.")
		return false, nil
	}
	return true, nil
}

// mergeParameters merges the parameters from the new override file into the existing override file.
func mergeParameters(existing *ArgoOverrideFile, newContent *ArgoOverrideFile) {
	for _, newParam := range newContent.Helm.Parameters {
		found := false
		for idx, existingParam := range existing.Helm.Parameters {
			if existingParam.Name == newParam.Name {
				// Update existing parameter
				existing.Helm.Parameters[idx] = newParam
				found = true
				break
			}
		}
		// If parameter with the same name doesn't exist, append it
		if !found {
			existing.Helm.Parameters = append(existing.Helm.Parameters, newParam)
		}
	}
}
