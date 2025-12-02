package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/internal/export/history"
	"github.com/shini4i/argo-watcher/internal/models"
)

// exportTasks godoc
// @Summary Export historical tasks
// @Description Streams the filtered task history as CSV or JSON.
// @Tags backend, frontend
// @Produce text/csv
// @Produce application/json
// @Param format query string false "Export format (csv or json)" Enums(csv,json)
// @Param anonymize query bool false "Remove author and status_reason columns" default(true)
// @Param from_timestamp query number false "Start timestamp (seconds since epoch, fractional seconds supported)"
// @Param to_timestamp query number false "End timestamp (seconds since epoch, fractional seconds supported)"
// @Param app query string false "Filter by application name"
// @Success 200
// @Failure 400 {object} models.TaskStatus
// @Failure 401 {object} models.TaskStatus
// @Failure 503 {object} models.TaskStatus
// @Router /api/v1/tasks/export [get]
func (env *Env) exportTasks(c *gin.Context) {
	params, reqErr := env.parseExportParams(c)
	if reqErr != nil {
		c.JSON(reqErr.statusCode, models.TaskStatus{
			Status: reqErr.message,
		})
		return
	}

	if reqErr = env.ensureExportAuthorized(c); reqErr != nil {
		c.JSON(reqErr.statusCode, models.TaskStatus{
			Status: reqErr.message,
		})
		return
	}

	lazyWriter := &LazyResponseWriter{ResponseWriter: c.Writer, headerMap: make(http.Header)}
	writer, contentType := buildExportWriter(params.format, params.anonymize, lazyWriter)

	defer func() {
		if err := writer.Close(); err != nil {
			log.Error().Err(err).Msg("failed to flush export writer")
		}
	}()

	now := time.Now().UTC()
	filename := fmt.Sprintf("history-tasks-%s.%s", now.Format("2006-01-02-15-04-05"), params.format)
	lazyWriter.Header().Set("Content-Type", contentType)
	lazyWriter.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	if err := env.streamExportRows(params.startTime, params.endTime, params.app, params.anonymize, writer); err != nil {
		log.Error().Err(err).Msg("failed to stream export rows")
		if !lazyWriter.wroteHeader {
			c.JSON(http.StatusServiceUnavailable, models.TaskStatus{
				Status: "failed to stream export rows",
			})
			return
		}
		return
	}
}

// parseExportParams extracts and validates query parameters for export requests.
func (env *Env) parseExportParams(c *gin.Context) (exportParams, *requestError) {
	params := exportParams{
		format: strings.ToLower(c.DefaultQuery("format", "csv")),
		app:    c.Query("app"),
	}

	if params.format != "csv" && params.format != "json" {
		return params, &requestError{
			statusCode: http.StatusBadRequest,
			message:    "unsupported export format",
		}
	}

	anonymize, err := parseBoolOrDefault(c.Query("anonymize"), true)
	if err != nil {
		return params, &requestError{
			statusCode: http.StatusBadRequest,
			message:    fmt.Sprintf("invalid anonymize flag: %v", err),
		}
	}

	now := time.Now().UTC()
	defaultFrom := now.Add(-24 * time.Hour).Unix()

	params.startTime, err = parseTimestampOrDefault(c.Query("from_timestamp"), float64(defaultFrom))
	if err != nil {
		return params, &requestError{
			statusCode: http.StatusBadRequest,
			message:    fmt.Sprintf("invalid from_timestamp: %v", err),
		}
	}

	params.endTime, err = parseTimestampOrDefault(c.Query("to_timestamp"), float64(now.Unix()))
	if err != nil {
		return params, &requestError{
			statusCode: http.StatusBadRequest,
			message:    fmt.Sprintf("invalid to_timestamp: %v", err),
		}
	}

	if params.endTime < params.startTime {
		return params, &requestError{
			statusCode: http.StatusBadRequest,
			message:    "to_timestamp must be greater than or equal to from_timestamp",
		}
	}

	params.anonymize = anonymize
	if !env.config.Keycloak.Enabled {
		// Without keycloak-provided privilege context, default to anonymized exports.
		params.anonymize = true
	}

	return params, nil
}

// ensureExportAuthorized validates authorization for export requests when authentication is configured.
func (env *Env) ensureExportAuthorized(c *gin.Context) *requestError {
	if !env.hasAuthConfigured() {
		return nil
	}

	header := ""
	if env.config.Keycloak.Enabled {
		header = keycloakHeader
	}

	return env.ensureTokenValid(c, header)
}

// streamExportRows fetches tasks in batches and streams them via the provided writer.
func (env *Env) streamExportRows(start float64, end float64, app string, anonymize bool, writer history.RowWriter) error {
	if env.argo == nil || env.argo.State == nil {
		return fmt.Errorf("task repository is not initialised")
	}

	offset := 0
	for {
		tasks, total := env.argo.State.GetTasks(start, end, app, historyExportBatch, offset)
		if len(tasks) == 0 {
			return nil
		}

		for _, task := range tasks {
			if err := writer.WriteRow(history.SanitizeTask(task, anonymize)); err != nil {
				return err
			}
		}

		offset += len(tasks)

		if offset >= int(total) || len(tasks) < historyExportBatch {
			return nil
		}
	}
}

// buildExportWriter returns the export writer and related content type for a format and anonymization flag.
func buildExportWriter(format string, anonymize bool, writer http.ResponseWriter) (history.RowWriter, string) {
	switch format {
	case "json":
		return history.NewJSONWriter(writer), "application/json"
	default:
		return history.NewCSVWriter(writer, history.ColumnsFor(anonymize), anonymize), "text/csv"
	}
}

func (env *Env) ensureTokenValid(c *gin.Context, header string) *requestError {
	valid, validationErr := env.validateToken(c, header)
	if validationErr != nil {
		log.Error().Err(validationErr).Msg("failed to validate export token")
		return &requestError{
			statusCode: http.StatusInternalServerError,
			message:    "validation process failed",
		}
	}
	if !valid {
		return &requestError{
			statusCode: http.StatusUnauthorized,
			message:    unauthorizedMessage,
		}
	}

	return nil
}

// LazyResponseWriter defers committing headers and status until the first write,
// enabling the handler to return a JSON error if streaming fails before any bytes are sent.
type LazyResponseWriter struct {
	gin.ResponseWriter
	status      int
	headerMap   http.Header
	wroteHeader bool
}

// Header returns the buffered header map without committing it to the underlying writer.
func (w *LazyResponseWriter) Header() http.Header {
	if w.headerMap == nil {
		w.headerMap = make(http.Header)
	}
	return w.headerMap
}

// WriteHeader stores the status code without committing headers yet.
func (w *LazyResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.status = statusCode
}

// Write commits headers on the first call and delegates the body write.
func (w *LazyResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		if w.status == 0 {
			w.status = http.StatusOK
		}
		dest := w.ResponseWriter.Header()
		for key, values := range w.headerMap {
			for _, value := range values {
				dest.Add(key, value)
			}
		}
		w.ResponseWriter.WriteHeader(w.status)
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// WriteString writes a string payload while honoring deferred headers.
func (w *LazyResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// Status returns the pending status code if set, otherwise the underlying writer's status.
func (w *LazyResponseWriter) Status() int {
	if w.status != 0 {
		return w.status
	}
	return w.ResponseWriter.Status()
}

// Written reports whether headers have been committed.
func (w *LazyResponseWriter) Written() bool {
	return w.wroteHeader || w.ResponseWriter.Written()
}

// WriteHeaderNow forces header commit immediately.
func (w *LazyResponseWriter) WriteHeaderNow() {
	// Force header write immediately.
	if !w.wroteHeader {
		if w.status == 0 {
			w.status = http.StatusOK
		}
		dest := w.ResponseWriter.Header()
		for key, values := range w.headerMap {
			for _, value := range values {
				dest.Add(key, value)
			}
		}
		w.ResponseWriter.WriteHeader(w.status)
		w.wroteHeader = true
	}

	if whn, ok := w.ResponseWriter.(interface{ WriteHeaderNow() }); ok {
		whn.WriteHeaderNow()
	}
}

// Flush propagates Flush to the underlying writer when supported.
func (w *LazyResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// requestError represents an HTTP error response that should be returned to the client.
type requestError struct {
	statusCode int
	message    string
}

// Error implements the error interface for requestError.
func (r requestError) Error() string {
	return r.message
}

// exportParams bundles request parameters required for history export.
type exportParams struct {
	format    string
	anonymize bool
	startTime float64
	endTime   float64
	app       string
}
