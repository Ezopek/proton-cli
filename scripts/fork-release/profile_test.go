package main

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// cleanTree is the cleaned fork tree: the cleaned configuration and the one
// workflow the cleaned tree already has.
func cleanTree(t *testing.T) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		".goreleaser.yaml":            {Data: fixture(t, "goreleaser.cleaned.yaml")},
		".github/workflows/ci.yml":    {Data: fixture(t, "ci.yml")},
		".github/ISSUE_TEMPLATE/a.md": {Data: []byte("x")},
	}
}

func TestCheckRelease(t *testing.T) {
	t.Run("cleaned tree passes", func(t *testing.T) {
		if fs := CheckRelease(cleanTree(t)); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
	t.Run("cleaned tree with fork-release.yml passes", func(t *testing.T) {
		fsys := cleanTree(t)
		fsys[".github/workflows/fork-release.yml"] = &fstest.MapFile{Data: []byte(forkReleaseY)}
		if fs := CheckRelease(fsys); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
	t.Run("no workflows directory is not held", func(t *testing.T) {
		fsys := fstest.MapFS{".goreleaser.yaml": {Data: fixture(t, "goreleaser.cleaned.yaml")}}
		if fs := CheckRelease(fsys); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
	t.Run("the goreleaser rules still apply without workflows", func(t *testing.T) {
		fsys := fstest.MapFS{}
		wantHeld(t, CheckRelease(fsys), "goreleaser-unreadable")
	})

	// Each held row of the spine's table, beside the pass above.
	t.Run("restored release.yml", func(t *testing.T) {
		fsys := cleanTree(t)
		fsys[".github/workflows/release.yml"] = &fstest.MapFile{Data: []byte("name: Release\n")}
		wantHeld(t, CheckRelease(fsys), "workflow-set", "release.yml")
	})
	t.Run("aurs back", func(t *testing.T) {
		fsys := cleanTree(t)
		fsys[".goreleaser.yaml"] = &fstest.MapFile{Data: append(fixture(t, "goreleaser.cleaned.yaml"), "\naurs:\n  - name: x\n"...)}
		wantHeld(t, CheckRelease(fsys), "goreleaser-top-level-key", "aurs")
	})
	t.Run("a second job with contents: write", func(t *testing.T) {
		fsys := cleanTree(t)
		ci := string(fixture(t, "ci.yml")) + "\n  extra:\n    runs-on: ubuntu-latest\n    permissions:\n      contents: write\n    steps:\n      - run: echo hi\n"
		fsys[".github/workflows/ci.yml"] = &fstest.MapFile{Data: []byte(ci)}
		wantHeld(t, CheckRelease(fsys), "workflow-permissions", "extra")
	})
	t.Run("a second job with contents: write beside fork-release.yml", func(t *testing.T) {
		fsys := cleanTree(t)
		src := forkReleaseY + "  extra:\n    runs-on: ubuntu-24.04\n    permissions:\n      contents: write\n    steps:\n      - run: echo hi\n"
		fsys[".github/workflows/fork-release.yml"] = &fstest.MapFile{Data: []byte(src)}
		wantHeld(t, CheckRelease(fsys), "workflow-permissions", "extra")
	})
	t.Run("an envOrDefault template", func(t *testing.T) {
		fsys := cleanTree(t)
		src := strings.Replace(string(fixture(t, "goreleaser.cleaned.yaml")), `name_template: "checksums.txt"`, `name_template: '{{ envOrDefault "X" "y" }}'`, 1)
		fsys[".goreleaser.yaml"] = &fstest.MapFile{Data: []byte(src)}
		wantHeld(t, CheckRelease(fsys), "goreleaser-template", "envOrDefault")
	})
	t.Run("mode replace", func(t *testing.T) {
		fsys := cleanTree(t)
		src := strings.Replace(string(fixture(t, "goreleaser.cleaned.yaml")), "mode: keep-existing", "mode: replace", 1)
		fsys[".goreleaser.yaml"] = &fstest.MapFile{Data: []byte(src)}
		wantHeld(t, CheckRelease(fsys), "goreleaser-release-pin", "release.mode", `"replace"`)
	})
	t.Run("replace_existing_artifacts true", func(t *testing.T) {
		fsys := cleanTree(t)
		src := strings.Replace(string(fixture(t, "goreleaser.cleaned.yaml")), "replace_existing_artifacts: false", "replace_existing_artifacts: true", 1)
		fsys[".goreleaser.yaml"] = &fstest.MapFile{Data: []byte(src)}
		wantHeld(t, CheckRelease(fsys), "goreleaser-release-pin", "release.replace_existing_artifacts", "true")
	})
	t.Run("the raw baseline shape is held by the wrong profile", func(t *testing.T) {
		fsys := cleanTree(t)
		raw := string(fixture(t, "goreleaser.cleaned.yaml"))
		raw = strings.Replace(raw, "mode: keep-existing", "mode: replace", 1)
		raw = strings.Replace(raw, "replace_existing_artifacts: false", "replace_existing_artifacts: true", 1)
		raw += "\nnfpms:\n  - id: x\n\naurs:\n  - name: x\n    private_key: \"{{ .Env.AUR_KEY }}\"\n\nhomebrew_casks:\n  - name: x\n\nwinget:\n  - name: x\n"
		fsys[".goreleaser.yaml"] = &fstest.MapFile{Data: []byte(raw)}
		for _, w := range []string{"release", "openapi-sync", "stats", "web"} {
			fsys[".github/workflows/"+w+".yml"] = &fstest.MapFile{Data: []byte("name: x\n")}
		}
		got := CheckRelease(fsys)
		for _, rule := range []string{"goreleaser-top-level-key", "goreleaser-release-pin", "goreleaser-template", "workflow-set"} {
			wantHeld(t, got, rule)
		}
		// Each name must be carried by a finding of the rule that holds it: the
		// rule and the name are checked on the same finding, not anywhere in
		// the verdict.
		named := func(rule, name string) {
			t.Helper()
			for _, f := range got {
				if f.Rule == rule && strings.Contains(f.String(), name) {
					return
				}
			}
			t.Fatalf("no %s finding names %q:\n%s", rule, name, details(got))
		}
		for _, name := range []string{"nfpms", "aurs", "homebrew_casks", "winget"} {
			named("goreleaser-top-level-key", name)
		}
		named("goreleaser-template", "AUR_KEY")
		for _, name := range []string{"web.yml", "stats.yml", "openapi-sync.yml", "release.yml"} {
			named("workflow-set", name)
		}
	})

	t.Run("findings are sorted and stable", func(t *testing.T) {
		fsys := cleanTree(t)
		fsys[".github/workflows/z.yml"] = &fstest.MapFile{Data: []byte("x: 1\n")}
		fsys[".github/workflows/a.yml"] = &fstest.MapFile{Data: []byte("x: 1\n")}
		fsys[".github/dependabot.yml"] = &fstest.MapFile{Data: []byte("version: 2\n")}
		first := CheckRelease(fsys)
		if len(first) != 3 {
			t.Fatalf("findings:\n%s", details(first))
		}
		if !reflect.DeepEqual(first, sortFindings(first)) {
			t.Fatalf("not sorted:\n%s", details(first))
		}
		for range 5 {
			if again := CheckRelease(fsys); !reflect.DeepEqual(first, again) {
				t.Fatalf("not deterministic:\n%s\n%s", details(first), details(again))
			}
		}
	})

	t.Run("a verdict names every held item", func(t *testing.T) {
		fsys := cleanTree(t)
		fsys[".github/workflows/new.yml"] = &fstest.MapFile{Data: []byte("x: 1\n")}
		fsys[".github/dependabot.yml"] = &fstest.MapFile{Data: []byte("version: 2\n")}
		fs := CheckRelease(fsys)
		v := verdict("release", fs)
		for _, f := range fs {
			if !strings.Contains(v, f.String()) {
				t.Fatalf("%q does not name %q", v, f)
			}
		}
	})
}

// A file that decodes to nothing is held by the public profile too, whichever of
// the checked files it is, and it is the only finding.
func TestCheckReleaseNullDocuments(t *testing.T) {
	targets := map[string]string{
		"goreleaser config": ".goreleaser.yaml",
		"ci workflow":       ".github/workflows/ci.yml",
		"fork workflow":     ".github/workflows/fork-release.yml",
	}
	for tname, path := range targets {
		for sname, src := range map[string]string{"null": "null\n", "tilde": "~\n", "explicit document null": "--- null\n"} {
			t.Run(tname+" "+sname, func(t *testing.T) {
				fsys := cleanTree(t)
				if path == ".github/workflows/fork-release.yml" {
					fsys[path] = &fstest.MapFile{Data: []byte(forkReleaseY)}
					if fs := CheckRelease(fsys); len(fs) != 0 {
						t.Fatalf("control: the tree with a sound fork workflow is not clean:\n%s", details(fs))
					}
				}
				fsys[path] = &fstest.MapFile{Data: []byte(src)}
				got := CheckRelease(fsys)
				if len(got) != 1 || got[0].Path != path || got[0].Rule != "yaml-parse" || !strings.Contains(got[0].Detail, "null") {
					t.Fatalf("want one yaml-parse finding naming null for %s, got:\n%s", path, details(got))
				}
			})
		}
	}
}

// The reviewer's counterexample, through the public profile: in the cleaned
// configuration an anchored sequence replaces the mapping merge. The sequence
// form is held; the direct form is the diagnostic control; the cleaned tree
// itself stays clean.
func TestCheckReleaseAnchoredSequenceMerge(t *testing.T) {
	withMerge := func(t *testing.T, to string) []Finding {
		fsys := cleanTree(t)
		fsys[".goreleaser.yaml"] = &fstest.MapFile{Data: []byte(mutate(t, "  - <<: *build\n", to))}
		return CheckRelease(fsys)
	}
	t.Run("control: the cleaned tree is clean", func(t *testing.T) {
		if fs := CheckRelease(cleanTree(t)); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
	for name, to := range map[string]string{
		"anchored sequence":       "  - <<: &merged [*build]\n",
		"direct sequence":         "  - <<: [*build]\n",
		"anchored block sequence": "  - <<: &merged\n      - *build\n",
	} {
		t.Run(name, func(t *testing.T) {
			got := withMerge(t, to)
			if len(got) != 1 || got[0].Path != ".goreleaser.yaml" || got[0].Rule != ruleYAMLMerge {
				t.Fatalf("want one %s finding for .goreleaser.yaml, got:\n%s", ruleYAMLMerge, details(got))
			}
		})
	}
}

// A configuration that gives one anchor name to two nodes is held by the public
// profile for that reason, and for no other: not as a sequence merge, and not
// let through. The four inputs are the executed counterexamples (two nesting a
// definition inside another of the same name, two merging an alias whose
// definition replays another). Each has a counterpart with unique names, which
// is classified as usual: a sequence merge is held, a mapping merge is clean.
func TestCheckReleaseReusedAnchors(t *testing.T) {
	const head = "version: 2\nbefore:\n  hooks:\n    - bash scripts/gen-completions.sh\n"
	const tail = "release:\n  mode: keep-existing\n  replace_existing_artifacts: false\n"
	check := func(t *testing.T, src string) []Finding {
		t.Helper()
		fsys := cleanTree(t)
		fsys[".goreleaser.yaml"] = &fstest.MapFile{Data: []byte(head + src + tail)}
		return CheckRelease(fsys)
	}

	reused := map[string]string{
		"nested, outer sequence":             "builds: &s\n  - &s {id: unix}\narchives:\n  - <<: *s\n    id: binary\n",
		"nested, outer mapping":              "builds: &s {k: &s [1]}\narchives:\n  - <<: *s\n    id: binary\n",
		"replayed through a merge, sequence": "builds:\n  - &a {k: &s [{id: unix}]}\n  - &s {id: unix}\narchives:\n  - <<: *a\n  - <<: *s\n",
		"replayed through a merge, mapping":  "builds:\n  - &a {k: &s {id: unix}}\n  - &s [{id: unix}]\narchives:\n  - <<: *a\n  - <<: *s\n",
	}
	for name, src := range reused {
		t.Run("held: "+name, func(t *testing.T) {
			got := check(t, src)
			if len(got) != 1 || got[0].Path != ".goreleaser.yaml" || got[0].Rule != ruleYAMLParse || !strings.Contains(got[0].Detail, "defined more than once") {
				t.Fatalf("want one %s finding for the reused anchor at .goreleaser.yaml, got:\n%s", ruleYAMLParse, details(got))
			}
		})
	}

	renamedSequence := map[string]string{
		"nested, outer sequence":            "builds: &o\n  - &i {id: unix}\narchives:\n  - <<: *o\n    id: binary\n",
		"replayed through a merge, mapping": "builds:\n  - &a {k: &s {id: unix}}\n  - &t [{id: unix}]\narchives:\n  - <<: *a\n  - <<: *t\n",
	}
	for name, src := range renamedSequence {
		t.Run("renamed, sequence merge held: "+name, func(t *testing.T) {
			got := check(t, src)
			if len(got) != 1 || got[0].Path != ".goreleaser.yaml" || got[0].Rule != ruleYAMLMerge {
				t.Fatalf("want one %s finding, got:\n%s", ruleYAMLMerge, details(got))
			}
		})
	}
	renamedMapping := map[string]string{
		"nested, outer mapping":              "builds: &o {k: &i [1]}\narchives:\n  - <<: *o\n    id: binary\n",
		"replayed through a merge, sequence": "builds:\n  - &a {k: &s [{id: unix}]}\n  - &t {id: unix}\narchives:\n  - <<: *a\n  - <<: *t\n",
	}
	for name, src := range renamedMapping {
		t.Run("renamed, mapping merge clean: "+name, func(t *testing.T) {
			if got := check(t, src); len(got) != 0 {
				t.Fatalf("findings:\n%s", details(got))
			}
		})
	}
}
