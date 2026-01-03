package updater

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

// ErrSSHKeyNotProvided is returned when the SSH key file path is empty.
var ErrSSHKeyNotProvided = errors.New("SSH key path is empty")

// ErrSSHKeyNotFound is returned when the SSH key file does not exist.
var ErrSSHKeyNotFound = errors.New("SSH key file not found")

// ErrSSHKeyEmpty is returned when the SSH key file is empty.
var ErrSSHKeyEmpty = errors.New("SSH key file is empty")

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
	// Check if path is provided
	if path == "" {
		return ErrSSHKeyNotProvided
	}

	// Normalize the path to prevent directory traversal attacks
	path = filepath.Clean(path)

	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrSSHKeyNotFound, path)
		}
		return fmt.Errorf("failed to stat SSH key file: %w", err)
	}

	// Check if path points to a file (not a directory)
	if info.IsDir() {
		return fmt.Errorf("SSH key path is a directory, not a file: %s", path)
	}

	// Check if file is not empty
	if info.Size() == 0 {
		return fmt.Errorf("%w: %s", ErrSSHKeyEmpty, path)
	}

	// Read and validate key format
	keyData, err := os.ReadFile(path) // #nosec G304
	if err != nil {
		return fmt.Errorf("failed to read SSH key file: %w", err)
	}

	// Attempt to parse the private key to validate its format
	_, err = cryptossh.ParsePrivateKey(keyData)
	if err != nil {
		// Check if it's a passphrase-protected key
		var passphraseErr *cryptossh.PassphraseMissingError
		if errors.As(err, &passphraseErr) {
			// Key is valid but encrypted - this is OK, the passphrase will be used later
			return nil
		}
		return fmt.Errorf("invalid SSH key format: %w", err)
	}

	return nil
}
