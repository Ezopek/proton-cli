package main

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// goTree is a tree holding only a .goreleaser.yaml.
func goTree(src string) fstest.MapFS {
	return fstest.MapFS{".goreleaser.yaml": {Data: []byte(src)}}
}

// cleaned is the byte-exact cleaned configuration.
func cleaned(t *testing.T) string { return string(fixture(t, "goreleaser.cleaned.yaml")) }

// mutate replaces one exact snippet of the cleaned configuration, so that a
// case that does not apply fails loudly instead of testing nothing.
func mutate(t *testing.T, from, to string) string {
	t.Helper()
	src := cleaned(t)
	if strings.Count(src, from) != 1 {
		t.Fatalf("%q appears %d times in the cleaned fixture", from, strings.Count(src, from))
	}
	return strings.Replace(src, from, to, 1)
}

func rulesOf(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		if !slices.Contains(out, f.Rule) {
			out = append(out, f.Rule)
		}
	}
	slices.Sort(out)
	return out
}

func details(fs []Finding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.String() + "\n")
	}
	return b.String()
}

// wantHeld fails unless the findings include the rule and every needle, all in
// the verdict text.
func wantHeld(t *testing.T, fs []Finding, rule string, needles ...string) {
	t.Helper()
	if !slices.Contains(rulesOf(fs), rule) {
		t.Fatalf("rule %q not held; findings:\n%s", rule, details(fs))
	}
	for _, f := range fs {
		if f.Path == "" {
			t.Fatalf("finding without a path: %v", f)
		}
	}
	for _, n := range needles {
		if !strings.Contains(details(fs), n) {
			t.Fatalf("findings do not name %q:\n%s", n, details(fs))
		}
	}
}

func TestCheckGoreleaser(t *testing.T) {
	t.Run("cleaned fixture passes", func(t *testing.T) {
		if fs := checkGoreleaser(goTree(cleaned(t))); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
	t.Run("a quoted keep-existing is the same string", func(t *testing.T) {
		src := mutate(t, "mode: keep-existing", `mode: "keep-existing"`)
		if fs := checkGoreleaser(goTree(src)); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})

	t.Run("release pins", func(t *testing.T) {
		cases := []struct {
			name     string
			from, to string
			rule     string
			needles  []string
		}{
			{"mode replace", "mode: keep-existing", "mode: replace", "goreleaser-release-pin", []string{"mode", "replace"}},
			{"mode absent", "  mode: keep-existing\n", "", "goreleaser-release-pin", []string{"mode"}},
			{"mode other string", "mode: keep-existing", "mode: append", "goreleaser-release-pin", []string{"mode", "append"}},
			{"mode not a string", "mode: keep-existing", "mode: 1", "goreleaser-release-pin", []string{"mode"}},
			{"replace true", "replace_existing_artifacts: false", "replace_existing_artifacts: true", "goreleaser-release-pin", []string{"replace_existing_artifacts", "true"}},
			{"replace absent", "  replace_existing_artifacts: false\n", "", "goreleaser-release-pin", []string{"replace_existing_artifacts"}},
			{"replace quoted false", "replace_existing_artifacts: false", `replace_existing_artifacts: "false"`, "goreleaser-release-pin", []string{"replace_existing_artifacts"}},
			{"replace no", "replace_existing_artifacts: false", "replace_existing_artifacts: no", "goreleaser-release-pin", []string{"replace_existing_artifacts"}},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				wantHeld(t, checkGoreleaser(goTree(mutate(t, c.from, c.to))), c.rule, c.needles...)
			})
		}
		t.Run("no release block", func(t *testing.T) {
			src := "version: 2\nbefore:\n  hooks:\n    - bash scripts/gen-completions.sh\nbuilds: []\narchives: []\nchecksum: {}\n"
			wantHeld(t, checkGoreleaser(goTree(src)), "goreleaser-release-pin", "release")
		})
		t.Run("release is not a mapping", func(t *testing.T) {
			src := "version: 2\nbefore:\n  hooks:\n    - bash scripts/gen-completions.sh\nbuilds: []\narchives: []\nchecksum: {}\nrelease: keep\n"
			wantHeld(t, checkGoreleaser(goTree(src)), "goreleaser-release-pin", "release")
		})
	})

	t.Run("extra top-level keys", func(t *testing.T) {
		for _, key := range []string{"aurs", "nfpms", "winget", "homebrew_casks", "changelog", "env"} {
			t.Run(key, func(t *testing.T) {
				src := cleaned(t) + "\n" + key + ":\n  - name: x\n"
				wantHeld(t, checkGoreleaser(goTree(src)), "goreleaser-top-level-key", key)
			})
		}
	})

	t.Run("extra release keys", func(t *testing.T) {
		for _, key := range []string{"use_existing_draft: true", "replace_existing_draft: true", "target_commitish: main", "github: {owner: x, name: y}"} {
			t.Run(strings.Split(key, ":")[0], func(t *testing.T) {
				src := cleaned(t) + "  " + key + "\n"
				wantHeld(t, checkGoreleaser(goTree(src)), "goreleaser-release-key", strings.Split(key, ":")[0])
			})
		}
	})

	t.Run("before.hooks", func(t *testing.T) {
		const hook = "    - bash scripts/gen-completions.sh\n"
		cases := []struct{ name, from, to string }{
			{"empty list", "  hooks:\n" + hook, "  hooks: []\n"},
			{"not a list", "  hooks:\n" + hook, "  hooks: bash scripts/gen-completions.sh\n"},
			{"different command", hook, "    - bash scripts/other.sh\n"},
			{"second entry", hook, hook + "    - echo hi\n"},
			{"hooks absent", "before:\n  hooks:\n" + hook, ""},
			{"before has another key", "before:\n", "before:\n  other: 1\n"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				wantHeld(t, checkGoreleaser(goTree(mutate(t, c.from, c.to))), "goreleaser-hooks")
			})
		}
		t.Run("hooks under a build", func(t *testing.T) {
			src := mutate(t, "    id: unix\n", "    id: unix\n    hooks:\n      post:\n        - echo hi\n")
			wantHeld(t, checkGoreleaser(goTree(src)), "goreleaser-hooks", "hooks")
		})
		t.Run("hooks under release", func(t *testing.T) {
			src := cleaned(t) + "  hooks: x\n"
			held := checkGoreleaser(goTree(src))
			if !slices.Contains(rulesOf(held), "goreleaser-hooks") {
				t.Fatalf("findings:\n%s", details(held))
			}
		})
	})

	t.Run("template actions", func(t *testing.T) {
		held := []struct{ name, action string }{
			{"env", "{{ .Env.AUR_KEY }}"},
			{"envOrDefault", `{{ envOrDefault "X" "y" }}`},
			{"isEnvSet", `{{ isEnvSet "X" }}`},
			{"pipe", "{{ .Version | tolower }}"},
			{"left trim", "{{- .Version }}"},
			{"right trim", "{{ .Version -}}"},
			{"unterminated", "{{ .Version"},
			{"other field", "{{ .Tag }}"},
			{"empty", "{{}}"},
			{"two actions one bad", "{{ .Version }}-{{ .Env.X }}"},
		}
		for _, c := range held {
			t.Run("held "+c.name, func(t *testing.T) {
				src := mutate(t, `name_template: "checksums.txt"`, `name_template: "checksums-`+strings.ReplaceAll(c.action, `"`, `'`)+`.txt"`)
				wantHeld(t, checkGoreleaser(goTree(src)), "goreleaser-template")
			})
		}
		t.Run("held in a key", func(t *testing.T) {
			src := cleaned(t) + "  \"{{ .Env.X }}\": 1\n"
			wantHeld(t, checkGoreleaser(goTree(src)), "goreleaser-template")
		})
		for _, a := range []string{"{{.Version}}", "{{ .Version }}", "{{ .ProjectName }}", "{{ .Os }}", "{{ .Arch }}", "{{  .Arch  }}"} {
			t.Run("pass "+a, func(t *testing.T) {
				src := mutate(t, `name_template: "checksums.txt"`, `name_template: "x-`+a+`.txt"`)
				if fs := checkGoreleaser(goTree(src)); len(fs) != 0 {
					t.Fatalf("findings:\n%s", details(fs))
				}
			})
		}
	})

	t.Run("yaml shape", func(t *testing.T) {
		t.Run("duplicate key", func(t *testing.T) {
			src := cleaned(t) + "checksum:\n  algorithm: sha256\n"
			wantHeld(t, checkGoreleaser(goTree(src)), ruleYAMLDuplicate)
		})
		t.Run("merge key not first", func(t *testing.T) {
			src := mutate(t, "  - <<: *build\n    id: windows\n", "  - id: windows\n    <<: *build\n")
			wantHeld(t, checkGoreleaser(goTree(src)), ruleYAMLMerge)
		})
		t.Run("sequence merge", func(t *testing.T) {
			src := mutate(t, "<<: *build", "<<: [*build]")
			wantHeld(t, checkGoreleaser(goTree(src)), ruleYAMLMerge)
		})
		t.Run("second document", func(t *testing.T) {
			wantHeld(t, checkGoreleaser(goTree(cleaned(t)+"---\na: 1\n")), ruleYAMLDocuments)
		})
		t.Run("root is not a mapping", func(t *testing.T) {
			wantHeld(t, checkGoreleaser(goTree("- a\n- b\n")), "goreleaser-top-level-key")
		})
	})

	t.Run("unreadable", func(t *testing.T) {
		t.Run("missing", func(t *testing.T) {
			wantHeld(t, checkGoreleaser(fstest.MapFS{}), "goreleaser-unreadable", ".goreleaser.yaml")
		})
		t.Run("unparseable", func(t *testing.T) {
			wantHeld(t, checkGoreleaser(goTree("a: [1, 2\n")), ruleYAMLParse)
		})
		t.Run("a directory", func(t *testing.T) {
			fsys := fstest.MapFS{".goreleaser.yaml/x": {Data: []byte("a")}}
			wantHeld(t, checkGoreleaser(fsys), "goreleaser-unreadable")
		})
	})
}

// A document that decodes to nothing (null, ~, an explicit null document) has no
// pins to read. It is held, and never read as a clean file. Nested nulls are
// ordinary YAML and stay unremarkable.
func TestCheckGoreleaserNullDocument(t *testing.T) {
	for name, src := range map[string]string{
		"null":                   "null\n",
		"tilde":                  "~\n",
		"explicit document null": "--- null\n",
	} {
		t.Run(name, func(t *testing.T) {
			got := checkGoreleaser(goTree(src))
			if len(got) != 1 || got[0].Path != goreleaserPath || got[0].Rule != ruleYAMLParse || !strings.Contains(got[0].Detail, "null") {
				t.Fatalf("want one %s finding naming null for %s, got:\n%s", ruleYAMLParse, goreleaserPath, details(got))
			}
		})
	}
	t.Run("a nested null stays ordinary", func(t *testing.T) {
		src := mutate(t, "algorithm: sha256", "algorithm: sha256\n  disable: ~")
		if fs := checkGoreleaser(goTree(src)); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
}

// A value of a type the decoder should not produce is held at its path, and the
// verdict names the type. The location used is otherwise allowed, so the only
// possible cause is the type. The same location with a plain string is clean.
func TestCheckGoreleaserUnexpectedType(t *testing.T) {
	t.Run("a binary scalar in a nested block is held with its path and type", func(t *testing.T) {
		src := mutate(t, "    id: unix\n", "    id: unix\n    x: !!binary aGk=\n")
		got := checkGoreleaser(goTree(src))
		if len(got) != 1 || got[0].Rule != ruleGoTopLevel || !strings.Contains(got[0].Detail, "value of an unexpected type at builds[0].x ([]uint8)") {
			t.Fatalf("want one unexpected-type finding at builds[0].x, got:\n%s", details(got))
		}
	})
	t.Run("the same location with a plain string is clean", func(t *testing.T) {
		src := mutate(t, "    id: unix\n", "    id: unix\n    x: hi\n")
		if fs := checkGoreleaser(goTree(src)); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
}

// A sequence merge behind an anchor is held like the inline one, under whatever
// name the anchor carries, and the mapping merge it replaces stays accepted.
func TestCheckGoreleaserAnchoredSequenceMerge(t *testing.T) {
	for name, to := range map[string]string{
		"anchor named merged": "<<: &merged [*build]",
		"anchor named other":  "<<: &ops [*build]",
	} {
		t.Run(name, func(t *testing.T) {
			wantHeld(t, checkGoreleaser(goTree(mutate(t, "<<: *build", to))), ruleYAMLMerge, "sequence merge")
		})
	}
	t.Run("an anchored mapping merge is accepted", func(t *testing.T) {
		got := checkGoreleaser(goTree(mutate(t, "<<: *build", "<<: &whole {id: windows}")))
		if slices.Contains(rulesOf(got), ruleYAMLMerge) {
			t.Fatalf("a mapping merge was held as a sequence merge:\n%s", details(got))
		}
	})
}

// Giving the name of an existing anchor to a second node is held for that
// reason. Uses of the one anchor, and a second anchor under its own name, are
// ordinary.
func TestCheckGoreleaserReusedAnchor(t *testing.T) {
	t.Run("a second definition of the same name is held", func(t *testing.T) {
		got := checkGoreleaser(goTree(mutate(t, "  - id: windows-alias\n", "  - &build\n    id: windows-alias\n")))
		wantHeld(t, got, ruleYAMLParse, "defined more than once", `"build"`)
		if slices.Contains(rulesOf(got), ruleYAMLMerge) {
			t.Fatalf("a merge was diagnosed:\n%s", details(got))
		}
	})
	t.Run("a second anchor under its own name is accepted", func(t *testing.T) {
		if got := checkGoreleaser(goTree(mutate(t, "  - id: windows-alias\n", "  - &other\n    id: windows-alias\n"))); len(got) != 0 {
			t.Fatalf("findings:\n%s", details(got))
		}
	})
	t.Run("a second use of the one anchor is accepted", func(t *testing.T) {
		if got := checkGoreleaser(goTree(mutate(t, "  - id: windows-alias\n", "  - <<: *build\n    id: windows-alias\n"))); len(got) != 0 {
			t.Fatalf("findings:\n%s", details(got))
		}
	})
}
