package main

import (
	"debug/buildinfo"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

const goodRevision = "0123456789abcdef0123456789abcdef01234567"

func TestOriginURLsOK(t *testing.T) {
	allowed := canonicalOrigins
	if len(allowed) != 3 {
		t.Fatalf("want the three canonical forms, got %v", allowed)
	}
	for _, want := range []string{
		"git@github.com:Ezopek/proton-cli.git",
		"https://github.com/Ezopek/proton-cli.git",
		"ssh://git@github.com/Ezopek/proton-cli.git",
	} {
		if !slices.Contains(allowed, want) {
			t.Fatalf("%s is not canonical: %v", want, allowed)
		}
	}

	for _, u := range allowed {
		t.Run("pass "+u, func(t *testing.T) {
			if err := originURLsOK([]string{u}, []string{u}, allowed); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("pass different canonical forms for fetch and push", func(t *testing.T) {
		if err := originURLsOK([]string{allowed[0]}, []string{allowed[1], allowed[2]}, allowed); err != nil {
			t.Fatal(err)
		}
	})

	bad := map[string]string{
		"upstream":             "https://github.com/roman-16/proton-cli.git",
		"other owner":          "https://github.com/Other/proton-cli.git",
		"other repo":           "https://github.com/Ezopek/other.git",
		"no .git suffix":       "https://github.com/Ezopek/proton-cli",
		"case differs":         "https://github.com/ezopek/proton-cli.git",
		"http":                 "http://github.com/Ezopek/proton-cli.git",
		"with a credential":    "https://token@github.com/Ezopek/proton-cli.git",
		"trailing slash":       "https://github.com/Ezopek/proton-cli.git/",
		"scp form of upstream": "git@github.com:roman-16/proton-cli.git",
		"a local path":         "/tmp/proton-cli",
		"empty":                "",
	}
	for name, u := range bad {
		t.Run("fetch refused: "+name, func(t *testing.T) {
			err := originURLsOK([]string{u}, []string{allowed[0]}, allowed)
			wantRefusal(t, err, "origin", "fetch")
			wantNoText(t, err, u)
		})
		t.Run("push refused: "+name, func(t *testing.T) {
			err := originURLsOK([]string{allowed[0]}, []string{u}, allowed)
			wantRefusal(t, err, "origin", "push")
			wantNoText(t, err, u)
		})
		t.Run("one bad among good refused: "+name, func(t *testing.T) {
			err := originURLsOK([]string{allowed[0], u, allowed[1]}, []string{allowed[0]}, allowed)
			wantRefusal(t, err, "origin", "fetch")
			wantNoText(t, err, u)
			err = originURLsOK([]string{allowed[0]}, []string{allowed[2], allowed[0], u}, allowed)
			wantRefusal(t, err, "origin", "push")
			wantNoText(t, err, u)
		})
	}
	t.Run("refused without any URL", func(t *testing.T) {
		wantRefusal(t, originURLsOK(nil, []string{allowed[0]}, allowed), "origin", "fetch")
		wantRefusal(t, originURLsOK([]string{allowed[0]}, nil, allowed), "origin", "push")
		wantRefusal(t, originURLsOK(nil, nil, allowed), "origin")
	})
	t.Run("an injected list replaces the canonical one", func(t *testing.T) {
		if err := originURLsOK([]string{"/scratch/o.git"}, []string{"/scratch/o.git"}, []string{"/scratch/o.git"}); err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, originURLsOK([]string{allowed[0]}, []string{allowed[0]}, []string{"/scratch/o.git"}), "origin")
	})
}

// wantRefusal fails unless err is a one-line refusal of the check whose text
// contains every needle.
func wantRefusal(t *testing.T, err error, check string, needles ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no refusal, want check %q", check)
	}
	r, ok := err.(refusal)
	if !ok || r.check != check {
		t.Fatalf("got %#v, want a refusal of check %q", err, check)
	}
	line := err.Error()
	if !strings.HasPrefix(line, "fork-release: "+check+": ") || strings.Contains(line, "\n") {
		t.Fatalf("not a one-line refusal: %q", line)
	}
	for _, n := range needles {
		if !strings.Contains(line, n) {
			t.Fatalf("refusal %q does not contain %q", line, n)
		}
	}
}

// wantNoText fails if a refusal prints text it must not, such as a URL.
func wantNoText(t *testing.T, err error, text string) {
	t.Helper()
	if text != "" && err != nil && strings.Contains(err.Error(), text) {
		t.Fatalf("the refusal prints %q: %v", text, err)
	}
}

func settings(kv ...string) *debug.BuildInfo {
	bi := &debug.BuildInfo{}
	for i := 0; i < len(kv); i += 2 {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
	}
	return bi
}

func TestBuildIdentity(t *testing.T) {
	t.Run("clean build passes and returns the revision", func(t *testing.T) {
		got, err := buildIdentity(settings("vcs.revision", goodRevision, "vcs.modified", "false", "vcs.time", "x"))
		if err != nil || got != goodRevision {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	refused := map[string]*debug.BuildInfo{
		"no build info":        nil,
		"no settings":          settings(),
		"no revision":          settings("vcs.modified", "false"),
		"no modified":          settings("vcs.revision", goodRevision),
		"empty revision":       settings("vcs.revision", "", "vcs.modified", "false"),
		"short revision":       settings("vcs.revision", goodRevision[:39], "vcs.modified", "false"),
		"long revision":        settings("vcs.revision", goodRevision+"0", "vcs.modified", "false"),
		"upper-case revision":  settings("vcs.revision", strings.ToUpper(goodRevision), "vcs.modified", "false"),
		"non-hex revision":     settings("vcs.revision", strings.Repeat("g", 40), "vcs.modified", "false"),
		"modified true":        settings("vcs.revision", goodRevision, "vcs.modified", "true"),
		"modified unreadable":  settings("vcs.revision", goodRevision, "vcs.modified", "maybe"),
		"modified empty":       settings("vcs.revision", goodRevision, "vcs.modified", ""),
		"modified capitalized": settings("vcs.revision", goodRevision, "vcs.modified", "False"),
	}
	for name, bi := range refused {
		t.Run("refused: "+name, func(t *testing.T) {
			got, err := buildIdentity(bi)
			wantRefusal(t, err, "build")
			if got != "" {
				t.Fatalf("returned %q beside a refusal", got)
			}
		})
	}
	t.Run("the reasons tell the causes apart", func(t *testing.T) {
		seen := map[string]string{}
		for name, bi := range map[string]*debug.BuildInfo{
			"none": nil, "rev": settings("vcs.modified", "false"), "mod": settings("vcs.revision", goodRevision),
			"bad": settings("vcs.revision", "zz", "vcs.modified", "false"), "dirty": settings("vcs.revision", goodRevision, "vcs.modified", "true"),
		} {
			_, err := buildIdentity(bi)
			if err == nil {
				t.Fatalf("%s: no refusal", name)
			}
			if other, dup := seen[err.Error()]; dup {
				t.Fatalf("%s and %s give the same reason: %s", name, other, err)
			}
			seen[err.Error()] = name
		}
	})
}

func TestAncestryDecision(t *testing.T) {
	if err := ancestryDecision(0); err != nil {
		t.Fatalf("status 0 is landed: %v", err)
	}
	notLanded := ancestryDecision(1)
	wantRefusal(t, notLanded, "ancestry", "not landed")
	for _, status := range []int{2, 127, 128, 129, -1, 255} {
		err := ancestryDecision(status)
		wantRefusal(t, err, "ancestry")
		if strings.Contains(err.Error(), "not landed") {
			t.Fatalf("status %d reads as not landed: %v", status, err)
		}
		if err.Error() == notLanded.Error() {
			t.Fatalf("status %d is not told apart from status 1", status)
		}
	}
}

func TestBuildRecipeIsTheOneTheTestsRun(t *testing.T) {
	const out = "<outside the clone>/fork-release"
	if got := "go " + strings.Join(buildRecipeArgs(out), " "); got != strings.ReplaceAll(buildRecipe, "<outside the clone>/fork-release", out) {
		t.Fatalf("recipe %q and argv %q disagree", buildRecipe, got)
	}
	if !strings.Contains(buildRecipe, "go build") || !strings.Contains(buildRecipe, "./scripts/fork-release") {
		t.Fatalf("recipe %q", buildRecipe)
	}
	if strings.Contains(buildRecipe, "-buildvcs") || strings.Contains(buildRecipe, "go run") {
		t.Fatalf("the recipe must not switch stamping off: %q", buildRecipe)
	}
}

// copyInternalSources copies the non-test Go files under the product's internal
// directory into the scratch clone, at the same paths. The helper imports
// internal/selfmanage (for the names of the release binaries), and that package
// imports others under internal, so a scratch build needs them all.
func copyInternalSources(t *testing.T, s *scratch) {
	t.Helper()
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s.write("internal/"+filepath.ToSlash(rel), string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// buildInScratchClone makes a fresh scratch repository holding copies of the
// helper's own non-test sources, the product sources under internal it imports
// and the module files, commits them, lets edit
// change the clone, and runs go with args (the documented recipe, or a
// variant) to build into a directory outside the clone. It returns the build
// information read from the binary and the commit.
func buildInScratchClone(t *testing.T, args func(out string) []string, edit func(s *scratch)) (*debug.BuildInfo, string) {
	t.Helper()
	s := newScratch(t)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		s.write("scripts/fork-release/"+name, string(data))
	}
	copyInternalSources(t, s)
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		s.write(name, string(data))
	}
	commit := s.commit("candidate")
	if edit != nil {
		edit(s)
	}

	out := filepath.Join(t.TempDir(), "fork-release")
	cmd := exec.Command("go", args(out)...)
	cmd.Dir = s.dir
	cmd.Env = append(s.env, "HOME="+os.Getenv("HOME"), "GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS=-mod=readonly", "GOWORK=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %v: %v\n%s", args(out), err, b)
	}
	bi, err := buildinfo.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return bi, commit
}

// The documented recipe, run in a clean scratch clone, stamps the revision and
// a clean tree, and a recipe or a clone that does not is detected.
func TestBuildRecipeStamps(t *testing.T) {
	t.Run("clean clone stamps vcs.revision and vcs.modified=false", func(t *testing.T) {
		bi, commit := buildInScratchClone(t, buildRecipeArgs, nil)
		got, err := buildIdentity(bi)
		if err != nil || got != commit {
			t.Fatalf("identity %q, %v; want %q", got, err, commit)
		}
		if v := settingOf(bi, "vcs.modified"); v != "false" {
			t.Fatalf("vcs.modified=%q", v)
		}
	})
	t.Run("an untracked file is detected as modified", func(t *testing.T) {
		bi, commit := buildInScratchClone(t, buildRecipeArgs, func(s *scratch) { s.write("stray.txt", "x") })
		if v := settingOf(bi, "vcs.revision"); v != commit {
			t.Fatalf("vcs.revision=%q, want %q", v, commit)
		}
		_, err := buildIdentity(bi)
		wantRefusal(t, err, "build", "modified")
	})
	t.Run("a modified tracked file is detected as modified", func(t *testing.T) {
		bi, _ := buildInScratchClone(t, buildRecipeArgs, func(s *scratch) {
			data, err := os.ReadFile("actions.go")
			if err != nil {
				t.Fatal(err)
			}
			s.write("scripts/fork-release/actions.go", string(data)+"\n// edited after the commit\n")
		})
		_, err := buildIdentity(bi)
		wantRefusal(t, err, "build", "modified")
	})
	t.Run("a recipe with -buildvcs=false is detected as unstamped", func(t *testing.T) {
		bi, _ := buildInScratchClone(t, func(out string) []string {
			return append([]string{"build", "-buildvcs=false"}, buildRecipeArgs(out)[1:]...)
		}, nil)
		if v := settingOf(bi, "vcs.revision"); v != "" {
			t.Fatalf("vcs.revision=%q, the variant should not stamp", v)
		}
		_, err := buildIdentity(bi)
		wantRefusal(t, err, "build", "revision")
	})
}

func settingOf(bi *debug.BuildInfo, key string) string {
	for _, s := range bi.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}
