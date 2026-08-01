package beads

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrMalformed marks a line that is not valid JSON. Callers branch on it to
// distinguish a corrupt file from an I/O failure.
var ErrMalformed = errors.New("malformed jsonl")

// maxLineBytes caps one JSONL record. bufio.Scanner's 64KB default is far too
// small: a bead's design field can hold an entire specification, and this
// project's own epic already carries about 14KB. 8MB is generous enough that
// hitting it means the file is not what we think it is.
const maxLineBytes = 8 << 20

// DecodeJSONL reads newline-delimited issue records.
//
// Decoding is deliberately lenient, because bv renders what another tool wrote
// rather than validating it: unknown JSON fields are ignored, unknown status
// and issue_type values are kept verbatim, labels are never checked, and blank
// lines are skipped. Only JSON that does not parse at all is an error, and it
// is reported with its line number.
func DecodeJSONL(r io.Reader) ([]Issue, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var (
		issues []Issue
		line   int
	)

	for scanner.Scan() {
		line++

		raw := bytes.TrimSpace(scanner.Bytes())
		if line == 1 {
			raw = bytes.TrimPrefix(raw, []byte("\ufeff"))
		}
		if len(raw) == 0 {
			continue
		}

		var issue Issue
		if err := json.Unmarshal(raw, &issue); err != nil {
			return nil, fmt.Errorf("%w: line %d: %w", ErrMalformed, line, err)
		}

		issues = append(issues, issue)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read jsonl: %w", err)
	}

	return issues, nil
}

// LoadIssues reads and decodes an issues.jsonl file.
//
// A decode failure is wrapped with a static "decode issues.jsonl:", not the
// dynamic path (I3): the workspace is already implied, so the path only
// pushed the decode reason past whatever a one-line status bar could show.
func LoadIssues(path string) ([]Issue, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("load issues: %w", err)
	}
	defer func() { _ = f.Close() }()

	issues, err := DecodeJSONL(f)
	if err != nil {
		return nil, fmt.Errorf("decode issues.jsonl: %w", err)
	}

	return issues, nil
}
