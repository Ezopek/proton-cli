package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// capCommit is the gate-commit of the healthy capture.
const capCommit = recCommit

// goodCapture is a capture file that passes: the six names of the record's
// attested.* fields, each once.
func goodCapture() string {
	return "gate-commit: " + capCommit + "\n" +
		"gates: test-fast,lint,build,snapshot\n" +
		"lint-diff: none\n" +
		"go-version: 1.27.0\n" +
		"goreleaser-version: 2.17.1\n" +
		"snapshot-version: 5.1.0-ezop.1-SNAPSHOT-0123456\n"
}

func TestCaptureKeysAreTheAttestedFieldsOfTheRecord(t *testing.T) {
	var want []string
	for _, k := range recordVerifiedTail {
		if name, ok := strings.CutPrefix(k, attestedPrefix); ok {
			want = append(want, name)
		}
	}
	if got := captureKeys(); !slices.Equal(got, want) || len(got) != 6 {
		t.Fatalf("keys %v, want %v", got, want)
	}
	// The helper's own file does not spell a second list of them.
	data, err := os.ReadFile("capture.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"snapshot-version"`, `"lint-diff"`, `"gate-commit"`} {
		if strings.Contains(string(data), k) {
			t.Errorf("capture.go spells the key %s", k)
		}
	}
}

func TestParseCapture(t *testing.T) {
	t.Run("the six names in the record's order", func(t *testing.T) {
		got, err := parseCapture([]byte(goodCapture()))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"gate-commit":        capCommit,
			"gates":              "test-fast,lint,build,snapshot",
			"lint-diff":          "none",
			"go-version":         "1.27.0",
			"goreleaser-version": "2.17.1",
			"snapshot-version":   "5.1.0-ezop.1-SNAPSHOT-0123456",
		}
		if len(got) != len(want) {
			t.Fatalf("got %v", got)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s = %q, want %q", k, got[k], v)
			}
		}
	})
	t.Run("the names in any order", func(t *testing.T) {
		lines := strings.Split(strings.TrimSuffix(goodCapture(), "\n"), "\n")
		slices.Reverse(lines)
		if _, err := parseCapture([]byte(strings.Join(lines, "\n") + "\n")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a value is judged later, by the record validator", func(t *testing.T) {
		text := strings.Replace(goodCapture(), "lint-diff: none", "lint-diff: some", 1)
		if _, err := parseCapture([]byte(text)); err != nil {
			t.Fatalf("a bad value was refused as a shape: %v", err)
		}
	})

	good := goodCapture()
	for name, text := range map[string]string{
		"empty":                       "",
		"no final line feed":          strings.TrimSuffix(good, "\n"),
		"CRLF":                        strings.ReplaceAll(good, "\n", "\r\n"),
		"a blank line":                strings.Replace(good, "lint-diff", "\nlint-diff", 1),
		"a trailing blank line":       good + "\n",
		"a missing key":               strings.Replace(good, "lint-diff: none\n", "", 1),
		"every key missing":           "\n",
		"a repeated key":              good + "gates: test-fast,lint,build,snapshot\n",
		"a repeated key with a value": good + "lint-diff: none\n",
		"an unknown key":              good + "extra: 1\n",
		"an unknown key instead":      strings.Replace(good, "lint-diff:", "lint_diff:", 1),
		"the attested. prefix":        strings.Replace(good, "gates:", "attested.gates:", 1),
		"a record header":             "schema: 1\n" + good,
		"a key with different case":   strings.Replace(good, "gates:", "Gates:", 1),
		"no colon":                    strings.Replace(good, "lint-diff: none", "lint-diff none", 1),
		"no space after the colon":    strings.Replace(good, "lint-diff: none", "lint-diff:none", 1),
		"two spaces after the colon":  strings.Replace(good, "lint-diff: none", "lint-diff:  none", 1),
		"a trailing space":            strings.Replace(good, "none\n", "none \n", 1),
		"a leading space on a line":   strings.Replace(good, "lint-diff", " lint-diff", 1),
		"an empty key":                good + ": x\n",
		"a tab":                       strings.Replace(good, "lint-diff: none", "lint-diff: no\tne", 1),
		"a non-ASCII byte":            strings.Replace(good, "none", "none\xc3\xa9", 1),
		"a NUL byte":                  strings.Replace(good, "none", "no\x00ne", 1),
		"a bare carriage return":      strings.Replace(good, "none", "no\rne", 1),
		"a comment line":              "# note\n" + good,
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			got, err := parseCapture([]byte(text))
			if err == nil || got != nil {
				t.Fatalf("accepted: %v", got)
			}
			if strings.Contains(err.Error(), capCommit) || strings.Contains(err.Error(), "SNAPSHOT") {
				t.Fatalf("the error quotes the file: %v", err)
			}
		})
	}
}

func TestReadCaptureFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("a regular file is read", func(t *testing.T) {
		got, err := readCaptureFile(write("ok.txt", []byte(goodCapture())))
		if err != nil || string(got) != goodCapture() {
			t.Fatalf("%q, %v", got, err)
		}
	})
	t.Run("a file of exactly 64 KiB is read", func(t *testing.T) {
		got, err := readCaptureFile(write("max.txt", make([]byte, 64<<10)))
		if err != nil || len(got) != 64<<10 {
			t.Fatalf("%d, %v", len(got), err)
		}
	})
	t.Run("a file of 64 KiB and a byte is refused", func(t *testing.T) {
		if got, err := readCaptureFile(write("big.txt", make([]byte, 64<<10+1))); err == nil || got != nil {
			t.Fatalf("%d, %v", len(got), err)
		}
	})
	t.Run("a missing file, a directory and a symlink are refused", func(t *testing.T) {
		target := write("target.txt", []byte(goodCapture()))
		link := filepath.Join(dir, "link.txt")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{filepath.Join(dir, "missing.txt"), dir, link, ""} {
			if got, err := readCaptureFile(p); err == nil || got != nil {
				t.Fatalf("%q was read", p)
			}
		}
	})
	t.Run("an error never quotes the path", func(t *testing.T) {
		_, err := readCaptureFile(filepath.Join(dir, "SECRET-PATH-MARKER"))
		if err == nil || strings.Contains(err.Error(), "SECRET-PATH-MARKER") {
			t.Fatalf("%v", err)
		}
	})
}
