package eval

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	apipb "github.com/jsannemo/omogenexec/api"
)

func writeTempFile(t *testing.T, dir, name string, contents []byte) string {
	path := filepath.Join(dir, name)
	if err := ioutil.WriteFile(path, contents, 0644); err != nil {
		t.Fatalf("could not write %s: %v", path, err)
	}
	return path
}

func TestReadPrefix(t *testing.T) {
	dir, err := ioutil.TempDir("", "feedback-test")
	if err != nil {
		t.Fatalf("could not create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	cases := []struct {
		name      string
		contents  []byte
		limit     int
		want      []byte
		truncated bool
	}{
		{name: "empty", contents: []byte{}, limit: 4, want: []byte{}, truncated: false},
		{name: "under", contents: []byte("abc"), limit: 4, want: []byte("abc"), truncated: false},
		{name: "exact", contents: []byte("abcd"), limit: 4, want: []byte("abcd"), truncated: false},
		{name: "over", contents: []byte("abcde"), limit: 4, want: []byte("abcd"), truncated: true},
		{name: "far-over", contents: bytes.Repeat([]byte("x"), 100000), limit: 10, want: []byte("xxxxxxxxxx"), truncated: true},
		{name: "binary", contents: []byte{0, 0xff, 0xfe, '\n', 0}, limit: 4, want: []byte{0, 0xff, 0xfe, '\n'}, truncated: true},
		{name: "zero-limit", contents: []byte("abc"), limit: 0, want: []byte{}, truncated: true},
		{name: "negative-limit", contents: []byte("abc"), limit: -1, want: []byte{}, truncated: true},
	}
	for _, tc := range cases {
		path := writeTempFile(t, dir, tc.name, tc.contents)
		got, truncated, err := readPrefix(path, tc.limit)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if !bytes.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
		if truncated != tc.truncated {
			t.Errorf("%s: got truncated = %v, want %v", tc.name, truncated, tc.truncated)
		}
	}
}

func TestReadPrefixMissingFile(t *testing.T) {
	dir, err := ioutil.TempDir("", "feedback-test")
	if err != nil {
		t.Fatalf("could not create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	if _, _, err := readPrefix(filepath.Join(dir, "does-not-exist"), 10); err == nil {
		t.Errorf("expected an error for a missing file")
	}
}

func TestCaseFeedback(t *testing.T) {
	dir, err := ioutil.TempDir("", "feedback-test")
	if err != nil {
		t.Fatalf("could not create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	writeTempFile(t, dir, "output", []byte("1 2 3\n"))
	writeTempFile(t, dir, "error", []byte("Traceback: something broke\n"))
	tc := &apipb.TestCase{Name: "1", FeedbackLimitBytes: 10}

	feedback := caseFeedback(tc, dir, &execResult{ExitType: exited, ExitCode: 3})
	if feedback.CaseName != "1" {
		t.Errorf("got case name %q, want %q", feedback.CaseName, "1")
	}
	if string(feedback.TeamOutput) != "1 2 3\n" || feedback.TeamOutputTruncated {
		t.Errorf("got output %q (truncated %v), want all of the output", feedback.TeamOutput, feedback.TeamOutputTruncated)
	}
	if string(feedback.TeamError) != "Traceback:" || !feedback.TeamErrorTruncated {
		t.Errorf("got error output %q (truncated %v), want a truncated prefix", feedback.TeamError, feedback.TeamErrorTruncated)
	}
	if feedback.ExitCode != 3 || feedback.Signal != 0 {
		t.Errorf("got exit code %d and signal %d, want 3 and 0", feedback.ExitCode, feedback.Signal)
	}

	feedback = caseFeedback(tc, dir, &execResult{ExitType: signaled, Signal: 11})
	if feedback.ExitCode != 0 || feedback.Signal != 11 {
		t.Errorf("got exit code %d and signal %d, want 0 and 11", feedback.ExitCode, feedback.Signal)
	}

	feedback = caseFeedback(tc, dir, &execResult{ExitType: timedOut})
	if feedback.ExitCode != 0 || feedback.Signal != 0 {
		t.Errorf("got exit code %d and signal %d for a timeout, want 0 and 0", feedback.ExitCode, feedback.Signal)
	}
}

func TestCaseFeedbackMissingOutput(t *testing.T) {
	dir, err := ioutil.TempDir("", "feedback-test")
	if err != nil {
		t.Fatalf("could not create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	tc := &apipb.TestCase{Name: "1", FeedbackLimitBytes: 10}

	// Missing files must never fail the evaluation.
	feedback := caseFeedback(tc, dir, &execResult{ExitType: exited, ExitCode: 1})
	if feedback == nil {
		t.Fatalf("expected feedback even though the output is missing")
	}
	if len(feedback.TeamOutput) != 0 || len(feedback.TeamError) != 0 {
		t.Errorf("expected empty outputs, got %q and %q", feedback.TeamOutput, feedback.TeamError)
	}
}
