package main

import (
	"crypto/sha1"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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

// rawBlobIDs are the git blob ids of the .github tree of the Upstream release
// v5.1.0 (commit 03f5c254dcd96582507dec2c4418e95d49cd8c6b), keyed by path
// under testdata/v5.1.0/github.
var rawBlobIDs = map[string]string{
	"workflows/ci.yml":           "8d0ee838323fee2adddfde8651ce6eefc65da17d",
	"workflows/openapi-sync.yml": "395660f6c60f032ee42665b29467874e7a934bde",
	"workflows/release.yml":      "df8dc81b2ac75598851affbe7dd55f57123c6d1d",
	"workflows/stats.yml":        "cc8e74f020342a5f4480f5c7918a3b1031d1fef6",
	"workflows/web.yml":          "d8668c4e9d2610046c0e0e098d4133f548ec9b79",
	"ISSUE_TEMPLATE/bug.yml":     "013c3eac4d4b9fe4b0097ef8d1c99b13e037ff5c",
	"ISSUE_TEMPLATE/config.yml":  "59451eac10c78478eeb9cdf3526f267b647d0dcb",
	"ISSUE_TEMPLATE/feature.yml": "aca540560d87834db53be4a22622c64fe76ad7de",
}

// blobID computes the id git gives a blob: SHA-1 of "blob <size>\0<data>".
func blobID(data []byte) string {
	h := sha1.New()
	h.Write([]byte("blob " + strconv.Itoa(len(data)) + "\x00"))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// rawTree is the frozen raw v5.1.0 .github tree, from committed bytes only.
func rawTree(t *testing.T) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	for name := range rawBlobIDs {
		m[".github/"+name] = &fstest.MapFile{Data: fixture(t, filepath.ToSlash(filepath.Join("v5.1.0", "github", name)))}
	}
	return m
}

func TestRawFixtureIsByteExact(t *testing.T) {
	if len(rawBlobIDs) != 8 {
		t.Fatalf("the fixture lists %d files, want 8", len(rawBlobIDs))
	}
	for name, want := range rawBlobIDs {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "v5.1.0", "github", filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			if got := blobID(data); got != want {
				t.Fatalf("blob id %s, want %s: the fixture drifted from v5.1.0", got, want)
			}
		})
	}
	t.Run("no other file", func(t *testing.T) {
		root := filepath.Join("testdata", "v5.1.0")
		var n int
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				n++
			}
			return err
		})
		if err != nil || n != len(rawBlobIDs) {
			t.Fatalf("found %d files under %s (err %v), want %d", n, root, err, len(rawBlobIDs))
		}
	})
}

var heldDisabledNames = []string{"release.yml", "openapi-sync.yml", "stats.yml", "web.yml"}

func TestCheckMirror(t *testing.T) {
	t.Run("the raw v5.1.0 tree passes", func(t *testing.T) {
		if got := CheckMirror(rawTree(t)); len(got) != 0 {
			t.Fatalf("findings:\n%s", details(got))
		}
	})
	t.Run("the raw v5.1.0 tree is held by release, naming the four workflows", func(t *testing.T) {
		got := CheckRelease(rawTree(t))
		for _, name := range heldDisabledNames {
			found := false
			for _, f := range got {
				if f.Rule == "workflow-set" && strings.HasSuffix(f.Path, "/"+name) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no workflow-set finding names %s:\n%s", name, details(got))
			}
		}
	})
	t.Run("the raw tree with only ci.yml passes, and a tree with no ci.yml is not held for that", func(t *testing.T) {
		fsys := rawTree(t)
		for _, name := range heldDisabledNames {
			delete(fsys, ".github/workflows/"+name)
		}
		if got := CheckMirror(fsys); len(got) != 0 {
			t.Fatalf("findings:\n%s", details(got))
		}
		delete(fsys, ".github/workflows/ci.yml")
		if got := CheckMirror(fsys); len(got) != 0 {
			t.Fatalf("findings:\n%s", details(got))
		}
	})
	t.Run("an empty tree passes", func(t *testing.T) {
		if got := CheckMirror(fstest.MapFS{}); len(got) != 0 {
			t.Fatalf("findings:\n%s", details(got))
		}
	})
	t.Run("the cleaned fork tree passes mirror when it has no fork-release.yml", func(t *testing.T) {
		if got := CheckMirror(cleanTree(t)); len(got) != 0 {
			t.Fatalf("findings:\n%s", details(got))
		}
	})
	t.Run("an ezop-derived tree with fork-release.yml is held", func(t *testing.T) {
		fsys := cleanTree(t)
		fsys[".github/workflows/fork-release.yml"] = &fstest.MapFile{Data: []byte(forkReleaseY)}
		wantHeld(t, CheckMirror(fsys), "workflow-set", "fork-release.yml")
		if got := CheckRelease(fsys); len(got) != 0 {
			t.Fatalf("control: release holds the derived tree:\n%s", details(got))
		}
	})

	mutate := func(t *testing.T, edit func(m fstest.MapFS)) []Finding {
		t.Helper()
		fsys := rawTree(t)
		if got := CheckMirror(fsys); len(got) != 0 {
			t.Fatalf("control: the raw tree is held:\n%s", details(got))
		}
		edit(fsys)
		return CheckMirror(fsys)
	}
	set := func(path, content string) func(m fstest.MapFS) {
		return func(m fstest.MapFS) { m[path] = &fstest.MapFile{Data: []byte(content)} }
	}
	ciWith := func(extra string) string {
		return job(extra)
	}

	t.Run("a new workflow is held", func(t *testing.T) {
		wantHeld(t, mutate(t, set(".github/workflows/new.yml", "x: 1\n")), "workflow-set", "new.yml")
	})
	t.Run("a local action is held", func(t *testing.T) {
		wantHeld(t, mutate(t, set(".github/actions/x/action.yml", "name: x\n")), "github-file", "actions/x")
	})
	t.Run("dependabot.yml is held", func(t *testing.T) {
		wantHeld(t, mutate(t, set(".github/dependabot.yml", "version: 2\n")), "github-file", "dependabot.yml")
	})
	t.Run("ci.yml with a write scope is held", func(t *testing.T) {
		got := mutate(t, set(".github/workflows/ci.yml", ciWith("    permissions:\n      contents: write\n"+steps("run: echo hi"))))
		wantHeld(t, got, "workflow-permissions", "ci.yml")
	})
	t.Run("ci.yml with secrets.X is held", func(t *testing.T) {
		got := mutate(t, set(".github/workflows/ci.yml", ciWith(steps("run: echo hi\n        env:\n          X: ${{ secrets.X }}"))))
		wantHeld(t, got, "workflow-secrets", "ci.yml")
	})
	t.Run("ci.yml with a curl step is held", func(t *testing.T) {
		wantHeld(t, mutate(t, set(".github/workflows/ci.yml", ciWith(steps("run: curl example.test")))), "workflow-command", "curl")
	})
	t.Run("ci.yml with an unlisted action SHA is held", func(t *testing.T) {
		got := mutate(t, set(".github/workflows/ci.yml", ciWith(steps("uses: actions/checkout@"+shaUnlisted))))
		wantHeld(t, got, "workflow-action", shaUnlisted)
	})
	t.Run("ci.yml with a listed action passes", func(t *testing.T) {
		got := mutate(t, set(".github/workflows/ci.yml", ciWith(steps(okAction("actions/checkout")))))
		if len(got) != 0 {
			t.Fatalf("findings:\n%s", details(got))
		}
	})
	t.Run("ci.yml with a tag-reachable trigger is held", func(t *testing.T) {
		src := "on:\n  push:\n    tags: ['v*']\npermissions:\n  contents: read\njobs:\n  build:\n    runs-on: ubuntu-24.04\n" + steps("run: echo hi")
		wantHeld(t, mutate(t, set(".github/workflows/ci.yml", src)), "workflow-trigger", "ci.yml")
	})
	for _, name := range heldDisabledNames {
		path := ".github/workflows/" + name
		t.Run(name+" with a push trigger with tags is held", func(t *testing.T) {
			got := mutate(t, set(path, "on:\n  push:\n    tags: ['v*']\n"))
			wantHeld(t, got, "workflow-trigger", name)
		})
		t.Run(name+" with any content but no tag-reachable trigger passes", func(t *testing.T) {
			src := "on:\n  push:\n    branches: [main]\npermissions: write-all\njobs:\n  x:\n    steps:\n      - run: curl ${{ secrets.X }}\n      - uses: evil/x@v1\n"
			if got := mutate(t, set(path, src)); len(got) != 0 {
				t.Fatalf("findings:\n%s", details(got))
			}
		})
		t.Run(name+" unparseable is held", func(t *testing.T) {
			wantHeld(t, mutate(t, set(path, "on: [push\n")), "yaml-parse", name)
		})
		t.Run(name+" that is not a mapping is held", func(t *testing.T) {
			wantHeld(t, mutate(t, set(path, "- a\n")), "workflow-shape", name)
		})
		t.Run(name+" that is null is held", func(t *testing.T) {
			wantHeld(t, mutate(t, set(path, "null\n")), "yaml-parse", name)
		})
		t.Run(name+" with a duplicate key is held", func(t *testing.T) {
			wantHeld(t, mutate(t, set(path, "on: pull_request\non: pull_request\n")), "yaml-duplicate-key", name)
		})
		t.Run(name+" as a symlink is held", func(t *testing.T) {
			got := mutate(t, func(m fstest.MapFS) {
				m[path] = &fstest.MapFile{Data: []byte("../x"), Mode: fs.ModeSymlink}
			})
			wantHeld(t, got, "github-non-regular", name)
		})
	}
	t.Run("a symlink in ISSUE_TEMPLATE is held", func(t *testing.T) {
		got := mutate(t, func(m fstest.MapFS) {
			m[".github/ISSUE_TEMPLATE/bug.yml"] = &fstest.MapFile{Data: []byte("../x"), Mode: fs.ModeSymlink}
		})
		wantHeld(t, got, "github-non-regular", "bug.yml")
	})
	t.Run("an ISSUE_TEMPLATE file of any content is inert", func(t *testing.T) {
		got := mutate(t, set(".github/ISSUE_TEMPLATE/anything.md", "secrets.X curl ${{ }}"))
		if len(got) != 0 {
			t.Fatalf("findings:\n%s", details(got))
		}
	})
	t.Run("findings are sorted and a verdict names them", func(t *testing.T) {
		got := mutate(t, func(m fstest.MapFS) {
			set(".github/workflows/z.yml", "x: 1\n")(m)
			set(".github/dependabot.yml", "version: 2\n")(m)
		})
		if len(got) != 2 || !reflect.DeepEqual(got, sortFindings(got)) {
			t.Fatalf("findings:\n%s", details(got))
		}
		v := verdict("mirror", got)
		if !strings.HasPrefix(v, "mirror: held, 2 item(s)\n") {
			t.Fatalf("verdict %q", v)
		}
	})
}

// recordingFS records every name opened through it.
type recordingFS struct {
	inner  fs.FS
	opened *[]string
}

func (r recordingFS) Open(name string) (fs.File, error) {
	*r.opened = append(*r.opened, name)
	return r.inner.Open(name)
}

// The mirror profile reads only .github/. A fake file system records every
// path it is asked for.
func TestCheckMirrorReadsOnlyGithub(t *testing.T) {
	inner := rawTree(t)
	inner[".goreleaser.yaml"] = &fstest.MapFile{Data: []byte("aurs: broken: [\n")}
	inner["go.mod"] = &fstest.MapFile{Data: []byte("module x\n")}
	inner["docs/a.md"] = &fstest.MapFile{Data: []byte("x")}
	var opened []string
	if got := CheckMirror(recordingFS{inner, &opened}); len(got) != 0 {
		t.Fatalf("findings:\n%s", details(got))
	}
	if len(opened) == 0 {
		t.Fatal("nothing was read")
	}
	for _, name := range opened {
		if name != ".github" && !strings.HasPrefix(name, ".github/") {
			t.Fatalf("mirror asked for %q, outside .github", name)
		}
	}
}
