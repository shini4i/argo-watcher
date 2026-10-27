package updater

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// inlineMarkerKey is the key of the JSON line comment that marks a field for inline
// write-back: `tag: 1.0.0 # {"$argo-watcher": "<app>:<alias>"}`.
const inlineMarkerKey = "$argo-watcher"

// imageTagPattern is the OCI distribution spec's tag grammar. Anything outside it
// could carry YAML syntax into a hand-maintained file.
var imageTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

// yaml11Booleans are the plain words a YAML 1.1 parser reads as booleans, which a
// YAML 1.2 parser mostly reads as strings.
var yaml11Booleans = map[string]bool{
	"y": true, "yes": true, "n": true, "no": true,
	"true": true, "false": true, "on": true, "off": true,
}

// InlineTagUpdate is one managed image's new tag, written to every field marked
// `{"$argo-watcher": "<app>:<Alias>"}`.
type InlineTagUpdate struct {
	Alias string
	Tag   string
}

// scalarEdit replaces length bytes at offset with replacement.
type scalarEdit struct {
	offset      int
	length      int
	replacement string
}

// ValidateImageTag returns an error unless tag matches the OCI tag grammar.
func ValidateImageTag(tag string) error {
	if !imageTagPattern.MatchString(tag) {
		return fmt.Errorf("%q is not a valid image tag", tag)
	}
	return nil
}

// CommitAppInline sets the marked fields of an existing file to the new tags and
// commits it locally WITHOUT pushing, keeping every other byte. It reports whether a
// commit was made, and fails on a missing file, an invalid tag, an alias with no
// marked field, or a marked field that cannot be rewritten in place.
func (repo *GitRepo) CommitAppInline(appName, path, fileName string, updates []InlineTagUpdate, tmplData any) (bool, error) {
	fullPath, err := resolveInsideRoot(repo.localRepoPath, filepath.Join(path, fileName))
	if err != nil {
		return false, err
	}

	content, err := os.ReadFile(fullPath) // #nosec G304 -- path validated by resolveInsideRoot above
	if err != nil {
		return false, fmt.Errorf("failed to read application manifest: %w", err)
	}

	updated, err := spliceMarkedTags(content, appName, updates)
	if err != nil {
		return false, fmt.Errorf("cannot update %s: %w", filepath.Join(path, fileName), err)
	}

	slog.Debug("Updating marked fields in application manifest", "path", fullPath)
	return repo.commitLocal(fullPath, repo.generateCommitMessage(appName, tmplData), updated)
}

// resolveInsideRoot follows symlinks in relPath and returns the target as a path
// under root, so git can stage it. It errors when the file is missing or resolves
// outside root: the repository's content is not trusted to keep a write inside it.
func resolveInsideRoot(root, relPath string) (string, error) {
	fullPath := filepath.Join(root, relPath)
	if err := assertInsideRoot(root, fullPath); err != nil {
		return "", err
	}

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve repository root: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read application manifest: %w", err)
	}
	if err := assertInsideRoot(realRoot, realPath); err != nil {
		return "", err
	}

	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil {
		return "", fmt.Errorf("could not determine relative path: %w", err)
	}
	return filepath.Join(root, rel), nil
}

// markedField is one field carrying a marker. err is set when the marker is placed
// where the field cannot be edited; it only matters to the app the marker names.
type markedField struct {
	node *yaml.Node
	err  error
}

// markedFields holds every marker of a file, keyed by its "<app>:<alias>" value.
// Malformed markers name no app, so they are kept apart.
type markedFields struct {
	byMarker  map[string][]markedField
	malformed []error
}

// spliceMarkedTags returns content with every field marked for appName and an
// update's alias set to that tag. It edits raw bytes so the rest of the file is kept,
// then re-parses the result to prove only the marked fields changed.
func spliceMarkedTags(content []byte, appName string, updates []InlineTagUpdate) ([]byte, error) {
	marked, err := findMarkedFields(content)
	if err != nil {
		return nil, err
	}

	lineStarts := lineOffsets(content)
	var edits []scalarEdit
	for _, update := range updates {
		if err := ValidateImageTag(update.Tag); err != nil {
			return nil, err
		}

		marker := appName + ":" + update.Alias
		fields := marked.byMarker[marker]
		if len(fields) == 0 {
			// A malformed marker may have been meant for this app, so name it too.
			noField := fmt.Errorf("no field is marked {%q: %q}", inlineMarkerKey, marker)
			return nil, errors.Join(append([]error{noField}, marked.malformed...)...)
		}

		for _, field := range fields {
			if field.err != nil {
				return nil, field.err
			}
			edit, err := planScalarEdit(content, lineStarts, field.node, update.Tag)
			if err != nil {
				return nil, err
			}
			edits = append(edits, edit)
		}
	}

	// Apply from the end of the file so earlier offsets stay valid.
	slices.SortFunc(edits, func(a, b scalarEdit) int { return b.offset - a.offset })
	updated := slices.Clone(content)
	for _, edit := range edits {
		updated = slices.Replace(updated, edit.offset, edit.offset+edit.length, []byte(edit.replacement)...)
	}

	if err := verifySplice(content, updated, appName, updates); err != nil {
		return nil, err
	}
	if len(marked.malformed) > 0 {
		slog.Warn("Ignored malformed argo-watcher markers", "app", appName, "error", errors.Join(marked.malformed...))
	}
	return updated, nil
}

// findMarkedFields parses every YAML document in content and returns its markers.
// The nodes belong to the parsed documents, which the caller may modify.
func findMarkedFields(content []byte) (*markedFields, error) {
	documents, err := decodeDocuments(content)
	if err != nil {
		return nil, err
	}

	marked := &markedFields{byMarker: map[string][]markedField{}}
	for _, document := range documents {
		collectMarkedFields(document, false, false, marked)
	}
	return marked, nil
}

// decodeDocuments decodes every document of a YAML stream.
func decodeDocuments(content []byte) ([]*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	var documents []*yaml.Node
	for {
		var document yaml.Node
		if err := decoder.Decode(&document); err != nil {
			if errors.Is(err, io.EOF) {
				return documents, nil
			}
			return nil, fmt.Errorf("failed to parse YAML: %w", err)
		}
		documents = append(documents, &document)
	}
}

// collectMarkedFields records each marker under node. A marker is editable only as
// the line comment of a scalar value outside any anchored collection (an alias would
// copy the edit elsewhere); any other placement is recorded as an error for its app.
func collectMarkedFields(node *yaml.Node, isKey, underAnchor bool, marked *markedFields) {
	if strings.Contains(node.LineComment, strconv.Quote(inlineMarkerKey)) {
		if marker, err := parseMarker(node); err != nil {
			marked.malformed = append(marked.malformed, err)
		} else {
			field := markedField{node: node}
			switch {
			case isKey || (node.Kind != yaml.ScalarNode && node.Kind != yaml.AliasNode):
				field.err = fmt.Errorf("the %s marker on line %d must follow a scalar value", inlineMarkerKey, node.Line)
			case underAnchor:
				field.err = fmt.Errorf("the marked field on line %d is inside an anchored collection; editing it would change every alias of it", node.Line)
			}
			marked.byMarker[marker] = append(marked.byMarker[marker], field)
		}
	}

	childUnderAnchor := underAnchor || (node.Kind != yaml.ScalarNode && node.Anchor != "")
	for i, child := range node.Content {
		childIsKey := node.Kind == yaml.MappingNode && i%2 == 0
		collectMarkedFields(child, childIsKey, childUnderAnchor, marked)
	}
}

// parseMarker returns the "<app>:<alias>" value of a node's marker comment.
func parseMarker(node *yaml.Node) (string, error) {
	raw := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(node.LineComment), "#"))

	var fields map[string]string
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields[inlineMarkerKey] == "" {
		return "", fmt.Errorf("malformed %s marker on line %d: %s", inlineMarkerKey, node.Line, raw)
	}
	return fields[inlineMarkerKey], nil
}

// planScalarEdit returns the edit that sets node's value to tag. It refuses a value
// it cannot rewrite as one token: an alias or anchor (the edit would reach other
// fields), an explicit tag, a block scalar, or text that is not a plain or simply
// quoted form of the value.
func planScalarEdit(content []byte, lineStarts []int, node *yaml.Node, tag string) (scalarEdit, error) {
	switch {
	case node.Kind == yaml.AliasNode:
		return scalarEdit{}, fmt.Errorf("the marked field on line %d is a YAML alias; write the value out instead", node.Line)
	case node.Anchor != "":
		return scalarEdit{}, fmt.Errorf("the marked field on line %d carries an anchor; editing it would change every alias of it", node.Line)
	case node.Style&yaml.TaggedStyle != 0:
		return scalarEdit{}, fmt.Errorf("the marked field on line %d has an explicit YAML tag; remove it", node.Line)
	}

	var raw, replacement string
	switch node.Style {
	case yaml.DoubleQuotedStyle:
		raw, replacement = `"`+node.Value+`"`, `"`+tag+`"`
	case yaml.SingleQuotedStyle:
		raw, replacement = `'`+node.Value+`'`, `'`+tag+`'`
	case 0:
		raw, replacement = node.Value, tag
		if plainTagNeedsQuotes(tag) {
			replacement = `"` + tag + `"`
		}
	}

	offset, ok := byteOffset(content, lineStarts, node.Line, node.Column)
	if raw == "" || !ok || !bytes.HasPrefix(content[offset:], []byte(raw)) {
		return scalarEdit{}, fmt.Errorf("the marked field on line %d cannot be edited in place; write it as a single-line value without escapes", node.Line)
	}

	return scalarEdit{offset: offset, length: len(raw), replacement: replacement}, nil
}

// plainTagNeedsQuotes reports whether tag, unquoted, could read as a non-string to a
// YAML 1.2 or YAML 1.1 parser (1.1 adds words like "on" and numbers with
// underscores or base prefixes).
func plainTagNeedsQuotes(tag string) bool {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(tag), &document); err != nil || len(document.Content) != 1 || document.Content[0].ShortTag() != "!!str" {
		return true
	}
	if yaml11Booleans[strings.ToLower(tag)] {
		return true
	}

	digits := strings.ReplaceAll(tag, "_", "")
	if _, err := strconv.ParseInt(digits, 0, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseUint(digits, 0, 64); err == nil {
		return true
	}
	_, err := strconv.ParseFloat(digits, 64)
	return err == nil
}

// lineOffsets returns the byte offset at which each line of content starts.
func lineOffsets(content []byte) []int {
	offsets := []int{0}
	for i, b := range content {
		if b == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

// byteOffset converts yaml.v3's 1-based line and rune column into a byte offset.
func byteOffset(content []byte, lineStarts []int, line, column int) (int, bool) {
	if line < 1 || line > len(lineStarts) || column < 1 {
		return 0, false
	}

	offset := lineStarts[line-1]
	for range column - 1 {
		if offset >= len(content) || content[offset] == '\n' {
			return 0, false
		}
		r, size := utf8.DecodeRune(content[offset:])
		if r == utf8.RuneError && size <= 1 {
			return 0, false
		}
		offset += size
	}
	return offset, true
}

// verifySplice checks that updated decodes to exactly original with the marked
// fields set to their tags as strings, so the byte splice can never change the
// file's meaning anywhere else.
func verifySplice(original, updated []byte, appName string, updates []InlineTagUpdate) error {
	expectedDocs, err := decodeDocuments(original)
	if err != nil {
		return err
	}
	expectedMarked := &markedFields{byMarker: map[string][]markedField{}}
	for _, document := range expectedDocs {
		collectMarkedFields(document, false, false, expectedMarked)
	}
	for _, update := range updates {
		for _, field := range expectedMarked.byMarker[appName+":"+update.Alias] {
			field.node.Value, field.node.Tag, field.node.Style = update.Tag, "!!str", yaml.DoubleQuotedStyle
		}
	}

	updatedDocs, err := decodeDocuments(updated)
	if err != nil {
		return fmt.Errorf("the edited file no longer parses: %w", err)
	}
	if len(updatedDocs) != len(expectedDocs) {
		return errors.New("the edit changed the number of YAML documents")
	}

	for i := range expectedDocs {
		var expected, actual any
		if err := expectedDocs[i].Decode(&expected); err != nil {
			return fmt.Errorf("failed to decode the expected document: %w", err)
		}
		if err := updatedDocs[i].Decode(&actual); err != nil {
			return fmt.Errorf("failed to decode the edited document: %w", err)
		}
		if !reflect.DeepEqual(expected, actual) {
			return errors.New("the edit changed more than the marked fields")
		}
	}
	return nil
}
