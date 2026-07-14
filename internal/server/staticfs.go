package server

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// safeFileSystem wraps http.Dir with additional symlink protection.
// This provides defense-in-depth beyond http.Dir's built-in path sanitization.
type safeFileSystem struct {
	root     http.Dir
	basePath string
}

// validatePath checks if a path is safe before any I/O operations.
// Returns the cleaned path if valid, or an error if the path would escape the base directory.
// This performs validation without any I/O operations by checking the cleaned path.
func (fs safeFileSystem) validatePath(name string) (string, error) {
	// Clean the path to remove any .. or . components
	cleanName := filepath.Clean("/" + name)

	// Construct the would-be full path and verify it stays within bounds
	// filepath.Join handles path separators and cleaning
	fullPath := filepath.Join(fs.basePath, cleanName)

	// Clean the full path to resolve any remaining . or .. components
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

// Open implements http.FileSystem interface with path validation and symlink protection.
func (fs safeFileSystem) Open(name string) (http.File, error) {
	// Validate path before any I/O operation
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

	// Additional symlink protection: verify the real path is within bounds
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

// createStaticFileHandler returns a handler for serving static files with SPA fallback.
// It attempts to serve the requested file, falling back to index.html for SPA routing.
func (env *Env) createStaticFileHandler(fs safeFileSystem, staticPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if tryServeStaticFile(c, fs) {
			return
		}
		// Fall back to index.html for SPA routing
		c.File(filepath.Join(staticPath, "index.html"))
	}
}

// tryServeStaticFile attempts to serve a static file and returns true if successful.
func tryServeStaticFile(c *gin.Context, fs safeFileSystem) bool {
	f, err := fs.Open(c.Request.URL.Path)
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

	http.ServeContent(c.Writer, c.Request, stat.Name(), stat.ModTime(), rs)
	return true
}
