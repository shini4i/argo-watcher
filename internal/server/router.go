package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
)

const deployLockEndpoint = "/deploy-lock"

const swaggerPrefix = "/swagger"

// CreateRouter initialize router.
func (env *Env) CreateRouter() *chi.Mux {
	// Initialize shutdown channel if not set (for tests that create Env directly)
	if env.shutdownCh == nil {
		env.shutdownCh = make(chan struct{})
	}

	router := chi.NewRouter()
	router.Use(middleware.Recoverer)
	router.Use(env.securityHeaders())
	router.Use(env.corsMiddleware())

	// The upgrade response is written straight to the connection the handler
	// hijacks, so a request that carries no "Upgrade: websocket" header is the only
	// one that can still be answered as ordinary HTTP. Reaching that branch usually
	// means a proxy in front of argo-watcher stripped the header, so log what did
	// arrive to make it diagnosable. Requests that do upgrade are deliberately not
	// logged, since every browser tab hits /ws and reconnects.
	router.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			env.handleWebSocketConnection(w, r)
			return
		}
		slog.Debug("non-upgrade request to /ws",
			"upgrade", r.Header.Get("Upgrade"),
			"connection", r.Header.Get("Connection"))
		writeString(w, http.StatusBadRequest, "WebSocket upgrade required")
	})

	// API routes. The probe endpoints stay unauthenticated: a kubelet cannot
	// perform an OIDC flow, and they expose no state beyond up/down.
	router.Get("/livez", env.livez)
	router.Get("/readyz", env.readyz)
	router.Method(http.MethodGet, "/metrics", promhttp.Handler())

	// Static file serving. The SPA handler is also what answers every unmatched
	// path and every unmatched method, so a deep link into the Web UI loads the
	// application instead of an error.
	staticFilesPath := env.config.StaticFilePath
	slog.Debug("serving frontend assets", "static_path", staticFilesPath)
	staticFS, absStaticPath := mustResolveFileSystem(staticFilesPath)
	spa := env.createStaticFileHandler(staticFS, absStaticPath)

	swaggerFS, _ := mustResolveFileSystem(filepath.Join(staticFilesPath, "swagger"))
	registerSwagger(router, swaggerFS, spa)

	// Routes are grouped by how they authenticate. The reads only the Web UI consumes
	// are gated, which costs no pipeline anything. The rest are open, each for a reason
	// that is not negotiable:
	//   - POST /tasks takes an optional credential by design (docs/reference/api.md).
	//   - GET /config bootstraps the login flow, so it cannot require a token.
	//   - GET /tasks/{id} is exempt while OIDC_REQUIRE_TASK_READ_AUTH is off, so a
	//     client polling it without a credential keeps working; the v4 UUID is the
	//     capability and the enumerable list is protected. Setting that variable moves
	//     the lookup under the same gate as every other read.
	//   - POST/DELETE /deploy-lock enforce privileged membership themselves, and are
	//     registered only under OIDC so they are never an open deploy-freeze switch.
	requireAuth := env.requireAuthenticatedRead()
	router.Route("/api/v1", func(r chi.Router) {
		// No API payload is anywhere near this size, and task submission — the only
		// route that stores a body — takes no credential.
		r.Use(middleware.RequestSize(maxRequestBodyBytes))

		r.Post("/tasks", env.addTask)
		r.Get("/config", env.getConfig)

		if env.config.OIDC.RequireTaskReadAuth {
			r.With(requireAuth).Get("/tasks/{id}", env.getTaskStatus)
		} else {
			r.With(env.countUnauthenticatedRead()).Get("/tasks/{id}", env.getTaskStatus)
		}

		r.With(requireAuth).Get("/tasks", env.getState)
		r.With(requireAuth).Get("/version", env.getVersion)
		// Read-only ArgoCD + state-backend reachability for the frontend
		// "unreachable" banner (issue #498). It exposes no privileged action and
		// mirrors the cached liveness-probe state without a live probe.
		r.With(requireAuth).Get("/reachability", env.reachability)
		r.With(requireAuth).Get(deployLockEndpoint, env.isDeployLockSet)

		if env.config.OIDC.Enabled {
			r.Post(deployLockEndpoint, env.SetDeployLock)
			r.Delete(deployLockEndpoint, env.ReleaseDeployLock)

			// Registered only where the tokens can actually live (see
			// Env.appTokenStrategy). Every one enforces privileged membership itself,
			// so listing tokens is as restricted as issuing them: the list names who
			// holds a credential for which applications.
			if env.appTokens != nil {
				r.Get(appTokensEndpoint, env.listAppTokens)
				r.Post(appTokensEndpoint, env.issueAppToken)
				r.Delete(appTokensEndpoint+"/{id}", env.revokeAppToken)
			}
		}
	})

	fallback := redirectTrailingSlash(router, spa)
	router.NotFound(fallback)
	// An unmatched method resolves to the same fallback as an unmatched path, so a
	// request the API does not serve reaches the Web UI rather than a bare 405.
	router.MethodNotAllowed(fallback)

	return router
}

// mustResolveFileSystem builds a symlink-guarded file system rooted at path,
// returning it alongside the absolute path it serves. Failure to resolve the path
// is fatal, because serving the Web UI from an unknown location is not recoverable.
func mustResolveFileSystem(path string) (safeFileSystem, string) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		slog.Error("failed to resolve static files path", "path", path, "error", err)
		os.Exit(1)
	}

	// Resolve symlinks in the base path to ensure consistent path comparison.
	// This is important on macOS where /var is a symlink to /private/var. A path
	// that does not exist yet is kept as-is.
	resolvedBasePath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		resolvedBasePath = absPath
	}

	return safeFileSystem{root: http.Dir(absPath), basePath: resolvedBasePath}, absPath
}

// registerSwagger mounts the generated API specification, falling back to the SPA
// handler for a path that names no file so a stale bookmark still loads the Web UI.
func registerSwagger(router *chi.Mux, fs safeFileSystem, fallback http.HandlerFunc) {
	fileServer := http.StripPrefix(swaggerPrefix, http.FileServer(fs))

	serve := func(w http.ResponseWriter, r *http.Request) {
		// Probed with the same string the file server will resolve. The routing
		// parameter would be the percent-encoded segment, so a name needing escaping
		// would miss here and be answered with the Web UI instead of the file.
		f, err := fs.Open(strings.TrimPrefix(r.URL.Path, swaggerPrefix))
		if err != nil {
			fallback(w, r)
			return
		}
		_ = f.Close() // #nosec G104 - existence probe only

		fileServer.ServeHTTP(w, r)
	}

	// A bare /swagger resolves to the directory it names, matching how the route
	// behaved when the file server owned the prefix.
	redirect := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, swaggerPrefix+"/", http.StatusMovedPermanently)
	}

	router.Get(swaggerPrefix, redirect)
	router.Head(swaggerPrefix, redirect)
	router.Get(swaggerPrefix+"/*", serve)
	router.Head(swaggerPrefix+"/*", serve)
}

// redirectTrailingSlash returns a handler that redirects "/path/" to "/path" when
// the latter is a route this router serves, and delegates to next otherwise.
//
// Clients and bookmarks rely on the redirect, so it is reproduced here rather than
// applied as blanket middleware: a blanket rule would also rewrite /swagger/, which
// redirects the other way.
func redirectTrailingSlash(router *chi.Mux, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// A leading "//" would make the Location header a protocol-relative URL
		// pointing at whatever host follows it, so only a single-slash-rooted path is
		// ever a redirect candidate.
		if len(path) > 1 && strings.HasSuffix(path, "/") && !strings.HasPrefix(path, "//") {
			trimmed := strings.TrimRight(path, "/")
			if trimmed != "" && router.Match(chi.NewRouteContext(), r.Method, trimmed) {
				code := http.StatusMovedPermanently
				if r.Method != http.MethodGet {
					code = http.StatusTemporaryRedirect
				}

				// Only the path and query are echoed back. A request line may carry an
				// absolute URI, whose host would otherwise reach the Location header and
				// turn this into an open redirect.
				target := trimmed
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				// #nosec G710 -- the target is a path this router matched, carries no
				// host, and cannot start with "//"; see the two guards above.
				http.Redirect(w, r, target, code)
				return
			}
		}

		next(w, r)
	}
}

// StartRouter creates and returns an HTTP server configured with the given router.
// The caller starts it and handles graceful shutdown. The read and write timeouts
// do not reach an established WebSocket: hijacking the connection clears the
// deadlines net/http put on it.
func (env *Env) StartRouter(router http.Handler) *http.Server {
	routerBind := fmt.Sprintf("%s:%s", env.config.Host, env.config.Port)
	slog.Debug("listening", "address", routerBind)
	return &http.Server{
		Addr:              routerBind,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second, // Prevent Slowloris attacks
		ReadTimeout:       30 * time.Second,
		// Generous because it is an absolute deadline on the whole response, and the
		// largest one is the uncompressed Web UI bundle (~1.1 MiB): at 30s a client
		// slower than 38 KB/s would receive a truncated bundle and a blank page.
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

// corsMiddleware returns the CORS policy.
//
// It gates the policy on the origin itself rather than only annotating responses:
// a cross-origin request from an origin outside the allowlist is refused here, so
// it never reaches a handler. Leaving it to the browser would not do — a simple
// request (a text/plain POST needs no preflight) reaches the server whatever the
// browser later does with the response, which for POST /api/v1/tasks means the
// deployment has already happened.
//
// The policy is not applied to the WebSocket handshake: that response is written
// to a hijacked connection, and the WebSocket protocol is not subject to CORS.
func (env *Env) corsMiddleware() func(http.Handler) http.Handler {
	options := env.corsOptions()
	handler := cors.New(options).Handler

	allowed := make(map[string]bool, len(options.AllowedOrigins))
	for _, origin := range options.AllowedOrigins {
		allowed[origin] = true
	}

	return func(next http.Handler) http.Handler {
		wrapped := handler(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// The handshake is exempt; the rest is not a cross-origin request at all,
			// so no CORS headers belong on the response.
			if r.URL.Path == "/ws" || origin == "" || isSameOrigin(origin, r.Host) {
				next.ServeHTTP(w, r)
				return
			}

			if !allowed[origin] {
				slog.Debug("rejecting cross-origin request", "origin", origin, "url", r.URL.Path)
				w.WriteHeader(http.StatusForbidden)
				return
			}

			// A preflight carries the method it is asking about. Without it this is a
			// bare OPTIONS, which is answered directly because the CORS library would
			// otherwise pass it down to the SPA handler. Only the origin is echoed: a
			// browser never asks this way, so there is no method or header list to
			// answer with.
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") == "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.WriteHeader(options.OptionsSuccessStatus)
				return
			}

			wrapped.ServeHTTP(w, r)
		})
	}
}

// isSameOrigin reports whether origin names the host the request was sent to, which
// a browser may still label with an Origin header (the Fetch API does). Compared
// case-insensitively, matching the WebSocket handshake's own check.
func isSameOrigin(origin, host string) bool {
	return strings.EqualFold(origin, "http://"+host) || strings.EqualFold(origin, "https://"+host)
}

// corsOptions returns the CORS policy.
//
// Every entry in AllowedOrigins must stay an exact origin: corsMiddleware matches
// them literally, so a pattern — `"*"` included — would be annotated by the CORS
// library yet refused by the gate in front of it.
func (env *Env) corsOptions() cors.Options {
	options := cors.Options{
		AllowedMethods:       []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:       []string{"Origin", "Content-Type", "Accept", "Authorization", oidcHeader, "ARGO_WATCHER_DEPLOY_TOKEN"},
		ExposedHeaders:       []string{"Content-Length"},
		MaxAge:               int((12 * time.Hour).Seconds()),
		OptionsSuccessStatus: http.StatusNoContent,
	}

	if env.config.DevEnvironment {
		options.AllowedOrigins = []string{
			"http://localhost:3000",
			"http://127.0.0.1:3000",
			"http://localhost:3100",
			"http://127.0.0.1:3100",
			"http://localhost:5173",
			"http://127.0.0.1:5173",
		}
		options.AllowCredentials = true
	} else {
		// No origin is allowed outside dev: the Web UI is served by this same binary
		// and so takes the same-origin branch, and non-browser callers send no Origin.
		// rs/cors reads an empty AllowedOrigins as "allow every origin", so the func is
		// what makes the library agree with the gate rather than depend on it.
		options.AllowedOrigins = nil
		options.AllowOriginFunc = func(string) bool { return false }
	}

	return options
}
