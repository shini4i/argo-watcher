package updater

import (
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage"
)

type GitHandler interface {
	Clone(s storage.Storer, worktree billy.Filesystem, o *git.CloneOptions) (*git.Repository, error)
	NewPublicKeysFromFile(user, path, passphrase string) (*ssh.PublicKeys, error)
}

type GitClient struct{}

func (GitClient) Clone(s storage.Storer, worktree billy.Filesystem, o *git.CloneOptions) (*git.Repository, error) {
	return git.Clone(s, worktree, o)
}

func (GitClient) NewPublicKeysFromFile(user, path, passphrase string) (*ssh.PublicKeys, error) {
	return ssh.NewPublicKeysFromFile(user, path, passphrase)
}
