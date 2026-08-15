package server

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// safeFileSystem wraps http.Dir with additional symlink protection.
// This provides defense-in-depth beyond http.Dir's built-in path sanitization.
type safeFileSystem struct {
	root     http.Dir
	basePath string
}

func (fs safeFileSystem) validatePath(name string) (string, error) {
	cleanName := filepath.Clean("/" + name)

	fullPath := filepath.Join(fs.basePath, cleanName)

	cleanedFull := filepath.Clean(fullPath)

	// Verify the cleaned path is still within the base directory
	// Check for exact match (root directory) or proper prefix with separator
	if cleanedFull != fs.basePath && !strings.HasPrefix(cleanedFull, fs.basePath+string(filepath.Separator)) {
		slog.Debug("blocked path traversal attempt",
			"requested_path", name,
			"resolved_path", cleanedFull,
			"base_path", fs.basePath)
		return "", os.ErrPermission
	}

	return cleanName, nil
}

// Open rejects any name that escapes the root, including via a symlink.
func (fs safeFileSystem) Open(name string) (http.File, error) {
	cleanName, err := fs.validatePath(name)
	if err != nil {
		return nil, err
	}

	// Open using the validated clean path
	// http.Dir.Open has its own path sanitization as additional protection
	f, err := fs.root.Open(cleanName)
	if err != nil {
		return nil, err
	}

	osFile, ok := f.(*os.File)
	if !ok {
		return f, nil
	}

	realPath, err := filepath.EvalSymlinks(osFile.Name())
	if err != nil {
		_ = f.Close() // #nosec G104 - best effort cleanup
		return nil, err
	}

	if !strings.HasPrefix(realPath, fs.basePath+string(filepath.Separator)) && realPath != fs.basePath {
		slog.Debug("blocked symlink escape attempt",
			"requested_path", name,
			"resolved_path", realPath,
			"base_path", fs.basePath)
		_ = f.Close() // #nosec G104 - best effort cleanup
		return nil, os.ErrPermission
	}

	return f, nil
}

func (env *Env) createStaticFileHandler(fs safeFileSystem, staticPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if tryServeStaticFile(w, r, fs) {
			return
		}
		http.ServeFile(w, r, filepath.Join(staticPath, "index.html"))
	}
}

func tryServeStaticFile(w http.ResponseWriter, r *http.Request, fs safeFileSystem) bool {
	f, err := fs.Open(r.URL.Path)
	if err != nil {
		return false
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		return false
	}

	rs, ok := f.(io.ReadSeeker)
	if !ok {
		return false
	}

	http.ServeContent(w, r, stat.Name(), stat.ModTime(), rs)
	return true
}
