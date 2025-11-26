package history

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/stretchr/testify/require"
)

func TestColumnsFor(t *testing.T) {
	require.Equal(t, []string{
		"id", "app", "project", "status", "created", "updated", "images",
	}, ColumnsFor(true))

	require.Equal(t, []string{
		"id", "app", "project", "status", "created", "updated", "images", "author", "status_reason",
	}, ColumnsFor(false))
}

func TestSanitizeTask(t *testing.T) {
	task := models.Task{
		Id:           "123",
		App:          "demo",
		Project:      "proj",
		Status:       "ok",
		Created:      10,
		Updated:      20,
		Author:       "alice",
		StatusReason: "done",
		Images: []models.Image{
			{Image: "svc", Tag: "1.0"},
			{Image: "worker", Tag: "2.0"},
		},
	}

	row := SanitizeTask(task, false)
	require.Equal(t, "svc:1.0, worker:2.0", row["images"])
	require.Equal(t, "alice", row["author"])
	require.Equal(t, "done", row["status_reason"])

	anonymized := SanitizeTask(task, true)
	require.NotContains(t, anonymized, "author")
	require.NotContains(t, anonymized, "status_reason")
}

func TestJSONWriter(t *testing.T) {
	buffer := new(bytes.Buffer)
	writer := NewJSONWriter(buffer)

	require.NoError(t, writer.WriteRow(Row{"id": "1"}))
	require.NoError(t, writer.WriteRow(Row{"id": "2"}))
	require.NoError(t, writer.Close())

	var payload []map[string]string
	require.NoError(t, json.Unmarshal(buffer.Bytes(), &payload))
	require.Len(t, payload, 2)
	require.Equal(t, "1", payload[0]["id"])
	require.Equal(t, "2", payload[1]["id"])
}

func TestJSONWriterEmpty(t *testing.T) {
	buffer := new(bytes.Buffer)
	writer := NewJSONWriter(buffer)
	require.NoError(t, writer.Close())
	require.Equal(t, "[]", buffer.String())
}

func TestJSONWriterWriteAfterClose(t *testing.T) {
	buffer := new(bytes.Buffer)
	writer := NewJSONWriter(buffer)
	require.NoError(t, writer.Close())
	require.Error(t, writer.WriteRow(Row{"id": "1"}))
}

func TestCSVWriter(t *testing.T) {
	buffer := new(bytes.Buffer)
	columns := ColumnsFor(false)
	writer := NewCSVWriter(buffer, columns)

	require.NoError(t, writer.WriteRow(Row{
		"id":            "1",
		"app":           "demo",
		"project":       "proj",
		"status":        "ok",
		"created":       float64(10),
		"updated":       float64(20),
		"images":        "svc:1.0",
		"author":        "alice",
		"status_reason": "done",
	}))
	require.NoError(t, writer.Close())

	reader := csv.NewReader(strings.NewReader(buffer.String()))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, columns, records[0])
	require.Equal(t, "demo", records[1][1])
	require.Equal(t, "svc:1.0", records[1][6])
}

func TestCSVWriterWriteAfterClose(t *testing.T) {
	buffer := new(bytes.Buffer)
	writer := NewCSVWriter(buffer, ColumnsFor(false))
	require.NoError(t, writer.Close())
	require.Error(t, writer.WriteRow(Row{"id": "1"}))
}
