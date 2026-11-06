package main

import (
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

// The gate-capture file is what the Operator writes while running the Pre-Tag
// Gates: the attested.* fields of the Verification Record, without their
// prefix, one `key: value` per line. verify checks its shape here, takes the
// names from the record's own key list (there is no second list), and judges the
// values when the record is composed and validated.

// maxCaptureSize bounds the file; a real one is a few hundred bytes.
const maxCaptureSize = 64 << 10

// captureKeys are the six capture names, in the record's order: the attested.*
// keys of recordVerifiedTail without their prefix.
func captureKeys() []string {
	var keys []string
	for _, k := range recordVerifiedTail {
		if name, ok := strings.CutPrefix(k, attestedPrefix); ok {
			keys = append(keys, name)
		}
	}
	return keys
}

// captureRefusal is a refusal of the capture, with a reason from a closed set.
func captureRefusal(reason string) error {
	return refusal{"verify", "the gate capture " + reason}
}

// readCaptureFile reads the capture: a regular file (not a link, a directory or
// a device) of at most maxCaptureSize bytes. An error never names the path.
func readCaptureFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, captureRefusal("is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, captureRefusal("cannot be read")
	}
	defer func() { _ = f.Close() }()
	if open, err := f.Stat(); err != nil || !open.Mode().IsRegular() {
		return nil, captureRefusal("is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCaptureSize+1))
	if err != nil {
		return nil, captureRefusal("cannot be read")
	}
	if len(data) > maxCaptureSize {
		return nil, captureRefusal("is larger than " + strconv.Itoa(maxCaptureSize) + " bytes")
	}
	return data, nil
}

// parseCapture reads the capture under the record's own line rules: printable
// ASCII with LF line ends and a final one, no blank line, and each line
// `key: value` with the value after one space and no space at either end of it
// (a key alone, `key:`, is an empty value). The keys are exactly captureKeys,
// each once, in any order. Values are not judged here. An error never quotes the
// file.
func parseCapture(data []byte) (map[string]string, error) {
	if len(data) == 0 {
		return nil, captureRefusal("is empty")
	}
	for _, c := range data {
		if c != '\n' && (c < 0x20 || c > 0x7e) {
			return nil, captureRefusal("holds a byte outside printable ASCII and LF line ends")
		}
	}
	if data[len(data)-1] != '\n' {
		return nil, captureRefusal("does not end with a line feed")
	}
	want := captureKeys()
	values := map[string]string{}
	for _, text := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if text == "" {
			return nil, captureRefusal("has a blank line")
		}
		key, rest, ok := strings.Cut(text, ":")
		val := ""
		switch {
		case !ok || key == "" || text[0] == ' ':
			return nil, captureRefusal("has a line that is not key: value")
		case rest == "":
		case rest[0] != ' ' || len(rest) == 1 || rest[1] == ' ' || rest[len(rest)-1] == ' ':
			return nil, captureRefusal("has a value that does not follow one space or has a space at its end")
		default:
			val = rest[1:]
		}
		if !slices.Contains(want, key) {
			return nil, captureRefusal("has a key that is not one of the six")
		}
		if _, again := values[key]; again {
			return nil, captureRefusal("repeats a key")
		}
		values[key] = val
	}
	if len(values) != len(want) {
		return nil, captureRefusal("lacks one of the six keys")
	}
	return values, nil
}
