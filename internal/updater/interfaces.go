package updater

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

// ErrSSHKeyNotProvided is returned when the SSH key file path is empty.
var ErrSSHKeyNotProvided = errors.New("SSH key path is empty")

// ErrSSHKeyNotFound is returned when the SSH key file does not exist.
var ErrSSHKeyNotFound = errors.New("SSH key file not found")

// ErrSSHKeyEmpty is returned when the SSH key file is empty.
var ErrSSHKeyEmpty = errors.New("SSH key file is empty")

// IsPermanent reports whether err describes a failure that retrying cannot fix.
//
// Two classes are treated as permanent:
//   - SSH key pre-flight validation errors (ErrSSHKeyNotProvided, ErrSSHKeyNotFound,
//     ErrSSHKeyEmpty): the key is missing or unreadable on every attempt.
//   - Git transport authentication errors (transport.ErrAuthenticationRequired,
//     transport.ErrAuthorizationFailed): the server rejected our credentials and
//     will continue to do so regardless of how many times we retry.
//
// Everything else — network errors, push races, transient git server failures,
// per-attempt context deadlines — is retryable.
func IsPermanent(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrSSHKeyNotProvided) ||
		errors.Is(err, ErrSSHKeyNotFound) ||
		errors.Is(err, ErrSSHKeyEmpty) ||
		errors.Is(err, transport.ErrAuthenticationRequired) ||
		errors.Is(err, transport.ErrAuthorizationFailed)
}

// GitHandler abstracts the small set of git operations the updater needs.
// It exists primarily to enable testing — production code uses GitClient,
// which delegates to go-git directly.
type GitHandler interface {
	// PlainClone clones a repository into path. ctx bounds the network I/O so
	// a hung remote cannot stall the caller indefinitely.
	PlainClone(ctx context.Context, path string, isBare bool, o *git.CloneOptions) (*git.Repository, error)
	PlainOpen(path string) (*git.Repository, error)
	AddSSHKey(user, path, passphrase string) (*ssh.PublicKeys, error)
}

type GitClient struct{}

func (GitClient) PlainClone(ctx context.Context, path string, isBare bool, o *git.CloneOptions) (*git.Repository, error) {
	return git.PlainCloneContext(ctx, path, isBare, o)
}

func (GitClient) PlainOpen(path string) (*git.Repository, error) {
	return git.PlainOpen(path)
}

// AddSSHKey loads and validates an SSH private key from the given file path.
// It performs explicit validation before passing the key to the SSH library
// to ensure any errors are returned gracefully rather than causing a panic.
func (GitClient) AddSSHKey(user, path, passphrase string) (*ssh.PublicKeys, error) {
	if err := validateSSHKeyFile(path); err != nil {
		return nil, err
	}
	return ssh.NewPublicKeysFromFile(user, path, passphrase)
}

// validateSSHKeyFile validates that the SSH key file exists, is readable,
// is not empty, and contains a valid SSH private key format.
func validateSSHKeyFile(path string) error {
	if path == "" {
		return ErrSSHKeyNotProvided
	}

	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrSSHKeyNotFound, path)
		}
		return fmt.Errorf("failed to stat SSH key file: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("SSH key path is a directory, not a file: %s", path)
	}

	if info.Size() == 0 {
		return fmt.Errorf("%w: %s", ErrSSHKeyEmpty, path)
	}

	keyData, err := os.ReadFile(path) // #nosec G304
	if err != nil {
		return fmt.Errorf("failed to read SSH key file: %w", err)
	}

	_, err = cryptossh.ParsePrivateKey(keyData)
	if err != nil {
		// An encrypted key is valid; the passphrase is applied later at clone time.
		var passphraseErr *cryptossh.PassphraseMissingError
		if errors.As(err, &passphraseErr) {
			return nil
		}
		return fmt.Errorf("invalid SSH key format: %w", err)
	}

	return nil
}
