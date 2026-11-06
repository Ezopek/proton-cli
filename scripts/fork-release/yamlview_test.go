package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixture reads a committed file from testdata. Tests read nothing else from
// disk.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%T is not a mapping", v)
	}
	return m
}

func asList(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("%T is not a sequence", v)
	}
	return l
}

func issueRules(is []yamlIssue) []string {
	var out []string
	for _, i := range is {
		out = append(out, i.rule)
	}
	return out
}

func TestParseYAMLFixtures(t *testing.T) {
	t.Run("ci.yml parses clean", func(t *testing.T) {
		v, issues := parseYAML(fixture(t, "ci.yml"))
		if len(issues) != 0 {
			t.Fatalf("issues: %v", issues)
		}
		m := asMap(t, v)
		if _, ok := m["on"]; !ok {
			t.Fatalf("the trigger key is not the string %q: keys %v", "on", m)
		}
		if _, ok := m["permissions"]; !ok {
			t.Fatal("no permissions key")
		}
	})

	t.Run("cleaned goreleaser fixture parses clean with typed pins", func(t *testing.T) {
		v, issues := parseYAML(fixture(t, "goreleaser.cleaned.yaml"))
		if len(issues) != 0 {
			t.Fatalf("issues: %v", issues)
		}
		rel := asMap(t, asMap(t, v)["release"])
		if rel["mode"] != "keep-existing" {
			t.Fatalf("mode = %#v", rel["mode"])
		}
		if rel["replace_existing_artifacts"] != false {
			t.Fatalf("replace_existing_artifacts = %#v", rel["replace_existing_artifacts"])
		}
	})

	t.Run("the real merge shape resolves with its overrides", func(t *testing.T) {
		v, issues := parseYAML(fixture(t, "goreleaser.cleaned.yaml"))
		if len(issues) != 0 {
			t.Fatalf("issues: %v", issues)
		}
		builds := asList(t, asMap(t, v)["builds"])
		win := asMap(t, builds[1])
		if win["id"] != "windows" {
			t.Fatalf("id = %#v", win["id"])
		}
		if win["main"] != "./cmd/proton" {
			t.Fatalf("main was not inherited through the merge: %#v", win["main"])
		}
		if !reflect.DeepEqual(win["goos"], []any{"windows"}) {
			t.Fatalf("goos override lost: %#v", win["goos"])
		}
		if !reflect.DeepEqual(win["flags"], []any{"-tags=webauthn"}) {
			t.Fatalf("flags = %#v", win["flags"])
		}
	})
}

func TestParseYAMLTyping(t *testing.T) {
	v, issues := parseYAML([]byte("a: no\nb: \"false\"\nc: false\nd: off\ne: on\nf: keep-existing\n"))
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	m := asMap(t, v)
	cases := map[string]any{"a": "no", "b": "false", "c": false, "d": "off", "e": "on", "f": "keep-existing"}
	for k, want := range cases {
		t.Run(k, func(t *testing.T) {
			if m[k] != want {
				t.Fatalf("%s = %#v (%T), want %#v", k, m[k], m[k], want)
			}
		})
	}
}

func TestParseYAMLOnKey(t *testing.T) {
	for name, src := range map[string]string{
		"plain scalar":  "on: push\n",
		"plain list":    "on: [push, pull_request]\n",
		"plain mapping": "on:\n  push:\n    branches: [main]\n",
		"quoted scalar": "\"on\": push\n",
		"quoted list":   "\"on\": [push]\n",
		"quoted map":    "\"on\":\n  push:\n    tags: ['v*']\n",
	} {
		t.Run(name, func(t *testing.T) {
			v, issues := parseYAML([]byte(src))
			if len(issues) != 0 {
				t.Fatalf("issues: %v", issues)
			}
			if _, ok := asMap(t, v)["on"]; !ok {
				t.Fatalf("no string key %q in %#v", "on", v)
			}
		})
	}
}

func TestParseYAMLHolds(t *testing.T) {
	cases := []struct {
		name string
		src  string
		rule string
	}{
		{"duplicate key", "a: 1\nb: 2\na: 3\n", "yaml-duplicate-key"},
		{"nested duplicate key", "x:\n  a: 1\n  a: 2\n", "yaml-duplicate-key"},
		{"merge key not first", "base: &b\n  k: 1\nm:\n  id: x\n  <<: *b\n", "yaml-merge"},
		{"sequence merge", "a: &a\n  k: 1\nb: &b\n  j: 2\nm:\n  <<: [*a, *b]\n", "yaml-merge"},
		{"second document", "a: 1\n---\nb: 2\n", "yaml-documents"},
		{"unparseable", "a: [1, 2\n", "yaml-parse"},
		{"unknown alias", "m:\n  <<: *nope\n", "yaml-parse"},
		{"empty", "", "yaml-parse"},
		{"only a comment", "# nothing\n", "yaml-parse"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, issues := parseYAML([]byte(c.src))
			if !reflect.DeepEqual(issueRules(issues), []string{c.rule}) {
				t.Fatalf("rules = %v, want [%s]", issues, c.rule)
			}
			for _, i := range issues {
				if strings.TrimSpace(i.detail) == "" {
					t.Fatal("an issue without a detail")
				}
			}
		})
	}

	t.Run("a merge override is not a duplicate", func(t *testing.T) {
		src := "base: &b\n  id: one\n  goos: [linux]\nm:\n  <<: *b\n  id: two\n  goos: [windows]\n  flags: [x]\n"
		v, issues := parseYAML([]byte(src))
		if len(issues) != 0 {
			t.Fatalf("issues: %v", issues)
		}
		m := asMap(t, asMap(t, v)["m"])
		if m["id"] != "two" || !reflect.DeepEqual(m["goos"], []any{"windows"}) {
			t.Fatalf("override lost: %#v", m)
		}
	})

	t.Run("a merge key first in a sequence entry is accepted", func(t *testing.T) {
		src := "base: &b\n  k: 1\nl:\n  - <<: *b\n    id: x\n"
		if _, issues := parseYAML([]byte(src)); len(issues) != 0 {
			t.Fatalf("issues: %v", issues)
		}
	})
}

// A sequence merge is held whatever wraps the sequence: written inline, behind
// an anchor, or reached through an alias to an anchored sequence. An anchored or
// aliased mapping is an ordinary merge, and a sequence inside a merged mapping
// is a plain value. Each case stands alone.
func TestParseYAMLSequenceMergeOperands(t *testing.T) {
	held := map[string]string{
		"inline sequence":                      "a: &a\n  k: 1\nm:\n  <<: [*a]\n",
		"anchored flow sequence":               "a: &a\n  k: 1\nm:\n  <<: &s [*a]\n",
		"anchored block sequence":              "a: &a\n  k: 1\nm:\n  <<: &s\n    - *a\n",
		"anchored sequence in a list entry":    "l:\n  - <<: &s [{k: 1}]\n    id: x\n",
		"anchored sequence with a sibling key": "a: &a\n  k: 1\nm:\n  <<: &s [*a]\n  x: 1\n",
		"alias to an anchored sequence":        "a: &a\n  k: 1\nseq: &s [*a]\nm:\n  <<: *s\n",
		"alias to a block sequence":            "a: &a\n  k: 1\nseq: &s\n  - *a\nm:\n  <<: *s\n",
	}
	for name, src := range held {
		t.Run("held: "+name, func(t *testing.T) {
			_, issues := parseYAML([]byte(src))
			if !reflect.DeepEqual(issueRules(issues), []string{"yaml-merge"}) {
				t.Fatalf("rules = %v, want [yaml-merge]", issues)
			}
		})
	}

	clean := map[string]string{
		"anchored flow mapping":                   "m:\n  <<: &m {k: 2}\n",
		"anchored block mapping":                  "m:\n  <<: &m\n    k: 2\n",
		"alias to an anchored mapping":            "a: &a\n  k: 1\nm:\n  <<: *a\n  j: 2\n",
		"sequences inside a merged mapping":       "m:\n  <<: &c\n    flags: [x]\n    ids:\n      - y\n",
		"sequences inside an aliased mapping":     "a: &a\n  goos: [linux]\n  env:\n    - A=1\nm:\n  <<: *a\n  flags: [x]\n",
		"an anchored sequence that is not merged": "a: &a\n  k: 1\nseq: &s [*a]\nother: *s\nm:\n  <<: *a\n",
	}
	for name, src := range clean {
		t.Run("clean: "+name, func(t *testing.T) {
			if _, issues := parseYAML([]byte(src)); len(issues) != 0 {
				t.Fatalf("issues: %v", issues)
			}
		})
	}
}

// A name given to two anchors in one document is not classified: which node an
// alias means then depends on how the decoder nests and restores its bindings,
// and the file is held for that reason, whatever the alias is used for. The
// first two inputs are the rebinding rows this change moved out of the
// sequence-merge and accepted-mapping controls above.
func TestParseYAMLReusedAnchors(t *testing.T) {
	held := map[string]string{
		"rebound to a sequence":              "s: &s {k: 1}\nt: &s [{j: 1}]\nm:\n  <<: *s\n",
		"rebound to a mapping":               "s: &s [1]\nt: &s {k: 1}\nm:\n  <<: *s\n",
		"nested, outer sequence":             "a: &s\n  - &s {id: unix}\nm:\n  <<: *s\n",
		"nested, outer mapping":              "a: &s {k: &s [1]}\nm: {<<: *s}\n",
		"replayed through a merge, sequence": "b:\n  - &a {k: &s [{id: unix}]}\n  - &s {id: unix}\nm:\n  - <<: *a\n  - <<: *s\n",
		"replayed through a merge, mapping":  "b:\n  - &a {k: &s {id: unix}}\n  - &s [{id: unix}]\nm:\n  - <<: *a\n  - <<: *s\n",
		"no merge at all":                    "a: &s 1\nb: &s 2\n",
	}
	for name, src := range held {
		t.Run("held: "+name, func(t *testing.T) {
			_, issues := parseYAML([]byte(src))
			if !reflect.DeepEqual(issueRules(issues), []string{"yaml-parse"}) {
				t.Fatalf("rules = %v, want [yaml-parse]", issues)
			}
			if !strings.Contains(issues[0].detail, "defined more than once") || !strings.Contains(issues[0].detail, `"s"`) {
				t.Fatalf("detail does not name the reused anchor: %q", issues[0].detail)
			}
		})
	}

	clean := map[string]string{
		"nested, renamed, outer mapping":   "a: &o {k: &i [1]}\nm: {<<: *o}\n",
		"several references to one anchor": "a: &a {k: 1}\nm:\n  <<: *a\nn:\n  <<: *a\no: *a\n",
		"two different anchors":            "a: &a {k: 1}\nb: &b {j: 2}\nm:\n  <<: *a\nn:\n  <<: *b\n",
	}
	for name, src := range clean {
		t.Run("clean: "+name, func(t *testing.T) {
			if _, issues := parseYAML([]byte(src)); len(issues) != 0 {
				t.Fatalf("issues: %v", issues)
			}
		})
	}

	t.Run("the same name in different documents is a document hold only", func(t *testing.T) {
		_, issues := parseYAML([]byte("a: &s 1\n---\nb: &s 2\n"))
		if !reflect.DeepEqual(issueRules(issues), []string{"yaml-documents"}) {
			t.Fatalf("rules = %v, want [yaml-documents]", issues)
		}
	})
	t.Run("renamed, the outer sequence is a sequence merge", func(t *testing.T) {
		_, issues := parseYAML([]byte("a: &o\n  - &i {id: unix}\nm:\n  <<: *o\n"))
		if !reflect.DeepEqual(issueRules(issues), []string{"yaml-merge"}) {
			t.Fatalf("rules = %v, want [yaml-merge]", issues)
		}
	})
	t.Run("a reused name is not diagnosed as a sequence merge", func(t *testing.T) {
		_, issues := parseYAML([]byte("a: &s {k: &s [1]}\nm: {<<: *s}\n"))
		for _, i := range issues {
			if i.rule == "yaml-merge" {
				t.Fatalf("a sequence merge was claimed: %v", issues)
			}
		}
	})
}
