package updater

import (
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

type GitHandler interface {
	PlainClone(path string, isBare bool, o *git.CloneOptions) (*git.Repository, error)
	PlainOpen(path string) (*git.Repository, error)
	AddSSHKey(user, path, passphrase string) (*ssh.PublicKeys, error)
}

type GitClient struct{}

func (GitClient) PlainClone(path string, isBare bool, o *git.CloneOptions) (*git.Repository, error) {
	return git.PlainClone(path, isBare, o)
}

func (GitClient) PlainOpen(path string) (*git.Repository, error) {
	return git.PlainOpen(path)
}

func (GitClient) AddSSHKey(user, path, passphrase string) (*ssh.PublicKeys, error) {
	return ssh.NewPublicKeysFromFile(user, path, passphrase)
}
