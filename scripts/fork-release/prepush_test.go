package main

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

// ---- guard --pre-push TAG C: the wire ----

func TestGuardPrePushShape(t *testing.T) {
	const tag = "v5.1.0-ezop.1"
	for name, args := range map[string][]string{
		"no argument":              {"--pre-push"},
		"tag only":                 {"--pre-push", tag},
		"extra argument":           {"--pre-push", tag, targetSHA, "extra"},
		"empty tag":                {"--pre-push", "", targetSHA},
		"empty commit":             {"--pre-push", tag, ""},
		"tag is not a Release Tag": {"--pre-push", "v5.1.0", targetSHA},
		"tag with a leading zero":  {"--pre-push", "v5.1.0-ezop.01", targetSHA},
		"tag with a newline":       {"--pre-push", tag + "\n", targetSHA},
		"tag placeholder":          {"--pre-push", "T", targetSHA},
		"commit is short":          {"--pre-push", tag, targetSHA[:39]},
		"commit is abbreviated":    {"--pre-push", tag, "abc"},
		"commit is long":           {"--pre-push", tag, targetSHA + "3"},
		"commit is upper case":     {"--pre-push", tag, strings.ToUpper("abcdef0123456789abcdef0123456789abcdef01")},
		"commit is a branch":       {"--pre-push", tag, "ezop"},
		"commit is HEAD":           {"--pre-push", tag, "HEAD"},
		"commit is not hex":        {"--pre-push", tag, strings.Repeat("g", 40)},
		"commit has a newline":     {"--pre-push", tag, targetSHA + "\n"},
		"commit starts with dash":  {"--pre-push", tag, "-" + targetSHA[1:]},
		"equals form":              {"--pre-push=" + tag, targetSHA},
		"flag after the values":    {tag, targetSHA, "--pre-push"},
		"flag twice":               {"--pre-push", "--pre-push", tag, targetSHA},
		"upper-case flag":          {"--PRE-PUSH", tag, targetSHA},
		"with a profile":           {"--pre-push", "--profile", "release", targetSHA},
	} {
		t.Run(name, func(t *testing.T) {
			w := newInRunWorld(t)
			code, out, errb := w.run(t, args...)
			if code != 2 || out != "" || errb != guardUsage+"\n" {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
			}
			if len(w.git.calls) != 0 || len(w.gh.calls) != 0 || len(w.envReads) != 0 {
				t.Fatalf("something was read: git %v, GitHub %v, environment %v", w.git.calls, w.gh.calls, w.envReads)
			}
		})
	}
	t.Run("the usage line names the mode", func(t *testing.T) {
		if !strings.Contains(guardUsage, "--pre-push TAG COMMIT") {
			t.Fatalf("usage %q", guardUsage)
		}
		if !strings.HasPrefix(guardUsage, "fork-release: usage: ") || strings.Contains(guardUsage, "\n") {
			t.Fatalf("usage %q", guardUsage)
		}
	})
	t.Run("a well-formed command line is the mode, not usage", func(t *testing.T) {
		w := newInRunWorld(t)
		w.git.fetchURLs = []string{"https://example.invalid/other.git"}
		code, out, errb := w.run(t, "--pre-push", inRunTag, targetSHA)
		if code != 1 || out != "" || !strings.HasPrefix(errb, "fork-release: origin: ") {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
}

// ---- guard --pre-push TAG C: the checks over fakes ----

// ppWorkflow is the real fork-release.yml, which passes the release profile,
// with its top-level env block (the GoReleaser pin) replaced by pinLine.
func ppWorkflow(pinLine string) string {
	data, err := fs.ReadFile(repoRoot(), forkReleasePath)
	if err != nil {
		panic("the real " + forkReleasePath + " cannot be read: " + err.Error())
	}
	const envBlock = "env:\n  GORELEASER_PIN: " + goodPin + "\n\n"
	if strings.Count(string(data), envBlock) != 1 {
		panic("the real " + forkReleasePath + " does not carry its pin env block exactly once")
	}
	return strings.Replace(string(data), envBlock, pinLine, 1)
}

// ppGit is a stepGit with the tag fetch of the real adapter, per-pair ancestry
// answers and a failing tag listing, each recorded in the call log of the fake.
type ppGit struct {
	*stepGit
	tagsErr      error
	fetchTagsErr error
	tagFetches   [][]string
	ancestry     map[string]int
}

func (g *ppGit) OriginTags() (map[string]string, error) {
	g.calls = append(g.calls, "origin tags")
	if g.tagsErr != nil {
		return nil, g.tagsErr
	}
	return g.tags, nil
}

func (g *ppGit) FetchTags(names []string) error {
	g.calls = append(g.calls, "fetch tags "+strings.Join(names, " "))
	g.tagFetches = append(g.tagFetches, slices.Clone(names))
	return g.fetchTagsErr
}

func (g *ppGit) IsAncestor(a, d string) (int, error) {
	status, err := g.fakeGit.IsAncestor(a, d)
	if s, ok := g.ancestry[a+" "+d]; ok {
		return s, err
	}
	return status, err
}

// ppWorld is one run of guard --pre-push: the git fake, the GitHub fake, the
// helper's build information and a record of the environment reads.
type ppWorld struct {
	git      *ppGit
	repo     gitRepo // what the command is given; the git fake unless a test swaps it
	gh       *routedGH
	build    func() (*debug.BuildInfo, bool)
	out      io.Writer
	envReads []string
}

const ppTag = inRunTag

func newPPWorld(t *testing.T) *ppWorld {
	t.Helper()
	tree := cleanTree(t)
	tree["go.mod"] = fstestFile(goodMod)
	tree["devbox.lock"] = fstestFile(goodLock)
	tree[".github/workflows/fork-release.yml"] = fstestFile(ppWorkflow("env:\n  GORELEASER_PIN: " + goodPin + "\n"))
	sg := &stepGit{fakeGit: newFakeGit(tree), tags: map[string]string{"v5.1.0": helperSHA}}
	g := &ppGit{stepGit: sg, ancestry: map[string]int{}}
	return &ppWorld{
		git:  g,
		repo: g,
		gh:   &routedGH{record: absentRecordWorld(), pages: pagesOf("[]")},
		build: func() (*debug.BuildInfo, bool) {
			return settings("vcs.revision", targetSHA, "vcs.modified", "false"), true
		},
	}
}

func (w *ppWorld) run(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	d := guardDeps{
		git: w.repo, build: w.build, origins: canonicalOrigins, stderr: &errb, gh: w.gh,
		getenv: func(name string) string { w.envReads = append(w.envReads, name); return "" },
	}
	d.stdout = &out
	if w.out != nil {
		d.stdout = w.out
	}
	if len(args) == 0 {
		args = []string{ppTag, targetSHA}
	}
	code = run(append([]string{"guard", "--pre-push"}, args...), d)
	return code, out.String(), errb.String()
}

// refused runs the mode and requires exit 1, nothing on standard output and
// exactly the lines of one check on standard error; it returns the lines.
func (w *ppWorld) refused(t *testing.T, check string) []string {
	t.Helper()
	code, out, errb := w.run()
	if code != 1 || out != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
	}
	lines := strings.Split(strings.TrimSuffix(errb, "\n"), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "fork-release: "+check+": ") || strings.TrimSpace(line) != line {
			t.Fatalf("line %q is not a refusal of %q (stderr %q)", line, check, errb)
		}
		for _, leak := range []string{"http", "SECRET", "gh failed", "github.com"} {
			if strings.Contains(line, leak) {
				t.Fatalf("line %q quotes %q", line, leak)
			}
		}
	}
	return lines
}

// callsWithout is the git call log without the blob reads.
func (w *ppWorld) callsWithout() []string {
	var calls []string
	for _, c := range w.git.calls {
		if c != "blob" {
			calls = append(calls, c)
		}
	}
	return calls
}

func (w *ppWorld) did(prefix string) bool {
	return slices.ContainsFunc(w.git.calls, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

func TestGuardPrePushPasses(t *testing.T) {
	want := targetSHA + ":refs/tags/" + ppTag + "\n"
	t.Run("first valid run prints exactly the SHA-bound refspec", func(t *testing.T) {
		w := newPPWorld(t)
		code, out, errb := w.run()
		if code != 0 || out != want || errb != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		if !fullSHA.MatchString(strings.SplitN(out, ":", 2)[0]) || strings.ContainsAny(out, "+*") || strings.HasPrefix(out, "refs/") {
			t.Fatalf("refspec %q", out)
		}
	})
	t.Run("the order of the reads", func(t *testing.T) {
		w := newPPWorld(t)
		if code, _, errb := w.run(); code != 0 {
			t.Fatal(errb)
		}
		wantCalls := []string{
			"urls push=false", "urls push=true",
			"fetch", "resolve " + targetSHA, "ancestor " + targetSHA + " " + ezopTipSHA,
			"origin tags",
			"fetch tags v5.1.0",
			"ancestor " + helperSHA + " " + targetSHA,
			"tree " + targetSHA + " " + strings.Join(profilePaths("release"), ","),
			"tree " + targetSHA + " " + forkReleasePath,
			"tree " + targetSHA + " " + strings.Join(toolchainPaths, ","),
		}
		if got := w.callsWithout(); !slices.Equal(got, wantCalls) {
			t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(wantCalls, "\n"))
		}
		if len(w.gh.calls) != 6 || w.gh.calls[0] != epRef {
			t.Fatalf("GitHub reads %v", w.gh.calls)
		}
	})
	t.Run("an ancestor of the fetched tip passes", func(t *testing.T) {
		w := newPPWorld(t)
		w.git.ancestry[targetSHA+" "+ezopTipSHA] = 0
		w.git.resolve[ezopTipSHA] = ezopTipSHA
		if code, out, errb := w.run(); code != 0 || out != want || errb != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("the candidate is the fetched tip", func(t *testing.T) {
		w := newPPWorld(t)
		w.git.fetchTip = targetSHA
		if code, out, errb := w.run(); code != 0 || out != want || errb != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("the Upstream tags fetched are only the Upstream release tags, sorted, by name", func(t *testing.T) {
		w := newPPWorld(t)
		w.git.tags = map[string]string{
			"v5.1.0": helperSHA, "v5.0.0": helperSHA, "v5.1.0-rc.1": helperSHA,
			"v5.0.0-ezop.1": helperSHA, "v4.9": helperSHA, "stable": helperSHA,
		}
		if code, _, errb := w.run(); code != 0 {
			t.Fatal(errb)
		}
		if len(w.git.tagFetches) != 1 || !slices.Equal(w.git.tagFetches[0], []string{"v5.0.0", "v5.1.0"}) {
			t.Fatalf("fetches %v", w.git.tagFetches)
		}
	})
	t.Run("it makes only reads", func(t *testing.T) {
		w := newPPWorld(t)
		if code, _, errb := w.run(); code != 0 {
			t.Fatal(errb)
		}
		for _, c := range w.git.calls {
			if strings.Contains(c, "HEAD") || strings.Contains(c, "refs/tags") || strings.HasPrefix(c, "push") || strings.HasPrefix(c, "tag") {
				t.Fatalf("call %q", c)
			}
		}
		for _, c := range w.gh.calls {
			if strings.HasPrefix(c, "actions") || strings.Contains(c, "/actions") {
				t.Fatalf("GitHub call %q reads Actions state", c)
			}
		}
		if len(w.envReads) != 0 {
			t.Fatalf("environment reads %v", w.envReads)
		}
	})
	t.Run("the pin and the lock are read from the tree of the commit", func(t *testing.T) {
		w := newPPWorld(t)
		if code, _, errb := w.run(); code != 0 {
			t.Fatal(errb)
		}
		if !slices.Contains(w.git.reads, forkReleasePath) || !slices.Contains(w.git.reads, "go.mod") || !slices.Contains(w.git.reads, "devbox.lock") {
			t.Fatalf("blobs read %v", w.git.reads)
		}
	})
}

func TestGuardPrePushRefusals(t *testing.T) {
	type row struct {
		name   string
		mutate func(w *ppWorld)
		check  string
		// notAfter are call prefixes that must not appear in the git log: the
		// reads that come after the refusing check.
		notAfter []string
		gh       bool // GitHub must not be read
	}
	var (
		afterOrigin    = []string{"fetch", "origin tags", "fetch tags", "tree", "ancestor"}
		afterBuild     = afterOrigin
		afterReach     = []string{"origin tags", "fetch tags", "tree"}
		afterTag       = []string{"fetch tags", "tree"}
		afterFetchTags = []string{"tree"}
		afterBase      = []string{"tree"}
		afterProfile   = []string{"tree " + targetSHA + " " + forkReleasePath, "tree " + targetSHA + " " + strings.Join(toolchainPaths, ",")}
		afterToolchain = []string(nil)
	)
	withTag := func(w *ppWorld, name, sha string) { w.git.tags[name] = sha }
	noRelease := func(w *ppWorld, path string) { delete(w.git.tree, path) }
	rows := []row{
		{"a wrong fetch URL", func(w *ppWorld) { w.git.fetchURLs = []string{"https://example.invalid/SECRET.git"} }, "origin", afterOrigin, true},
		{"a wrong push URL", func(w *ppWorld) { w.git.pushURLs = []string{"https://example.invalid/SECRET.git"} }, "origin", afterOrigin, true},
		{"one bad push URL among good ones", func(w *ppWorld) { w.git.pushURLs = append(slices.Clone(canonicalOrigins), "git@example.invalid:x.git") }, "origin", afterOrigin, true},
		{"an unreadable origin", func(w *ppWorld) { w.git.urlsErr = errors.New("boom SECRET") }, "origin", afterOrigin, true},
		{"no embedded build information", func(w *ppWorld) { w.build = func() (*debug.BuildInfo, bool) { return nil, false } }, "build", afterBuild, true},
		{"a helper from another commit", func(w *ppWorld) {
			w.build = func() (*debug.BuildInfo, bool) {
				return settings("vcs.revision", helperSHA, "vcs.modified", "false"), true
			}
		}, "build", afterBuild, true},
		{"a modified build", func(w *ppWorld) {
			w.build = func() (*debug.BuildInfo, bool) {
				return settings("vcs.revision", targetSHA, "vcs.modified", "true"), true
			}
		}, "build", afterBuild, true},
		{"no vcs.modified", func(w *ppWorld) {
			w.build = func() (*debug.BuildInfo, bool) { return settings("vcs.revision", targetSHA), true }
		}, "build", afterBuild, true},
		{"a failed fetch of ezop", func(w *ppWorld) { w.git.fetchErr = errors.New("boom SECRET") }, "fetch", afterReach, true},
		{"a malformed fetched tip", func(w *ppWorld) { w.git.fetchTip = "not-a-sha" }, "fetch", afterReach, true},
		{"a candidate that is no commit", func(w *ppWorld) { delete(w.git.resolve, targetSHA) }, "reachable", afterReach, true},
		{"a candidate that is an annotated-tag object", func(w *ppWorld) { w.git.resolve[targetSHA] = relOther }, "reachable", afterReach, true},
		{"a candidate that is not on origin/ezop", func(w *ppWorld) { w.git.ancestry[targetSHA+" "+ezopTipSHA] = 1 }, "reachable", afterReach, true},
		{"an undecided ancestry", func(w *ppWorld) { w.git.ancestry[targetSHA+" "+ezopTipSHA] = 128 }, "reachable", afterReach, true},
		{"a failed ancestry run", func(w *ppWorld) { w.git.ancestryErr = errors.New("boom SECRET") }, "reachable", afterReach, true},
		{"the tag already on origin at the commit", func(w *ppWorld) { withTag(w, ppTag, targetSHA) }, "tag", afterTag, true},
		{"the tag already on origin at another commit", func(w *ppWorld) { withTag(w, ppTag, relOther) }, "tag", afterTag, true},
		{"a failed tag listing", func(w *ppWorld) { w.git.tagsErr = errors.New("boom SECRET") }, "tag", afterTag, true},
		{"a failed fetch of the Upstream tags", func(w *ppWorld) { w.git.fetchTagsErr = errors.New("boom SECRET") }, "fetch", afterFetchTags, true},
		{"a git without the tag fetch", func(w *ppWorld) { w.repo = w.git.stepGit }, "fetch", afterFetchTags, true},
		{"no Upstream tag listed", func(w *ppWorld) { w.git.tags = map[string]string{"v5.1.0-rc.1": helperSHA, "other": helperSHA} }, "base", afterBase, true},
		{"no tag at all", func(w *ppWorld) { w.git.tags = map[string]string{} }, "base", afterBase, true},
		{"an Upstream base other than the tag's", func(w *ppWorld) { w.git.tags = map[string]string{"v5.0.0": helperSHA} }, "base", afterBase, true},
		{"the Upstream tag is not an ancestor", func(w *ppWorld) { w.git.ancestry[helperSHA+" "+targetSHA] = 1 }, "base", afterBase, true},
		{"an undecided Upstream ancestry", func(w *ppWorld) { w.git.ancestry[helperSHA+" "+targetSHA] = 128 }, "base", afterBase, true},
		{"two base tags with one numeric version", func(w *ppWorld) { withTag(w, "v5.01.0", helperSHA) }, "base", afterBase, true},
		{"a held profile", func(w *ppWorld) { w.git.tree[".github/workflows/release.yml"] = fstestFile("name: Release\n") }, "profile", afterProfile, true},
		{"an unreadable tree", func(w *ppWorld) { w.git.treeErr = errors.New("boom SECRET") }, "profile", afterProfile, true},
		{"no fork-release.yml", func(w *ppWorld) { noRelease(w, forkReleasePath) }, "toolchain", afterToolchain, true},
		{"no pin", func(w *ppWorld) { w.git.tree[forkReleasePath] = fstestFile(ppWorkflow("")) }, "profile", afterProfile, true},
		{"a pin that differs from the lock", func(w *ppWorld) {
			w.git.tree[forkReleasePath] = fstestFile(ppWorkflow("env:\n  GORELEASER_PIN: v2.99.0\n"))
		}, "toolchain", afterToolchain, true},
		{"a pin without v", func(w *ppWorld) {
			w.git.tree[forkReleasePath] = fstestFile(ppWorkflow("env:\n  GORELEASER_PIN: 2.17.1\n"))
		}, "profile", afterProfile, true},
		{"a pin only in a job", func(w *ppWorld) {
			w.git.tree[forkReleasePath] = fstestFile(strings.Replace(ppWorkflow(""), "    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    env:\n      GORELEASER_PIN: "+goodPin+"\n", 1))
		}, "profile", afterProfile, true},
		{"a repeated pin is held by the profile before the pin is read", func(w *ppWorld) {
			w.git.tree[forkReleasePath] = fstestFile(ppWorkflow("env:\n  GORELEASER_PIN: " + goodPin + "\n  GORELEASER_PIN: " + goodPin + "\n"))
		}, "profile", afterProfile, true},
		{"no go.mod", func(w *ppWorld) { noRelease(w, "go.mod") }, "toolchain", afterToolchain, true},
		{"no devbox.lock", func(w *ppWorld) { noRelease(w, "devbox.lock") }, "toolchain", afterToolchain, true},
		{"a lock with another GoReleaser", func(w *ppWorld) {
			w.git.tree["devbox.lock"] = fstestFile(strings.Replace(goodLock, `"version":"2.17.1"`, `"version":"2.16.0"`, 1))
		}, "toolchain", afterToolchain, true},
		{"a lock with another Go", func(w *ppWorld) {
			w.git.tree["go.mod"] = fstestFile(strings.Replace(goodMod, "go 1.27.0", "go 1.26.0", 1))
		}, "toolchain", afterToolchain, true},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			control := newPPWorld(t)
			if code, _, errb := control.run(); code != 0 {
				t.Fatalf("control: exit %d, %q", code, errb)
			}
			w := newPPWorld(t)
			r.mutate(w)
			lines := w.refused(t, r.check)
			if len(lines) == 0 {
				t.Fatal("no refusal line")
			}
			for _, prefix := range r.notAfter {
				if w.did(prefix) {
					t.Fatalf("%q was read after the refusal: %v", prefix, w.git.calls)
				}
			}
			if r.gh && len(w.gh.calls) != 0 {
				t.Fatalf("GitHub was read: %v", w.gh.calls)
			}
			if len(w.envReads) != 0 {
				t.Fatalf("environment reads %v", w.envReads)
			}
		})
	}
	t.Run("a held profile gives one line per finding", func(t *testing.T) {
		w := newPPWorld(t)
		w.git.tree[".github/workflows/release.yml"] = fstestFile("name: Release\n")
		w.git.tree[".github/workflows/stats.yml"] = fstestFile("name: Stats\n")
		if lines := w.refused(t, "profile"); len(lines) < 2 {
			t.Fatalf("lines %v", lines)
		}
	})
}

func TestGuardPrePushLookups(t *testing.T) {
	for name, make := range recordWorlds {
		t.Run("record "+name, func(t *testing.T) {
			w := newPPWorld(t)
			w.gh.record = make()
			if name == "Absent" {
				if code, _, errb := w.run(); code != 0 {
					t.Fatalf("control: exit %d, %q", code, errb)
				}
				return
			}
			w.refused(t, "repeat")
		})
	}
	for _, state := range []string{"None", "Published-Same", "Draft", "Other", "Unknown"} {
		t.Run("release "+state, func(t *testing.T) {
			w := newPPWorld(t)
			w.gh.pages = releasePages(state)
			if state == "None" {
				if code, _, errb := w.run(); code != 0 {
					t.Fatalf("control: exit %d, %q", code, errb)
				}
				return
			}
			w.refused(t, "repeat")
		})
	}
	t.Run("no GitHub reader refuses", func(t *testing.T) {
		w := newPPWorld(t)
		var out, errb bytes.Buffer
		d := guardDeps{git: w.repo, build: w.build, origins: canonicalOrigins, stdout: &out, stderr: &errb}
		if code := run([]string{"guard", "--pre-push", ppTag, targetSHA}, d); code != 1 || out.Len() != 0 || !strings.HasPrefix(errb.String(), "fork-release: repeat: ") {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, &out, &errb)
		}
	})
	t.Run("the lookups are the last reads", func(t *testing.T) {
		w := newPPWorld(t)
		w.gh.pages = releasePages("Draft")
		w.refused(t, "repeat")
		if !w.did("tree "+targetSHA+" "+strings.Join(toolchainPaths, ",")) || len(w.gh.calls) == 0 {
			t.Fatalf("git %v, GitHub %v", w.git.calls, w.gh.calls)
		}
		for _, c := range w.gh.calls {
			if strings.HasPrefix(c, "actions") {
				t.Fatalf("GitHub call %q", c)
			}
		}
	})
}

func TestGuardPrePushOutputFailure(t *testing.T) {
	w := newPPWorld(t)
	w.out = errWriter{}
	code, out, errb := w.run()
	if code != 1 || out != "" || !strings.HasPrefix(errb, "fork-release: output: ") || strings.Count(errb, "\n") != 1 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
	}
}

// ---- guard --pre-push TAG C: end to end with the real git adapter ----

func TestGuardPrePushEndToEnd(t *testing.T) {
	bareDir := newBare(t)
	src := newScratch(t)
	src.git("remote", "add", "origin", bareDir)
	src.write("go.mod", goodMod)
	src.write("devbox.lock", goodLock)
	upstream := src.commit("upstream release")
	src.git("tag", "-a", "-m", "upstream", "v5.1.0", upstream)
	src.write(".goreleaser.yaml", string(fixture(t, "goreleaser.cleaned.yaml")))
	src.write(".github/workflows/ci.yml", string(fixture(t, "ci.yml")))
	src.write(".github/workflows/fork-release.yml", ppWorkflow("env:\n  GORELEASER_PIN: "+goodPin+"\n"))
	landed := src.commit("fork commit")
	src.git("branch", "-m", "ezop")
	src.write("later.txt", "x")
	tip := src.commit("later, the tip")
	// A pull request head that was never merged, and an Upstream tag on a commit
	// that ezop does not contain.
	src.git("checkout", "-q", "-b", "pr", landed)
	src.write("pr.txt", "x")
	head := src.commit("a head that was never merged")
	src.git("tag", "-a", "-m", "newer upstream", "v5.2.0", head)
	src.git("push", "-q", "origin", "ezop", "pr", "refs/tags/v5.1.0", "refs/tags/v5.2.0")

	// The Operator's clone: it reaches the origin over file:// so that objects
	// really travel. Like any clone it already holds origin/ezop (git updates that
	// remote-tracking ref when the guard fetches ezop, so the ref list is the same
	// before and after a run only when the clone is current), but it holds neither
	// the pull request head nor the Upstream tag objects.
	originURL := "file://" + bareDir
	newClone := func() *scratch {
		w := newScratch(t)
		w.git("remote", "add", "origin", originURL)
		w.git("fetch", "-q", "--no-tags", "origin", "+refs/heads/ezop:refs/remotes/origin/ezop")
		return w
	}
	bare := &scratch{t: t, dir: bareDir, env: src.env}
	state := func(w *scratch) string {
		return bare.git("for-each-ref") + "\n--\n" + w.git("for-each-ref") + "\n--\n" + w.git("tag", "--list") + "\n--\n" + bare.git("tag", "--list")
	}
	prePush := func(w *scratch, origins []string, tag, c string) (int, string, string) {
		var out, errb bytes.Buffer
		d := guardDeps{
			git: w.adapter(), origins: origins, stdout: &out, stderr: &errb,
			build: func() (*debug.BuildInfo, bool) { return settings("vcs.revision", c, "vcs.modified", "false"), true },
			gh:    &routedGH{record: absentRecordWorld(), pages: pagesOf("[]")},
		}
		code := run([]string{"guard", "--pre-push", tag, c}, d)
		return code, out.String(), errb.String()
	}
	origins := []string{originURL}
	// fetchHead is what the last fetch of the clone left in FETCH_HEAD.
	fetchHead := func(w *scratch) string {
		data, err := os.ReadFile(filepath.Join(w.dir, ".git", "FETCH_HEAD"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	t.Run("the first valid run prints the SHA-bound refspec and changes nothing", func(t *testing.T) {
		w := newClone()
		before := state(w)
		code, out, errb := prePush(w, origins, ppTag, tip)
		if code != 0 || out != tip+":refs/tags/"+ppTag+"\n" || errb != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		if after := state(w); after != before {
			t.Fatalf("a ref or tag changed:\n%s\n-- became --\n%s", before, after)
		}
		if tags := w.git("tag", "--list"); tags != "" {
			t.Fatalf("local tags %q", tags)
		}
	})
	t.Run("an ancestor of the tip passes", func(t *testing.T) {
		w := newClone()
		code, out, errb := prePush(w, origins, ppTag, landed)
		if code != 0 || out != landed+":refs/tags/"+ppTag+"\n" || errb != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("a tag already on origin is refused, even at the commit", func(t *testing.T) {
		src.git("tag", "-a", "-m", "taken", ppTag, tip)
		src.git("push", "-q", "origin", "refs/tags/"+ppTag)
		defer func() { _, _ = (&scratch{t: t, dir: bareDir, env: src.env}).gitErr("tag", "-d", ppTag) }()
		w := newClone()
		before := state(w)
		code, out, errb := prePush(w, origins, ppTag, tip)
		if code != 1 || out != "" || !strings.HasPrefix(errb, "fork-release: tag: ") {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		if after := state(w); after != before {
			t.Fatalf("state changed:\n%s\n-- became --\n%s", before, after)
		}
	})
	t.Run("a commit that is not on origin/ezop is refused", func(t *testing.T) {
		// The object is not even in the clone: the candidate does not resolve.
		w := newClone()
		code, out, errb := prePush(w, origins, ppTag, head)
		if code != 1 || out != "" || !strings.HasPrefix(errb, "fork-release: reachable: ") {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		// The object is in the clone: git answers that it is not an ancestor.
		w.git("fetch", "-q", "--no-tags", "origin", "refs/heads/pr")
		code, out, errb = prePush(w, origins, ppTag, head)
		if code != 1 || out != "" || !strings.HasPrefix(errb, "fork-release: reachable: ") || !strings.Contains(errb, "not reachable") {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("a helper built at another commit is refused before any fetch", func(t *testing.T) {
		w := newClone()
		fetched := fetchHead(w)
		var out, errb bytes.Buffer
		d := guardDeps{
			git: w.adapter(), origins: origins, stdout: &out, stderr: &errb,
			build: func() (*debug.BuildInfo, bool) {
				return settings("vcs.revision", landed, "vcs.modified", "false"), true
			},
			gh: &routedGH{record: absentRecordWorld(), pages: pagesOf("[]")},
		}
		if code := run([]string{"guard", "--pre-push", ppTag, tip}, d); code != 1 || out.Len() != 0 || !strings.HasPrefix(errb.String(), "fork-release: build: ") {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, &out, &errb)
		}
		if fetchHead(w) != fetched {
			t.Fatal("a fetch ran")
		}
	})
	t.Run("a wrong origin is refused before any fetch", func(t *testing.T) {
		w := newClone()
		fetched := fetchHead(w)
		code, out, errb := prePush(w, canonicalOrigins, ppTag, tip)
		if code != 1 || out != "" || !strings.HasPrefix(errb, "fork-release: origin: ") || strings.Contains(errb, bareDir) {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		if fetchHead(w) != fetched {
			t.Fatal("a fetch ran")
		}
	})
	t.Run("the Upstream tag objects are fetched before the ancestry test", func(t *testing.T) {
		// v5.2.0 names a commit that the fetch of ezop does not bring. Without the
		// tag fetch its ancestry would be undecidable and the base check would
		// refuse; with it the answer is "not an ancestor" and v5.1.0 is the base.
		w := newClone()
		if _, err := w.gitErr("cat-file", "-e", head); err == nil {
			t.Fatal("control: the side commit is already in the clone")
		}
		code, out, errb := prePush(w, origins, ppTag, tip)
		if code != 0 || out != tip+":refs/tags/"+ppTag+"\n" || errb != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		if _, err := w.gitErr("cat-file", "-e", head); err != nil {
			t.Fatal("the object of the Upstream tag did not arrive")
		}
	})
}
