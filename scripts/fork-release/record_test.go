package main

import (
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	recTag    = "v5.1.0-ezop.1"
	recCommit = "0123456789abcdef0123456789abcdef01234567"
	recHash   = "89abcdef0123456789abcdef0123456789abcdef0123456789abcdef01234567"
)

// kv is one line of a record under test.
type kv struct{ k, v string }

func renderRecord(lines []kv) []byte {
	var b strings.Builder
	for _, l := range lines {
		if l.v == "" {
			b.WriteString(l.k + ":\n")
			continue
		}
		b.WriteString(l.k + ": " + l.v + "\n")
	}
	return []byte(b.String())
}

// validVerified is a record that passes every rule.
func validVerified() []kv {
	return []kv{
		{"schema", "1"},
		{"tag", recTag},
		{"status", "verified"},
		{"time", "2026-11-06T10:20:30Z"},
		{"commit", recCommit},
		{"asset.checksums.txt", recHash},
		{"asset.proton-cli_linux_amd64", recHash},
		{"asset.proton-cli_windows_amd64.exe", recHash},
		{"exceptions", "checksums.txt"},
		{"host-platform", "linux/amd64"},
		{"proton-version", "5.1.0-ezop.1"},
		{"goreleaser-config-blob", recCommit},
		{"devbox-lock-blob", recCommit},
		{"go-version", "1.27.0"},
		{"goreleaser-version", "2.17.1"},
		{"run-id", "123456789"},
		{"run-attempt", "1"},
		{"run-conclusion", "success"},
		{"attested.gate-commit", recCommit},
		{"attested.gates", "test-fast,lint,build,snapshot"},
		{"attested.lint-diff", "none"},
		{"attested.go-version", "1.27.0"},
		{"attested.goreleaser-version", "2.17.1"},
		{"attested.snapshot-version", "5.1.0-ezop.1-SNAPSHOT-0123456"},
	}
}

func validAbandoned() []kv {
	return []kv{
		{"schema", "1"},
		{"tag", recTag},
		{"status", "abandoned"},
		{"time", "2026-11-06T10:20:30Z"},
		{"reason", "refused"},
	}
}

func setKey(lines []kv, key, value string) []kv {
	out := slices.Clone(lines)
	for i := range out {
		if out[i].k == key {
			out[i].v = value
			return out
		}
	}
	panic("no key " + key)
}

func dropKey(lines []kv, key string) []kv {
	return slices.DeleteFunc(slices.Clone(lines), func(l kv) bool { return l.k == key })
}

func insertBefore(lines []kv, key string, add ...kv) []kv {
	out := slices.Clone(lines)
	i := slices.IndexFunc(out, func(l kv) bool { return l.k == key })
	if i < 0 {
		panic("no key " + key)
	}
	return slices.Insert(out, i, add...)
}

func swapKeys(lines []kv, a, b string) []kv {
	out := slices.Clone(lines)
	i := slices.IndexFunc(out, func(l kv) bool { return l.k == a })
	j := slices.IndexFunc(out, func(l kv) bool { return l.k == b })
	out[i], out[j] = out[j], out[i]
	return out
}

func rules(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	return out
}

func TestParseRecordAccepts(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"verified", renderRecord(validVerified())},
		{"abandoned", renderRecord(validAbandoned())},
		{"verified with empty exceptions", renderRecord(setKey(validVerified(), "exceptions", ""))},
		{"exceptions naming two assets", renderRecord(setKey(validVerified(), "exceptions", "checksums.txt,proton-cli_linux_amd64"))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, fs := parseRecord("fork/verification/"+recTag+".md", recTag, c.data)
			if len(fs) != 0 {
				t.Fatalf("findings on a valid record: %v", fs)
			}
			if rec.Values["tag"] != recTag {
				t.Fatalf("tag value = %q", rec.Values["tag"])
			}
		})
	}
	t.Run("the status and the reason are read", func(t *testing.T) {
		rec, _ := parseRecord("p", recTag, renderRecord(validAbandoned()))
		if rec.Status != "abandoned" || rec.Values["reason"] != "refused" {
			t.Fatalf("record = %+v", rec)
		}
	})
	for _, reason := range []string{"refused", "failed", "draft", "window-passed", "rebased", "operator-decision"} {
		t.Run("reason "+reason, func(t *testing.T) {
			_, fs := parseRecord("p", recTag, renderRecord(setKey(validAbandoned(), "reason", reason)))
			if len(fs) != 0 {
				t.Fatalf("findings: %v", fs)
			}
		})
	}
}

func TestParseRecordRejects(t *testing.T) {
	v, a := validVerified(), validAbandoned()
	raw := func(s string) []byte { return []byte(s) }
	good := string(renderRecord(a))
	cases := []struct {
		name string
		data []byte
		rule string
		// detail is text the detail must hold, usually the key or a line number.
		detail string
	}{
		// Shape of the file.
		{"empty file", raw(""), "record-empty", ""},
		{"CRLF line ends", raw(strings.ReplaceAll(good, "\n", "\r\n")), "record-line-ends", "line 1"},
		{"no final LF", raw(strings.TrimSuffix(good, "\n")), "record-final-lf", ""},
		{"blank line", raw(strings.Replace(good, "status: abandoned\n", "\nstatus: abandoned\n", 1)), "record-blank-line", "line 3"},
		{"trailing blank line", raw(good + "\n"), "record-blank-line", "line 6"},
		{"non-ASCII", raw(strings.Replace(good, "refused", "refuséd", 1)), "record-characters", "line 5"},
		{"control character", raw(strings.Replace(good, "refused", "refus\x01d", 1)), "record-characters", "line 5"},
		{"tab", raw(strings.Replace(good, ": refused", ":\trefused", 1)), "record-characters", "line 5"},
		{"comment line", raw("# a comment\n" + good), "record-line", "line 1"},
		{"line without a separator", raw(strings.Replace(good, "schema: 1", "schema 1", 1)), "record-line", "line 1"},
		{"no space after the colon", raw(strings.Replace(good, "schema: 1", "schema:1", 1)), "record-line", "line 1"},
		{"leading space in a value", raw(strings.Replace(good, "schema: 1", "schema:  1", 1)), "record-line", "line 1"},
		{"trailing space in a value", raw(strings.Replace(good, "schema: 1", "schema: 1 ", 1)), "record-line", "line 1"},
		{"leading space before a key", raw(strings.Replace(good, "schema: 1", " schema: 1", 1)), "record-line", "line 1"},
		{"empty value outside exceptions", renderRecord(setKey(a, "reason", "")), "record-value", "reason"},
		{"empty value with a trailing space on exceptions", raw(strings.Replace(string(renderRecord(setKey(v, "exceptions", ""))), "exceptions:\n", "exceptions: \n", 1)), "record-line", ""},

		// Keys.
		{"unknown key", renderRecord(append(slices.Clone(a), kv{"extra", "x"})), "record-key-unknown", "line 6"},
		{"unknown attested key", renderRecord(insertBefore(v, "attested.gates", kv{"attested.other", "x"})), "record-key-unknown", ""},
		{"pattern key with an empty name", renderRecord(insertBefore(v, "exceptions", kv{"asset.", recHash})), "record-key-unknown", ""},
		{"pattern key with a bad name", renderRecord(insertBefore(v, "exceptions", kv{"asset.bad name", recHash})), "record-key-unknown", ""},
		{"repeated key", renderRecord(append(slices.Clone(a), kv{"reason", "failed"})), "record-key-repeated", "reason"},
		{"repeated header key", renderRecord(slices.Insert(slices.Clone(a), 1, kv{"schema", "1"})), "record-key-repeated", "schema"},
		{"repeated asset", renderRecord(insertBefore(v, "exceptions", v[5])), "record-key-repeated", ""},
		{"first key missing", renderRecord(dropKey(a, "schema")), "record-key-order", "line 1"},
		{"tag missing", renderRecord(dropKey(a, "tag")), "record-key-order", "line 2"},
		{"status missing", renderRecord(dropKey(a, "status")), "record-key-order", "line 3"},
		{"time missing", renderRecord(dropKey(a, "time")), "record-key-order", "line 4"},
		{"header reordered", renderRecord(swapKeys(a, "tag", "status")), "record-key-order", "line 2"},
		{"abandoned without reason", renderRecord(dropKey(a, "reason")), "record-key-missing", "reason"},
		{"abandoned with an extra key", renderRecord(append(slices.Clone(a), kv{"commit", recCommit})), "record-key-order", "line 6"},
		{"abandoned with a verified key", renderRecord(setKeyName(a, "reason", "commit")), "record-key-order", "line 5"},
		{"verified without commit", renderRecord(dropKey(v, "commit")), "record-key-order", "line 5"},
		{"verified with a reason key", renderRecord(insertBefore(v, "commit", kv{"reason", "refused"})), "record-key-order", "line 5"},
		{"verified key missing", renderRecord(dropKey(v, "run-id")), "record-key-order", "line 16"},
		{"verified last key missing", renderRecord(dropKey(v, "attested.snapshot-version")), "record-key-missing", "attested.snapshot-version"},
		{"verified keys reordered", renderRecord(swapKeys(v, "go-version", "goreleaser-version")), "record-key-order", "line 14"},
		{"attested before the run result", renderRecord(swapKeys(v, "run-conclusion", "attested.gates")), "record-key-order", "line 18"},
		{"verified with a key after the last", renderRecord(append(slices.Clone(v), kv{"reason", "refused"})), "record-key-order", "line 25"},
		{"assets not sorted", renderRecord(swapKeys(v, "asset.checksums.txt", "asset.proton-cli_linux_amd64")), "record-key-order", "line 7"},

		// Values.
		{"schema other than 1", renderRecord(setKey(a, "schema", "2")), "record-value", "schema"},
		{"status unknown", renderRecord(setKey(a, "status", "pending")), "record-value", "status"},
		{"time not UTC", renderRecord(setKey(a, "time", "2026-11-06T10:20:30+01:00")), "record-value", "time"},
		{"time without seconds", renderRecord(setKey(a, "time", "2026-11-06T10:20Z")), "record-value", "time"},
		{"time not an instant", renderRecord(setKey(a, "time", "2026-02-30T10:20:30Z")), "record-value", "time"},
		{"time hour 24", renderRecord(setKey(a, "time", "2026-11-06T24:00:00Z")), "record-value", "time"},
		{"time with a fraction", renderRecord(setKey(a, "time", "2026-11-06T10:20:30.5Z")), "record-value", "time"},
		{"time with a space", renderRecord(setKey(a, "time", "2026-11-06 10:20:30Z")), "record-value", "time"},
		{"reason outside the list", renderRecord(setKey(a, "reason", "because")), "record-value", "reason"},
		{"reason in capitals", renderRecord(setKey(a, "reason", "Refused")), "record-value", "reason"},
		{"tag not a Release Tag", renderRecord(setKey(a, "tag", "v5.1.0")), "record-value", "tag"},
		{"tag with leading zero in N", renderRecord(setKey(a, "tag", "v5.1.0-ezop.01")), "record-value", "tag"},
		{"commit uppercase", renderRecord(setKey(v, "commit", strings.ToUpper(recCommit))), "record-value", "commit"},
		{"commit too short", renderRecord(setKey(v, "commit", recCommit[:39])), "record-value", "commit"},
		{"commit not hex", renderRecord(setKey(v, "commit", strings.Repeat("g", 40))), "record-value", "commit"},
		{"asset hash too short", renderRecord(setKey(v, "asset.checksums.txt", recHash[:63])), "record-value", "line 6"},
		{"asset hash uppercase", renderRecord(setKey(v, "asset.checksums.txt", strings.ToUpper(recHash))), "record-value", "line 6"},
		{"asset hash is a 40 hex id", renderRecord(setKey(v, "asset.checksums.txt", recCommit)), "record-value", "line 6"},
		{"config blob too short", renderRecord(setKey(v, "goreleaser-config-blob", "abc")), "record-value", "goreleaser-config-blob"},
		{"lock blob uppercase", renderRecord(setKey(v, "devbox-lock-blob", strings.ToUpper(recCommit))), "record-value", "devbox-lock-blob"},
		{"host platform empty", renderRecord(setKey(v, "host-platform", "")), "record-value", "host-platform"},
		{"go version not dotted", renderRecord(setKey(v, "go-version", "go1.27")), "record-value", "go-version"},
		{"go version single number", renderRecord(setKey(v, "go-version", "1")), "record-value", "go-version"},
		{"goreleaser version with a prefix", renderRecord(setKey(v, "goreleaser-version", "v2.17.1")), "record-value", "goreleaser-version"},
		{"run id zero", renderRecord(setKey(v, "run-id", "0")), "record-value", "run-id"},
		{"run id with a leading zero", renderRecord(setKey(v, "run-id", "012")), "record-value", "run-id"},
		{"run id negative", renderRecord(setKey(v, "run-id", "-5")), "record-value", "run-id"},
		{"run attempt not a number", renderRecord(setKey(v, "run-attempt", "one")), "record-value", "run-attempt"},
		{"attested go version", renderRecord(setKey(v, "attested.go-version", "latest")), "record-value", "attested.go-version"},
		{"attested goreleaser version", renderRecord(setKey(v, "attested.goreleaser-version", "")), "record-value", "attested.goreleaser-version"},
		{"attested snapshot version empty", renderRecord(setKey(v, "attested.snapshot-version", "")), "record-value", "attested.snapshot-version"},
		{"asset name with a bad character", renderRecord(insertBefore(v, "exceptions", kv{"asset.a/b", recHash})), "record-key-unknown", ""},
		{"exceptions with a space", renderRecord(setKey(v, "exceptions", "checksums.txt, proton-cli_linux_amd64")), "record-value", "exceptions"},
		{"exceptions with an empty item", renderRecord(setKey(v, "exceptions", "checksums.txt,,proton-cli_linux_amd64")), "record-value", "exceptions"},
		{"exceptions with a trailing comma", renderRecord(setKey(v, "exceptions", "checksums.txt,")), "record-value", "exceptions"},
		{"exceptions not sorted", renderRecord(setKey(v, "exceptions", "proton-cli_linux_amd64,checksums.txt")), "record-value", "exceptions"},
		{"exceptions repeated", renderRecord(setKey(v, "exceptions", "checksums.txt,checksums.txt")), "record-value", "exceptions"},

		// Invariants.
		{"tag differs from the file name", renderRecord(setKey(a, "tag", "v5.1.0-ezop.2")), "record-invariant", "tag"},
		{"gate commit differs from commit", renderRecord(setKey(v, "attested.gate-commit", strings.Repeat("b", 40))), "record-invariant", "attested.gate-commit"},
		{"run conclusion failure", renderRecord(setKey(v, "run-conclusion", "failure")), "record-invariant", "run-conclusion"},
		{"run conclusion cancelled", renderRecord(setKey(v, "run-conclusion", "cancelled")), "record-invariant", "run-conclusion"},
		{"gates in another order", renderRecord(setKey(v, "attested.gates", "lint,test-fast,build,snapshot")), "record-invariant", "attested.gates"},
		{"gates missing one", renderRecord(setKey(v, "attested.gates", "test-fast,lint,build")), "record-invariant", "attested.gates"},
		{"lint left a diff", renderRecord(setKey(v, "attested.lint-diff", "present")), "record-invariant", "attested.lint-diff"},
		{"proton version differs from the tag", renderRecord(setKey(v, "proton-version", "5.1.0-ezop.2")), "record-invariant", "proton-version"},
		{"proton version keeps the v", renderRecord(setKey(v, "proton-version", "v5.1.0-ezop.1")), "record-invariant", "proton-version"},
		{"exceptions names an unlisted asset", renderRecord(setKey(v, "exceptions", "ghost.txt")), "record-invariant", "exceptions"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := "fork/verification/" + recTag + ".md"
			_, fs := parseRecord(path, recTag, c.data)
			var hit bool
			for _, f := range fs {
				if f.Path != path {
					t.Fatalf("finding path = %q", f.Path)
				}
				if f.Rule == c.rule && strings.Contains(f.Detail, c.detail) {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("want rule %q with detail holding %q, got %v", c.rule, c.detail, fs)
			}
		})
	}
}

// setKeyName renames a key and keeps its value.
func setKeyName(lines []kv, from, to string) []kv {
	out := slices.Clone(lines)
	for i := range out {
		if out[i].k == from {
			out[i].k = to
			return out
		}
	}
	panic("no key " + from)
}

func TestParseRecordNeverEchoesContent(t *testing.T) {
	secret := "SECRETVALUE-9f3"
	cases := [][]byte{
		renderRecord(setKey(validAbandoned(), "reason", secret)),
		renderRecord(append(validAbandoned(), kv{secret, "x"})),
		renderRecord(setKey(validAbandoned(), "time", secret)),
		renderRecord(setKey(validVerified(), "commit", secret)),
		renderRecord(setKey(validVerified(), "exceptions", secret)),
		renderRecord(setKey(validVerified(), "host-platform", secret+"é")),
		renderRecord(insertBefore(validVerified(), "exceptions", kv{"asset." + secret + " x", recHash})),
		[]byte(secret + "\r\n"),
		[]byte(secret),
		[]byte("# " + secret + "\n"),
	}
	for i, data := range cases {
		_, fs := parseRecord("p", recTag, data)
		if len(fs) == 0 {
			t.Fatalf("case %d: no finding", i)
		}
		for _, f := range fs {
			if strings.Contains(f.String(), secret) {
				t.Fatalf("case %d: finding echoes content: %v", i, f)
			}
		}
	}
}

func TestParseRecordReportsEveryValueFault(t *testing.T) {
	data := renderRecord(setKey(setKey(validVerified(), "run-id", "0"), "go-version", "x"))
	_, fs := parseRecord("p", recTag, data)
	if got := rules(fs); !slices.Equal(got, []string{"record-value", "record-value"}) {
		t.Fatalf("rules = %v", got)
	}
}

const verDir = "fork/verification/"

// recordFor is a valid record of either status for the tag.
func recordFor(tag string, verified bool) []byte {
	lines := validAbandoned()
	if verified {
		lines = validVerified()
		lines = setKey(lines, "proton-version", strings.TrimPrefix(tag, "v"))
	}
	return renderRecord(setKey(lines, "tag", tag))
}

func file(data []byte) *fstest.MapFile { return &fstest.MapFile{Data: data, Mode: 0o644} }

// validTree is a surface that passes: the README and two records.
func validTree() fstest.MapFS {
	return fstest.MapFS{
		verDir + "README.md":           file([]byte("# README\n")),
		verDir + "v5.1.0-ezop.1.md":    file(recordFor("v5.1.0-ezop.1", true)),
		verDir + "v5.1.0-ezop.2.md":    file(recordFor("v5.1.0-ezop.2", false)),
		"fork/other.md":                file([]byte("not under the surface\n")),
		"scripts/fork-release/main.go": file([]byte("package main\n")),
	}
}

func hasFinding(fs []Finding, path, rule string) bool {
	return slices.ContainsFunc(fs, func(f Finding) bool { return f.Path == path && f.Rule == rule })
}

func TestValidateVerificationDirAccepts(t *testing.T) {
	if fs := validateVerificationDir(validTree()); len(fs) != 0 {
		t.Fatalf("findings on a valid tree: %v", fs)
	}
	t.Run("a surface holding only the README", func(t *testing.T) {
		tree := fstest.MapFS{verDir + "README.md": file([]byte("# README\n"))}
		if fs := validateVerificationDir(tree); len(fs) != 0 {
			t.Fatalf("findings: %v", fs)
		}
	})
	t.Run("an executable README is a regular file", func(t *testing.T) {
		tree := validTree()
		tree[verDir+"README.md"] = &fstest.MapFile{Data: []byte("x"), Mode: 0o755}
		if fs := validateVerificationDir(tree); len(fs) != 0 {
			t.Fatalf("findings: %v", fs)
		}
	})
}

func TestValidateVerificationDirRejects(t *testing.T) {
	control := verDir + "v5.1.0-ezop.1.md"
	cases := []struct {
		name   string
		mutate func(tree fstest.MapFS)
		path   string
		rule   string
	}{
		{"missing surface", func(m fstest.MapFS) {
			for k := range m {
				if strings.HasPrefix(k, verDir) {
					delete(m, k)
				}
			}
		}, "fork/verification", "record-surface"},
		{"surface is a file", func(m fstest.MapFS) {
			for k := range m {
				if strings.HasPrefix(k, verDir) {
					delete(m, k)
				}
			}
			m["fork/verification"] = file([]byte("x"))
		}, "fork/verification", "record-surface"},
		{"missing README", func(m fstest.MapFS) { delete(m, verDir+"README.md") }, verDir + "README.md", "record-surface"},
		{"README is a directory", func(m fstest.MapFS) {
			delete(m, verDir+"README.md")
			m[verDir+"README.md/inner"] = file([]byte("x"))
		}, verDir + "README.md", "record-surface"},
		{"README is a symlink", func(m fstest.MapFS) {
			m[verDir+"README.md"] = &fstest.MapFile{Data: []byte("../../README.md"), Mode: fs.ModeSymlink | 0o777}
		}, verDir + "README.md", "record-surface"},
		{"README is a gitlink", func(m fstest.MapFS) {
			m[verDir+"README.md"] = &fstest.MapFile{Mode: fs.ModeIrregular}
		}, verDir + "README.md", "record-surface"},
		{"README in another case", func(m fstest.MapFS) {
			m[verDir+"readme.md"] = m[verDir+"README.md"]
			delete(m, verDir+"README.md")
		}, verDir + "README.md", "record-surface"},
		{"a record that is a directory", func(m fstest.MapFS) {
			m[verDir+"v5.1.0-ezop.3.md/inner"] = file([]byte("x"))
		}, verDir + "v5.1.0-ezop.3.md", "record-kind"},
		{"a record that is a symlink", func(m fstest.MapFS) {
			m[verDir+"v5.1.0-ezop.3.md"] = &fstest.MapFile{Data: []byte("v5.1.0-ezop.1.md"), Mode: fs.ModeSymlink | 0o777}
		}, verDir + "v5.1.0-ezop.3.md", "record-kind"},
		{"a record that is a gitlink", func(m fstest.MapFS) {
			m[verDir+"v5.1.0-ezop.3.md"] = &fstest.MapFile{Mode: fs.ModeIrregular}
		}, verDir + "v5.1.0-ezop.3.md", "record-kind"},
		{"a directory outside the pattern", func(m fstest.MapFS) {
			m[verDir+"evidence/x.md"] = file([]byte("x"))
		}, verDir + "evidence", "record-kind"},
		{"an upstream tag name", func(m fstest.MapFS) { m[verDir+"v5.1.0.md"] = file(recordFor("v5.1.0-ezop.1", false)) }, verDir + "v5.1.0.md", "record-name"},
		{"a free name", func(m fstest.MapFS) { m[verDir+"notes.md"] = file([]byte("x\n")) }, verDir + "notes.md", "record-name"},
		{"N zero", func(m fstest.MapFS) { m[verDir+"v5.1.0-ezop.0.md"] = file(recordFor("v5.1.0-ezop.1", false)) }, verDir + "v5.1.0-ezop.0.md", "record-name"},
		{"N with a leading zero", func(m fstest.MapFS) { m[verDir+"v5.1.0-ezop.01.md"] = file(recordFor("v5.1.0-ezop.1", false)) }, verDir + "v5.1.0-ezop.01.md", "record-name"},
		{"wrong extension", func(m fstest.MapFS) { m[verDir+"v5.1.0-ezop.1.txt"] = file(recordFor("v5.1.0-ezop.1", false)) }, verDir + "v5.1.0-ezop.1.txt", "record-name"},
		{"no extension", func(m fstest.MapFS) { m[verDir+"v5.1.0-ezop.3"] = file(recordFor("v5.1.0-ezop.3", false)) }, verDir + "v5.1.0-ezop.3", "record-name"},
		{"a tag with a prefix", func(m fstest.MapFS) { m[verDir+"xv5.1.0-ezop.3.md"] = file(recordFor("v5.1.0-ezop.3", false)) }, verDir + "xv5.1.0-ezop.3.md", "record-name"},
		{"a hidden file", func(m fstest.MapFS) { m[verDir+".gitkeep"] = file(nil) }, verDir + ".gitkeep", "record-name"},
		{"an empty record", func(m fstest.MapFS) { m[verDir+"v5.1.0-ezop.3.md"] = file(nil) }, verDir + "v5.1.0-ezop.3.md", "record-empty"},
		{"a malformed record", func(m fstest.MapFS) {
			m[verDir+"v5.1.0-ezop.3.md"] = file(renderRecord(setKey(validAbandoned(), "reason", "because")))
		}, verDir + "v5.1.0-ezop.3.md", "record-value"},
		{"an unknown key", func(m fstest.MapFS) {
			m[verDir+"v5.1.0-ezop.3.md"] = file(renderRecord(append(setKey(validAbandoned(), "tag", "v5.1.0-ezop.3"), kv{"note", "x"})))
		}, verDir + "v5.1.0-ezop.3.md", "record-key-unknown"},
		{"a record naming another tag", func(m fstest.MapFS) {
			m[verDir+"v5.1.0-ezop.3.md"] = file(recordFor("v5.1.0-ezop.4", false))
		}, verDir + "v5.1.0-ezop.3.md", "record-invariant"},
		{"a gate commit that differs", func(m fstest.MapFS) {
			m[verDir+"v5.1.0-ezop.3.md"] = file(renderRecord(setKey(setKey(setKey(validVerified(), "tag", "v5.1.0-ezop.3"), "proton-version", "5.1.0-ezop.3"), "attested.gate-commit", strings.Repeat("c", 40))))
		}, verDir + "v5.1.0-ezop.3.md", "record-invariant"},
		{"a run that did not succeed", func(m fstest.MapFS) {
			m[verDir+"v5.1.0-ezop.3.md"] = file(renderRecord(setKey(setKey(setKey(validVerified(), "tag", "v5.1.0-ezop.3"), "proton-version", "5.1.0-ezop.3"), "run-conclusion", "failure")))
		}, verDir + "v5.1.0-ezop.3.md", "record-invariant"},
		{"a record that is far too large", func(m fstest.MapFS) {
			m[verDir+"v5.1.0-ezop.3.md"] = file([]byte(strings.Repeat("a", 70000)))
		}, verDir + "v5.1.0-ezop.3.md", "record-size"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tree := validTree()
			c.mutate(tree)
			got := validateVerificationDir(tree)
			if !hasFinding(got, c.path, c.rule) {
				t.Fatalf("want %s at %s, got %v", c.rule, c.path, got)
			}
			// The healthy record beside the fault stays clean.
			for _, f := range got {
				if f.Path == control {
					t.Fatalf("finding on the passing record: %v", f)
				}
			}
		})
	}
}

func TestValidateVerificationDirListsEveryFault(t *testing.T) {
	tree := validTree()
	tree[verDir+"notes.md"] = file([]byte("x\n"))
	tree[verDir+"v5.1.0-ezop.3.md"] = file(nil)
	got := validateVerificationDir(tree)
	if !hasFinding(got, verDir+"notes.md", "record-name") || !hasFinding(got, verDir+"v5.1.0-ezop.3.md", "record-empty") {
		t.Fatalf("findings = %v", got)
	}
	if !slices.Equal(got, sortFindings(got)) {
		t.Fatalf("findings are not sorted: %v", got)
	}
}

func TestValidateVerificationDirNeverEchoesContent(t *testing.T) {
	tree := validTree()
	tree[verDir+"v5.1.0-ezop.3.md"] = file([]byte("SECRETVALUE: x\n"))
	for _, f := range validateVerificationDir(tree) {
		if strings.Contains(f.String(), "SECRETVALUE") {
			t.Fatalf("finding echoes content: %v", f)
		}
	}
}

// repoRoot is the repository as a file system. The tests below read only
// fork/verification/ of it.
func repoRoot() fs.FS { return os.DirFS("../..") }

func TestRealVerificationDirPasses(t *testing.T) {
	if found := validateVerificationDir(repoRoot()); len(found) != 0 {
		t.Fatalf("the real fork/verification/ is held: %v", found)
	}
}

// readmeKeys reads the bullets that start with a backticked key from the three
// sections of the README that list the closed set.
func readmeKeys(t *testing.T) map[string][]string {
	t.Helper()
	data, err := fs.ReadFile(repoRoot(), "fork/verification/README.md")
	if err != nil {
		t.Fatalf("the README cannot be read: %v", err)
	}
	bullet := regexp.MustCompile("^- `([^`]+)`")
	sections := map[string][]string{}
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		if name, ok := strings.CutPrefix(line, "## "); ok {
			section = name
			continue
		}
		if m := bullet.FindStringSubmatch(line); m != nil {
			sections[section] = append(sections[section], m[1])
		}
	}
	return sections
}

// keySetDifferences compares the keys a README section names with the helper's
// list in both directions and for repeats.
func keySetDifferences(doc, code []string) []string {
	var out []string
	for _, k := range doc {
		if !slices.Contains(code, k) {
			out = append(out, "the README names "+k+", which the helper's key set does not have")
		}
	}
	for _, k := range code {
		if !slices.Contains(doc, k) {
			out = append(out, "the helper has "+k+", which the README does not name")
		}
	}
	if len(slices.Compact(slices.Sorted(slices.Values(doc)))) != len(doc) {
		out = append(out, "the README names a key twice")
	}
	return out
}

func TestReadmeMatchesTheKeySet(t *testing.T) {
	sections := readmeKeys(t)
	for _, c := range []struct {
		section string
		code    []string
	}{
		{"Fields", recordFields()},
		{"Pattern keys", recordPatterns()},
		{"Reasons", recordReasons},
	} {
		t.Run(c.section, func(t *testing.T) {
			for _, d := range keySetDifferences(sections[c.section], c.code) {
				t.Error(d)
			}
		})
	}
}

func TestKeySetDifferencesSeeEitherSideChangingAlone(t *testing.T) {
	code := []string{"schema", "tag", "status"}
	if d := keySetDifferences(code, code); len(d) != 0 {
		t.Fatalf("equal sets differ: %v", d)
	}
	if d := keySetDifferences([]string{"schema", "tag"}, code); len(d) != 1 || !strings.Contains(d[0], "helper has status") {
		t.Fatalf("a key the README lacks: %v", d)
	}
	if d := keySetDifferences([]string{"schema", "tag", "status", "extra"}, code); len(d) != 1 || !strings.Contains(d[0], "README names extra") {
		t.Fatalf("a key the helper lacks: %v", d)
	}
	if d := keySetDifferences([]string{"schema", "tag", "status", "tag"}, code); len(d) != 1 {
		t.Fatalf("a repeat: %v", d)
	}
}
