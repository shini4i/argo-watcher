package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunWatcher_InvalidConfig(t *testing.T) {
	assert.Error(t, runWatcher(true, true, false, false))
	assert.Error(t, runWatcher(false, false, false, false))
}

func TestUsage(t *testing.T) {
	// Create a pipe to capture the output
	pr, pw, _ := os.Pipe()
	// Redirect os.Stderr to the pipe writer
	oldStderr := os.Stderr
	os.Stderr = pw

	// Run the usage function
	printUsage()

	// Close the pipe writer and restore os.Stderr
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stderr = oldStderr

	// Read the captured output from the pipe reader
	output, _ := io.ReadAll(pr)

	// Convert the output to string and check for expected values
	expected := []string{
		"Usage: argo-watcher [options]",
		"Invalid mode specified. Please specify either -server, -client or -migration. \nMigration also supports -dry-run\n",
	}

	for _, exp := range expected {
		if !strings.Contains(string(output), exp) {
			t.Errorf("expected output %q not found", exp)
		}
	}
}
