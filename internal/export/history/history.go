package history

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/shini4i/argo-watcher/internal/models"
)

const (
	columnID           = "id"
	columnApp          = "app"
	columnProject      = "project"
	columnStatus       = "status"
	columnCreated      = "created"
	columnUpdated      = "updated"
	columnImages       = "images"
	columnAuthor       = "author"
	columnStatusReason = "status_reason"
)

// Row represents a serialisable export entry keyed by the output column names.
type Row map[string]any

// ColumnsFor returns the ordered column names that should be used for the export,
// adapting to the anonymisation flag.
func ColumnsFor(anonymize bool) []string {
	columns := []string{
		columnID,
		columnApp,
		columnProject,
		columnStatus,
		columnCreated,
		columnUpdated,
		columnImages,
	}

	if !anonymize {
		columns = append(columns, columnAuthor, columnStatusReason)
	}

	return columns
}

// SanitizeTask flattens a Task into a Row while optionally stripping
// personally identifiable fields.
func SanitizeTask(task models.Task, anonymize bool) Row {
	row := Row{
		columnID:      task.Id,
		columnApp:     task.App,
		columnProject: task.Project,
		columnStatus:  defaultString(task.Status),
		columnCreated: task.Created,
		columnUpdated: task.Updated,
		columnImages:  joinImages(task.Images),
	}

	if !anonymize {
		row[columnAuthor] = task.Author
		row[columnStatusReason] = defaultString(task.StatusReason)
	}

	return row
}

// RowWriter streams rows into a concrete format (JSON, CSV, etc.).
type RowWriter interface {
	WriteRow(Row) error
	Close() error
}

// JSONWriter streams export rows as a JSON array without storing the
// entire dataset in memory.
type JSONWriter struct {
	writer    io.Writer
	started   bool
	completed bool
}

// NewJSONWriter initialises a JSONWriter that streams the payload into the provided writer.
func NewJSONWriter(destination io.Writer) *JSONWriter {
	return &JSONWriter{writer: destination}
}

// WriteRow writes a single row, inserting separators as needed.
func (w *JSONWriter) WriteRow(row Row) error {
	if w.completed {
		return errors.New("json writer already closed")
	}

	payload, err := json.Marshal(row)
	if err != nil {
		return err
	}

	if !w.started {
		if _, err := w.writer.Write([]byte("[")); err != nil {
			return err
		}
		w.started = true
	} else {
		if _, err := w.writer.Write([]byte(",")); err != nil {
			return err
		}
	}

	_, err = w.writer.Write(payload)
	return err
}

// Close finalises the array, ensuring valid JSON even when no rows were written.
func (w *JSONWriter) Close() error {
	if w.completed {
		return nil
	}
	defer func() {
		w.completed = true
	}()

	if !w.started {
		_, err := w.writer.Write([]byte("[]"))
		return err
	}

	_, err := w.writer.Write([]byte("]"))
	return err
}

// CSVWriter streams export rows into CSV format using the provided header order.
type CSVWriter struct {
	writer      *csv.Writer
	header      []string
	wroteHeader bool
	closed      bool
}

// NewCSVWriter initialises a CSV writer that writes the supplied header and rows to destination.
func NewCSVWriter(destination io.Writer, header []string) *CSVWriter {
	headerCopy := make([]string, len(header))
	copy(headerCopy, header)

	return &CSVWriter{
		writer: csv.NewWriter(destination),
		header: headerCopy,
	}
}

// WriteRow writes the header (if necessary) followed by the ordered row values.
func (w *CSVWriter) WriteRow(row Row) error {
	if w.closed {
		return errors.New("csv writer already closed")
	}

	if !w.wroteHeader {
		if err := w.writer.Write(w.header); err != nil {
			return err
		}
		w.wroteHeader = true
	}

	record := make([]string, len(w.header))
	for index, key := range w.header {
		record[index] = stringifyValue(row[key])
	}

	return w.writer.Write(record)
}

// Close flushes the CSV writer so callers can observe any buffered errors.
func (w *CSVWriter) Close() error {
	if w.closed {
		return nil
	}
	w.writer.Flush()
	w.closed = true
	return w.writer.Error()
}

func joinImages(images []models.Image) string {
	if len(images) == 0 {
		return ""
	}

	parts := make([]string, len(images))
	for index, image := range images {
		parts[index] = fmt.Sprintf("%s:%s", image.Image, image.Tag)
	}

	return strings.Join(parts, ", ")
}

func defaultString(value string) string {
	if value == "" {
		return ""
	}
	return value
}

func stringifyValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case bool:
		return strconv.FormatBool(typed)
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}
