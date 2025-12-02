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

// ExportRow represents a strongly typed export entry.
type ExportRow struct {
	ID           string `json:"id"`
	App          string `json:"app"`
	Project      string `json:"project"`
	Status       string `json:"status"`
	Created      int64  `json:"created"`
	Updated      int64  `json:"updated"`
	Images       string `json:"images"`
	Author       string `json:"author,omitempty"`
	StatusReason string `json:"status_reason,omitempty"`
}

// ToCSV returns the ordered CSV values for the row, respecting anonymization.
func (r ExportRow) ToCSV(anonymize bool) []string {
	values := []string{
		r.ID,
		r.App,
		r.Project,
		r.Status,
		strconv.FormatInt(r.Created, 10),
		strconv.FormatInt(r.Updated, 10),
		r.Images,
	}

	if !anonymize {
		values = append(values, r.Author, r.StatusReason)
	}

	return values
}

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
func SanitizeTask(task models.Task, anonymize bool) ExportRow {
	row := ExportRow{
		ID:      task.Id,
		App:     task.App,
		Project: task.Project,
		Status:  task.Status,
		Created: int64(task.Created),
		Updated: int64(task.Updated),
		Images:  joinImages(task.Images),
	}

	if !anonymize {
		row.Author = task.Author
		row.StatusReason = task.StatusReason
	}

	return row
}

// RowWriter streams rows into a concrete format (JSON, CSV, etc.).
type RowWriter interface {
	WriteRow(ExportRow) error
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
func (w *JSONWriter) WriteRow(row ExportRow) error {
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
	anonymize   bool
}

// NewCSVWriter initialises a CSV writer that writes the supplied header and rows to destination.
func NewCSVWriter(destination io.Writer, header []string, anonymize bool) *CSVWriter {
	headerCopy := make([]string, len(header))
	copy(headerCopy, header)

	return &CSVWriter{
		writer:    csv.NewWriter(destination),
		header:    headerCopy,
		anonymize: anonymize,
	}
}

// WriteRow writes the header (if necessary) followed by the ordered row values.
func (w *CSVWriter) WriteRow(row ExportRow) error {
	if w.closed {
		return errors.New("csv writer already closed")
	}

	if !w.wroteHeader {
		if err := w.writer.Write(w.header); err != nil {
			return err
		}
		w.wroteHeader = true
	}

	return w.writer.Write(row.ToCSV(w.anonymize))
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
