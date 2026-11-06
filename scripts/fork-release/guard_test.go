package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const (
	secretURL  = "https://secret.invalid/SECRET-URL-MARKER.git"
	helperSHA  = "1111111111111111111111111111111111111111"
	ezopTipSHA = "2222222222222222222222222222222222222222"
	targetSHA  = "3333333333333333333333333333333333333333"
)

// fakeGit is a gitRepo that serves a canned answer for every call and records
// the calls in order.
type fakeGit struct {
	fetchURLs, pushURLs []string
	urlsErr             error
	fetchTip            string
	fetchErr            error
	ancestry            int
	ancestryErr         error
	resolve             map[string]string
	tree                fstest.MapFS
	treeErr             error

	calls []string
	reads []string
	blobs map[string][]byte
	names map[string]string
}

func newFakeGit(tree fstest.MapFS) *fakeGit {
	return &fakeGit{
		fetchURLs: canonicalOrigins[:1],
		pushURLs:  canonicalOrigins[1:2],
		fetchTip:  ezopTipSHA,
		resolve:   map[string]string{"HEAD": targetSHA, "main": targetSHA, targetSHA: targetSHA},
		tree:      tree,
	}
}

func (f *fakeGit) OriginURLs(push bool) ([]string, error) {
	f.calls = append(f.calls, fmt.Sprintf("urls push=%v", push))
	if f.urlsErr != nil {
		return nil, f.urlsErr
	}
	if push {
		return f.pushURLs, nil
	}
	return f.fetchURLs, nil
}

func (f *fakeGit) FetchEzop() (string, error) {
	f.calls = append(f.calls, "fetch")
	return f.fetchTip, f.fetchErr
}

func (f *fakeGit) ResolveCommit(rev string) (string, error) {
	f.calls = append(f.calls, "resolve "+rev)
	if sha, ok := f.resolve[rev]; ok {
		return sha, nil
	}
	return "", errors.New("does not name a commit " + secretURL)
}

func (f *fakeGit) IsAncestor(ancestor, descendant string) (int, error) {
	f.calls = append(f.calls, "ancestor "+ancestor+" "+descendant)
	return f.ancestry, f.ancestryErr
}

func (f *fakeGit) Tree(commit string, paths []string) ([]treeEntry, error) {
	f.calls = append(f.calls, "tree "+commit+" "+strings.Join(paths, ","))
	if f.treeErr != nil {
		return nil, f.treeErr
	}
	f.blobs = map[string][]byte{}
	f.names = map[string]string{}
	var entries []treeEntry
	names := make([]string, 0, len(f.tree))
	for name := range f.tree {
		names = append(names, name)
	}
	slices.Sort(names)
	// The fake returns the whole tree whatever the paths say, so that the
	// view builder is the one that limits what is read.
	for i, name := range names {
		file := f.tree[name]
		oid := fmt.Sprintf("%040x", i+1)
		f.blobs[oid] = file.Data
		f.names[oid] = name
		mode, typ := "100644", "blob"
		switch {
		case file.Mode&fs.ModeSymlink != 0:
			mode = "120000"
		case file.Mode&fs.ModeIrregular != 0:
			mode, typ = "160000", "commit"
		}
		entries = append(entries, treeEntry{mode, typ, oid, name})
	}
	return entries, nil
}

func (f *fakeGit) Blob(oid string) ([]byte, error) {
	f.calls = append(f.calls, "blob")
	data, ok := f.blobs[oid]
	if !ok {
		return nil, errors.New("no such blob")
	}
	f.reads = append(f.reads, f.names[oid])
	return data, nil
}

func (f *fakeGit) OriginTags() (map[string]string, error) {
	f.calls = append(f.calls, "origin tags")
	return nil, errors.New("not served by this fake")
}

func cleanBuild() (*debug.BuildInfo, bool) {
	return settings("vcs.revision", helperSHA, "vcs.modified", "false"), true
}

func testDeps(g *fakeGit) (guardDeps, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return guardDeps{git: g, build: cleanBuild, origins: canonicalOrigins, stdout: &out, stderr: &errb}, &out, &errb
}

func (f *fakeGit) did(prefix string) bool {
	return slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

func TestRunGuardPassAndOrder(t *testing.T) {
	g := newFakeGit(cleanTree(t))
	d, out, errb := testDeps(g)
	if code := runGuard([]string{"--profile", "release", "main"}, d); code != 0 {
		t.Fatalf("exit %d; stdout %q stderr %q", code, out, errb)
	}
	if want := "checked " + targetSHA + "\nrelease: pass\n"; out.String() != want {
		t.Fatalf("stdout %q, want %q", out, want)
	}
	if errb.Len() != 0 {
		t.Fatalf("stderr %q", errb)
	}
	// The check order: origin (fetch URLs, push URLs), then once the fetch,
	// then ancestry of the helper's own commit against that tip, then the
	// commit, then its tree, then the blobs.
	want := []string{
		"urls push=false", "urls push=true", "fetch",
		"ancestor " + helperSHA + " " + ezopTipSHA, "resolve main", "tree " + targetSHA + " .goreleaser.yaml,.github",
	}
	if len(g.calls) < len(want) || !slices.Equal(g.calls[:len(want)], want) {
		t.Fatalf("calls:\n%s\nwant a start of:\n%s", strings.Join(g.calls, "\n"), strings.Join(want, "\n"))
	}
	for _, c := range g.calls[len(want):] {
		if c != "blob" {
			t.Fatalf("unexpected call %q after the tree", c)
		}
	}
	if n := countOf(g.calls, "fetch"); n != 1 {
		t.Fatalf("fetch ran %d times", n)
	}
}

func countOf(calls []string, c string) int {
	n := 0
	for _, x := range calls {
		if x == c {
			n++
		}
	}
	return n
}

func TestRunGuardProfiles(t *testing.T) {
	t.Run("mirror passes the raw tree, and never reads .goreleaser.yaml", func(t *testing.T) {
		tree := rawTree(t)
		tree[".goreleaser.yaml"] = &fstest.MapFile{Data: []byte("aurs: [\n")}
		g := newFakeGit(tree)
		d, out, errb := testDeps(g)
		if code := runGuard([]string{"--profile", "mirror", "HEAD"}, d); code != 0 {
			t.Fatalf("exit %d; stdout %q stderr %q", code, out, errb)
		}
		if want := "checked " + targetSHA + "\nmirror: pass\n"; out.String() != want {
			t.Fatalf("stdout %q", out)
		}
		if slices.Contains(g.reads, ".goreleaser.yaml") {
			t.Fatalf("blobs read: %v", g.reads)
		}
		if !g.did("tree "+targetSHA+" .github") || slices.ContainsFunc(g.calls, func(c string) bool { return strings.Contains(c, "goreleaser") }) {
			t.Fatalf("calls: %v", g.calls)
		}
	})
	t.Run("release holds the raw tree, naming the four workflows, and prints the SHA", func(t *testing.T) {
		tree := rawTree(t)
		tree[".goreleaser.yaml"] = &fstest.MapFile{Data: fixture(t, "goreleaser.cleaned.yaml")}
		g := newFakeGit(tree)
		d, out, errb := testDeps(g)
		if code := runGuard([]string{"--profile", "release", "main"}, d); code != 1 {
			t.Fatalf("exit %d; stdout %q", code, out)
		}
		if !strings.HasPrefix(out.String(), "checked "+targetSHA+"\nrelease: held, 4 item(s)\n") {
			t.Fatalf("stdout %q", out)
		}
		for _, name := range heldDisabledNames {
			if !strings.Contains(out.String(), "workflows/"+name+": workflow-set") {
				t.Fatalf("%s is not named:\n%s", name, out)
			}
		}
		if errb.Len() != 0 {
			t.Fatalf("stderr %q", errb)
		}
	})
	t.Run("mirror holds and names each item", func(t *testing.T) {
		tree := rawTree(t)
		tree[".github/workflows/new.yml"] = &fstest.MapFile{Data: []byte("x: 1\n")}
		tree[".github/dependabot.yml"] = &fstest.MapFile{Data: []byte("version: 2\n")}
		g := newFakeGit(tree)
		d, out, _ := testDeps(g)
		if code := runGuard([]string{"--profile", "mirror", "main"}, d); code != 1 {
			t.Fatalf("exit %d", code)
		}
		want := "checked " + targetSHA + "\nmirror: held, 2 item(s)\n"
		if !strings.HasPrefix(out.String(), want) || !strings.Contains(out.String(), "new.yml") || !strings.Contains(out.String(), "dependabot.yml") {
			t.Fatalf("stdout %q", out)
		}
	})
	t.Run("a symlink and a gitlink entry are held", func(t *testing.T) {
		tree := rawTree(t)
		tree[".github/workflows/web.yml"] = &fstest.MapFile{Data: []byte("../x"), Mode: fs.ModeSymlink}
		tree[".github/sub"] = &fstest.MapFile{Mode: fs.ModeIrregular}
		g := newFakeGit(tree)
		d, out, _ := testDeps(g)
		if code := runGuard([]string{"--profile", "mirror", "main"}, d); code != 1 {
			t.Fatalf("exit %d", code)
		}
		if !strings.Contains(out.String(), "web.yml: github-non-regular") || !strings.Contains(out.String(), ".github/sub: github-non-regular") {
			t.Fatalf("stdout %q", out)
		}
	})
	t.Run("a .github that is a symlink is held", func(t *testing.T) {
		g := newFakeGit(fstest.MapFS{".github": {Data: []byte("elsewhere"), Mode: fs.ModeSymlink}})
		d, out, _ := testDeps(g)
		if code := runGuard([]string{"--profile", "mirror", "main"}, d); code != 1 || !strings.Contains(out.String(), ".github: github-file") {
			t.Fatalf("exit %d; stdout %q", code, out)
		}
	})
	t.Run("a .goreleaser.yaml that is a symlink is held by release", func(t *testing.T) {
		tree := cleanTree(t)
		tree[".goreleaser.yaml"] = &fstest.MapFile{Data: []byte("elsewhere"), Mode: fs.ModeSymlink}
		g := newFakeGit(tree)
		d, out, _ := testDeps(g)
		if code := runGuard([]string{"--profile", "release", "main"}, d); code != 1 || !strings.Contains(out.String(), "goreleaser-unreadable") {
			t.Fatalf("exit %d; stdout %q", code, out)
		}
	})
}

// refused runs the guard and checks it ended in a one-line refusal of the
// check, with exit 1, no stdout (so no SHA) and no URL anywhere.
func refused(t *testing.T, g *fakeGit, d guardDeps, out, errb *bytes.Buffer, args []string, check string) {
	t.Helper()
	if code := runGuard(args, d); code != 1 {
		t.Fatalf("exit %d; stdout %q stderr %q", code, out, errb)
	}
	line := errb.String()
	if !strings.HasPrefix(line, "fork-release: "+check+": ") || strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") {
		t.Fatalf("stderr %q, want a one-line %s refusal", line, check)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout %q beside a refusal", out)
	}
	for _, s := range []string{"SECRET-URL-MARKER", "github.com"} {
		if strings.Contains(line, s) || strings.Contains(out.String(), s) {
			t.Fatalf("output carries %q: %q", s, line)
		}
	}
}

var releaseMain = []string{"--profile", "release", "main"}

func TestRunGuardOriginRefusals(t *testing.T) {
	cases := map[string]func(g *fakeGit){
		"fetch URL wrong":   func(g *fakeGit) { g.fetchURLs = []string{secretURL} },
		"push URL wrong":    func(g *fakeGit) { g.pushURLs = []string{secretURL} },
		"one bad of many":   func(g *fakeGit) { g.pushURLs = []string{canonicalOrigins[0], secretURL} },
		"origin missing":    func(g *fakeGit) { g.urlsErr = errors.New("No such remote " + secretURL) },
		"no fetch URL":      func(g *fakeGit) { g.fetchURLs = nil },
		"no push URL":       func(g *fakeGit) { g.pushURLs = nil },
		"upstream fetch":    func(g *fakeGit) { g.fetchURLs = []string{"https://github.com/roman-16/proton-cli.git"} },
		"upstream push scp": func(g *fakeGit) { g.pushURLs = []string{"git@github.com:roman-16/proton-cli.git"} },
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			g := newFakeGit(cleanTree(t))
			set(g)
			d, out, errb := testDeps(g)
			refused(t, g, d, out, errb, releaseMain, "origin")
			if g.did("fetch") || g.did("ancestor") || g.did("resolve") || g.did("tree") {
				t.Fatalf("work was done after a wrong origin: %v", g.calls)
			}
		})
	}
	t.Run("a different allowed list changes the verdict", func(t *testing.T) {
		g := newFakeGit(cleanTree(t))
		g.fetchURLs, g.pushURLs = []string{"/scratch/o.git"}, []string{"/scratch/o.git"}
		d, out, errb := testDeps(g)
		d.origins = []string{"/scratch/o.git"}
		if code := runGuard(releaseMain, d); code != 0 {
			t.Fatalf("exit %d; %q %q", code, out, errb)
		}
	})
}

func TestRunGuardBuildRefusals(t *testing.T) {
	infos := map[string]func() (*debug.BuildInfo, bool){
		"no build info": func() (*debug.BuildInfo, bool) { return nil, false },
		"no revision":   func() (*debug.BuildInfo, bool) { return settings("vcs.modified", "false"), true },
		"no modified":   func() (*debug.BuildInfo, bool) { return settings("vcs.revision", helperSHA), true },
		"bad revision":  func() (*debug.BuildInfo, bool) { return settings("vcs.revision", "abc", "vcs.modified", "false"), true },
		"modified": func() (*debug.BuildInfo, bool) {
			return settings("vcs.revision", helperSHA, "vcs.modified", "true"), true
		},
		"ok=false, info": func() (*debug.BuildInfo, bool) {
			return settings("vcs.revision", helperSHA, "vcs.modified", "false"), false
		},
	}
	for name, build := range infos {
		t.Run(name, func(t *testing.T) {
			g := newFakeGit(cleanTree(t))
			d, out, errb := testDeps(g)
			d.build = build
			refused(t, g, d, out, errb, releaseMain, "build")
			if g.did("fetch") || g.did("ancestor") {
				t.Fatalf("work was done before the build identity: %v", g.calls)
			}
		})
	}
}

func TestRunGuardAncestry(t *testing.T) {
	t.Run("status 1 is not landed", func(t *testing.T) {
		g := newFakeGit(cleanTree(t))
		g.ancestry = 1
		d, out, errb := testDeps(g)
		refused(t, g, d, out, errb, releaseMain, "ancestry")
		if !strings.Contains(errb.String(), "not landed") {
			t.Fatalf("stderr %q", errb)
		}
		if g.did("resolve") || g.did("tree") {
			t.Fatalf("calls after a refusal: %v", g.calls)
		}
	})
	for _, status := range []int{2, 128, 255} {
		t.Run(fmt.Sprintf("status %d is undecidable, told apart from not landed", status), func(t *testing.T) {
			g := newFakeGit(cleanTree(t))
			g.ancestry = status
			d, out, errb := testDeps(g)
			refused(t, g, d, out, errb, releaseMain, "ancestry")
			if strings.Contains(errb.String(), "not landed") || !strings.Contains(errb.String(), "cannot decide") {
				t.Fatalf("stderr %q", errb)
			}
			if g.did("resolve") || g.did("tree") {
				t.Fatalf("calls after a refusal: %v", g.calls)
			}
		})
	}
	t.Run("git that cannot run is undecidable", func(t *testing.T) {
		g := newFakeGit(cleanTree(t))
		g.ancestryErr = errors.New("killed")
		d, out, errb := testDeps(g)
		refused(t, g, d, out, errb, releaseMain, "ancestry")
		if strings.Contains(errb.String(), "not landed") {
			t.Fatalf("stderr %q", errb)
		}
	})
	t.Run("a failed fetch is refused as a fetch", func(t *testing.T) {
		g := newFakeGit(cleanTree(t))
		g.fetchErr = errors.New("fatal: " + secretURL)
		d, out, errb := testDeps(g)
		refused(t, g, d, out, errb, releaseMain, "fetch")
		if g.did("ancestor") {
			t.Fatalf("ancestry was asked after a failed fetch: %v", g.calls)
		}
	})
	t.Run("a fetch that returns no full SHA is refused as a fetch", func(t *testing.T) {
		g := newFakeGit(cleanTree(t))
		g.fetchTip = "not-a-sha"
		d, out, errb := testDeps(g)
		refused(t, g, d, out, errb, releaseMain, "fetch")
	})
}

func TestRunGuardCommitRefusals(t *testing.T) {
	for _, rev := range []string{"no-such-ref", "-x", "--all", "--output=/tmp/x", ""} {
		t.Run(fmt.Sprintf("%q", rev), func(t *testing.T) {
			g := newFakeGit(cleanTree(t))
			d, out, errb := testDeps(g)
			refused(t, g, d, out, errb, []string{"--profile", "release", rev}, "commit")
			if strings.HasPrefix(rev, "-") && g.did("resolve") {
				t.Fatalf("an option-like revision reached git: %v", g.calls)
			}
			if g.did("tree") {
				t.Fatalf("a tree was read: %v", g.calls)
			}
		})
	}
	t.Run("a commit that resolves to something that is not a full SHA", func(t *testing.T) {
		g := newFakeGit(cleanTree(t))
		g.resolve["odd"] = "abc"
		d, out, errb := testDeps(g)
		refused(t, g, d, out, errb, []string{"--profile", "release", "odd"}, "commit")
	})
}

func TestRunGuardTreeRefusal(t *testing.T) {
	g := newFakeGit(cleanTree(t))
	g.treeErr = errors.New("boom " + secretURL)
	d, out, errb := testDeps(g)
	refused(t, g, d, out, errb, releaseMain, "tree")
}

func TestRunGuardBlobRefusal(t *testing.T) {
	g := newFakeGit(cleanTree(t))
	d, out, errb := testDeps(g)
	d.git = blobFails{g}
	refused(t, g, d, out, errb, releaseMain, "tree")
}

type blobFails struct{ *fakeGit }

func (blobFails) Blob(string) ([]byte, error) { return nil, errors.New("boom " + secretURL) }

func TestRunGuardUsage(t *testing.T) {
	cases := map[string][]string{
		"no arguments":       nil,
		"profile only":       {"--profile"},
		"profile, no value":  {"--profile", "release"},
		"unknown profile":    {"--profile", "other", "main"},
		"empty profile":      {"--profile", "", "main"},
		"no flag":            {"release", "main"},
		"extra argument":     {"--profile", "release", "main", "extra"},
		"profile twice":      {"--profile", "release", "--profile", "mirror", "main"},
		"equals form":        {"--profile=release", "main"},
		"commit before flag": {"main", "--profile", "release"},
		"pre-push mode":      {"--pre-push", "v5.1.0-ezop.1", "abc"},
		"in-run mode":        {"in-run"},
		"upper-case profile": {"--profile", "Release", "main"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			g := newFakeGit(cleanTree(t))
			d, out, errb := testDeps(g)
			if code := runGuard(args, d); code != 2 {
				t.Fatalf("exit %d", code)
			}
			if errb.String() != guardUsage+"\n" || out.Len() != 0 {
				t.Fatalf("stderr %q stdout %q", errb, out)
			}
			if len(g.calls) != 0 {
				t.Fatalf("git was called: %v", g.calls)
			}
		})
	}
	if !strings.HasPrefix(guardUsage, "fork-release: usage: ") || strings.Contains(guardUsage, "\n") {
		t.Fatalf("usage %q", guardUsage)
	}
}

func TestRunDispatch(t *testing.T) {
	for name, args := range map[string][]string{
		"no command":      nil,
		"unknown command": {"notes"},
		"stamp":           {"stamp"},
		"verify":          {"verify", "--status"},
		"assets":          {"assets"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newFakeGit(cleanTree(t))
			d, out, errb := testDeps(g)
			if code := run(args, d); code != 2 {
				t.Fatalf("exit %d", code)
			}
			if out.Len() != 0 || !strings.HasPrefix(errb.String(), "fork-release: ") || strings.Count(errb.String(), "\n") != 1 || len(g.calls) != 0 {
				t.Fatalf("stdout %q stderr %q calls %v", out, errb, g.calls)
			}
		})
	}
	t.Run("guard is dispatched", func(t *testing.T) {
		g := newFakeGit(cleanTree(t))
		d, out, _ := testDeps(g)
		if code := run(append([]string{"guard"}, releaseMain...), d); code != 0 || !strings.Contains(out.String(), "release: pass") {
			t.Fatalf("exit %d, stdout %q", code, out)
		}
	})
	t.Run("guard modes other than --profile are usage", func(t *testing.T) {
		d, _, errb := testDeps(newFakeGit(cleanTree(t)))
		if code := run([]string{"guard", "--pre-push", "T", "C"}, d); code != 2 || errb.String() != guardUsage+"\n" {
			t.Fatalf("exit %d, stderr %q", code, errb)
		}
	})
}

func TestProductionDeps(t *testing.T) {
	d := productionDeps()
	if !slices.Equal(d.origins, canonicalOrigins) {
		t.Fatalf("production origins %v", d.origins)
	}
	if _, ok := d.git.(execGit); !ok {
		t.Fatalf("production git is %T", d.git)
	}
	if d.build == nil || d.stdout == nil || d.stderr == nil {
		t.Fatal("incomplete production dependencies")
	}
}

// End to end: a scratch working repository, a scratch bare repository as its
// origin, the real git adapter, and a helper build identity injected as plain
// settings. The origin list is injected too, since no scratch path is the
// real remote; the production list is unchanged and checked above.
func TestRunGuardEndToEnd(t *testing.T) {
	bare := newBare(t)
	w := newScratch(t)
	w.git("remote", "add", "origin", bare)
	w.git("checkout", "-q", "-b", "ezop")
	w.write(".goreleaser.yaml", string(fixture(t, "goreleaser.cleaned.yaml")))
	w.write(".github/workflows/ci.yml", string(fixture(t, "ci.yml")))
	w.write(".github/ISSUE_TEMPLATE/a.md", "x")
	landed := w.commit("landed")
	w.git("push", "-q", "origin", "ezop")

	w.git("tag", "-a", "-m", "tag", "release-tag", landed)
	w.git("checkout", "-q", "-b", "held-branch")
	w.write(".github/workflows/new.yml", "x: 1\n")
	held := w.commit("adds a workflow")
	w.git("checkout", "-q", "ezop")

	deps := func(helper string, origins ...string) (guardDeps, *bytes.Buffer, *bytes.Buffer) {
		var out, errb bytes.Buffer
		return guardDeps{
			git: w.adapter(),
			build: func() (*debug.BuildInfo, bool) {
				return settings("vcs.revision", helper, "vcs.modified", "false"), true
			},
			origins: origins,
			stdout:  &out,
			stderr:  &errb,
		}, &out, &errb
	}
	pass := func(t *testing.T, profile, rev, want string) {
		t.Helper()
		d, out, errb := deps(landed, bare)
		if code := runGuard([]string{"--profile", profile, rev}, d); code != 0 || out.String() != "checked "+want+"\n"+profile+": pass\n" || errb.Len() != 0 {
			t.Fatalf("exit %d; stdout %q stderr %q", code, out, errb)
		}
	}

	t.Run("release passes on the landed commit, by SHA, branch, tag and HEAD", func(t *testing.T) {
		pass(t, "release", landed, landed)
		pass(t, "release", "ezop", landed)
		pass(t, "release", "release-tag", landed)
		pass(t, "release", "HEAD", landed)
	})
	t.Run("mirror passes on a tree with only ci.yml", func(t *testing.T) {
		pass(t, "mirror", landed, landed)
	})
	t.Run("the tree is the commit's, not the working tree's", func(t *testing.T) {
		w.write(".github/workflows/evil.yml", "x: 1\n")
		w.write(".github/dependabot.yml", "version: 2\n")
		w.write(".goreleaser.yaml", "aurs: []\n")
		defer func() {
			w.git("clean", "-fdq")
			w.git("checkout", "-q", "--", ".")
		}()
		pass(t, "release", landed, landed)
		pass(t, "mirror", "HEAD", landed)
	})
	t.Run("a held tree prints the SHA and the items, and exits 1", func(t *testing.T) {
		d, out, errb := deps(landed, bare)
		if code := runGuard([]string{"--profile", "release", held}, d); code != 1 {
			t.Fatalf("exit %d; stderr %q", code, errb)
		}
		if !strings.HasPrefix(out.String(), "checked "+held+"\nrelease: held, 1 item(s)\n") || !strings.Contains(out.String(), "new.yml: workflow-set") {
			t.Fatalf("stdout %q", out)
		}
	})
	t.Run("a helper that is a descendant of origin/ezop is not landed", func(t *testing.T) {
		d, out, errb := deps(held, bare)
		if code := runGuard([]string{"--profile", "release", landed}, d); code != 1 || out.Len() != 0 {
			t.Fatalf("exit %d; stdout %q", code, out)
		}
		if !strings.HasPrefix(errb.String(), "fork-release: ancestry: ") || !strings.Contains(errb.String(), "not landed") {
			t.Fatalf("stderr %q", errb)
		}
	})
	t.Run("a helper commit git does not know is undecidable", func(t *testing.T) {
		d, out, errb := deps(strings.Repeat("ab", 20), bare)
		if code := runGuard([]string{"--profile", "release", landed}, d); code != 1 || out.Len() != 0 {
			t.Fatalf("exit %d; stdout %q", code, out)
		}
		if !strings.HasPrefix(errb.String(), "fork-release: ancestry: ") || strings.Contains(errb.String(), "not landed") {
			t.Fatalf("stderr %q", errb)
		}
	})
	t.Run("a helper that is an ancestor of origin/ezop is landed", func(t *testing.T) {
		root := w.git("rev-list", "--max-parents=0", "ezop")
		if root != landed {
			t.Fatalf("setup: %s %s", root, landed)
		}
		w.git("checkout", "-q", "ezop")
		w.write("later.txt", "x")
		later := w.commit("later")
		w.git("push", "-q", "origin", "ezop")
		d, out, errb := deps(landed, bare)
		if code := runGuard([]string{"--profile", "release", later}, d); code != 0 {
			t.Fatalf("exit %d; stdout %q stderr %q", code, out, errb)
		}
	})
	t.Run("a wrong origin is refused before any fetch", func(t *testing.T) {
		fresh := newScratch(t)
		fresh.git("remote", "add", "origin", bare)
		out, errb := &bytes.Buffer{}, &bytes.Buffer{}
		d := guardDeps{git: fresh.adapter(), build: cleanBuild, origins: canonicalOrigins, stdout: out, stderr: errb}
		if code := runGuard(releaseMain, d); code != 1 || out.Len() != 0 {
			t.Fatalf("exit %d; stdout %q", code, out)
		}
		if !strings.HasPrefix(errb.String(), "fork-release: origin: ") || strings.Contains(errb.String(), bare) {
			t.Fatalf("stderr %q", errb)
		}
		if _, err := fresh.gitErr("rev-parse", "--verify", "FETCH_HEAD"); err == nil {
			t.Fatal("a fetch ran")
		}
		if _, err := fresh.gitErr("rev-parse", "--verify", "refs/remotes/origin/ezop"); err == nil {
			t.Fatal("origin/ezop was fetched")
		}
	})
	t.Run("a failed fetch is refused as a fetch", func(t *testing.T) {
		fresh := newScratch(t)
		gone := filepath.Join(t.TempDir(), "gone.git")
		fresh.git("remote", "add", "origin", gone)
		out, errb := &bytes.Buffer{}, &bytes.Buffer{}
		d := guardDeps{git: fresh.adapter(), build: cleanBuild, origins: []string{gone}, stdout: out, stderr: errb}
		if code := runGuard(releaseMain, d); code != 1 || out.Len() != 0 {
			t.Fatalf("exit %d; stdout %q", code, out)
		}
		if !strings.HasPrefix(errb.String(), "fork-release: fetch: ") || strings.Contains(errb.String(), "gone.git") {
			t.Fatalf("stderr %q", errb)
		}
	})
	t.Run("an unknown revision and an option-like one are refused as a commit", func(t *testing.T) {
		for _, rev := range []string{"no-such-ref", "-x", "--all", "HEAD:.goreleaser.yaml"} {
			d, out, errb := deps(landed, bare)
			if code := runGuard([]string{"--profile", "release", rev}, d); code != 1 || out.Len() != 0 {
				t.Fatalf("%s: exit %d; stdout %q", rev, code, out)
			}
			if !strings.HasPrefix(errb.String(), "fork-release: commit: ") {
				t.Fatalf("%s: stderr %q", rev, errb)
			}
		}
	})
}

// buildHelperBinary builds the candidate helper with the documented recipe in a
// fresh scratch clone, from copies of its own non-test sources and the module
// files, and returns the binary. The binary is built outside the clone.
func buildHelperBinary(t *testing.T) string {
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
	s.commit("candidate")
	out := filepath.Join(t.TempDir(), "fork-release")
	cmd := exec.Command("go", buildRecipeArgs(out)...)
	cmd.Dir = s.dir
	cmd.Env = append(s.env, "HOME="+os.Getenv("HOME"), "GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS=-mod=readonly", "GOWORK=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %v: %v\n%s", buildRecipeArgs(out), err, b)
	}
	return out
}

// runBinary runs the built helper in dir with a scratch environment and returns
// its standard output, standard error and exit code.
func runBinary(t *testing.T, bin, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = scratchEnv(t.TempDir())
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("run %v: %v", args, err)
	}
	return out.String(), errb.String(), code
}

// The real binary, built by the documented recipe, goes through main, the
// argument dispatch, the production dependencies and the default git adapter
// (which works in the current directory). No origin is contacted: every case
// ends at the command line or at the origin refusal of a scratch clone.
func TestEntrypointBinary(t *testing.T) {
	bin := buildHelperBinary(t)

	t.Run("no command is usage, exit 2", func(t *testing.T) {
		out, errb, code := runBinary(t, bin, t.TempDir())
		if code != 2 || out != "" || errb != "fork-release: no command\n" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("an unknown command is usage, exit 2", func(t *testing.T) {
		out, errb, code := runBinary(t, bin, t.TempDir(), "bogus")
		if code != 2 || out != "" || errb != "fork-release: unknown command\n" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("a bad flag is usage, exit 2", func(t *testing.T) {
		out, errb, code := runBinary(t, bin, t.TempDir(), "guard", "--bogus")
		if code != 2 || out != "" || errb != guardUsage+"\n" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("a clone without origin is refused as origin, exit 1", func(t *testing.T) {
		clone := newScratch(t)
		clone.write("a.txt", "a")
		clone.commit("one")
		out, errb, code := runBinary(t, bin, clone.dir, "guard", "--profile", "mirror", "HEAD")
		if code != 1 || out != "" || errb != "fork-release: origin: origin is not configured or cannot be read\n" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("the default adapter reads the clone in the current directory", func(t *testing.T) {
		const odd = "https://secret.invalid/not-the-fork.git"
		clone := newScratch(t)
		clone.write("a.txt", "a")
		clone.commit("one")
		clone.git("remote", "add", "origin", odd)
		out, errb, code := runBinary(t, bin, clone.dir, "guard", "--profile", "release", "HEAD")
		want := "fork-release: origin: a fetch URL of origin is not the canonical Ezopek/proton-cli remote\n"
		if code != 1 || out != "" || errb != want {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		if strings.Contains(errb, "secret.invalid") {
			t.Fatalf("the refusal prints the URL: %q", errb)
		}
	})
	t.Run("a directory that is not a repository is refused as origin, exit 1", func(t *testing.T) {
		out, errb, code := runBinary(t, bin, t.TempDir(), "guard", "--profile", "release", "HEAD")
		if code != 1 || out != "" || errb != "fork-release: origin: origin is not configured or cannot be read\n" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
}

// errWriter is an output stream that always fails.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

// A verdict that cannot be delivered is not a pass: the command refuses with a
// one-line message on standard error and exits 1, whether the tree passed or
// was held. A working stream is the control.
func TestRunGuardOutputFailure(t *testing.T) {
	t.Run("control: a working stream delivers the SHA and the verdict", func(t *testing.T) {
		d, out, errb := testDeps(newFakeGit(cleanTree(t)))
		if code := runGuard([]string{"--profile", "release", "main"}, d); code != 0 {
			t.Fatalf("exit %d; stderr %q", code, errb)
		}
		if want := "checked " + targetSHA + "\nrelease: pass\n"; out.String() != want {
			t.Fatalf("stdout %q, want %q", out, want)
		}
	})
	t.Run("a passing tree whose verdict cannot be written exits 1", func(t *testing.T) {
		d, _, errb := testDeps(newFakeGit(cleanTree(t)))
		d.stdout = errWriter{}
		if code := runGuard([]string{"--profile", "release", "main"}, d); code != 1 {
			t.Fatalf("exit %d, want 1; stderr %q", code, errb)
		}
		if want := "fork-release: output: could not write the result\n"; errb.String() != want {
			t.Fatalf("stderr %q, want %q", errb, want)
		}
	})
	t.Run("a held tree whose verdict cannot be written exits 1 and says so", func(t *testing.T) {
		tree := cleanTree(t)
		tree[".github/workflows/extra.yml"] = &fstest.MapFile{Data: []byte("on: push\n")}
		d, _, errb := testDeps(newFakeGit(tree))
		d.stdout = errWriter{}
		if code := runGuard([]string{"--profile", "release", "main"}, d); code != 1 {
			t.Fatalf("exit %d, want 1; stderr %q", code, errb)
		}
		if want := "fork-release: output: could not write the result\n"; errb.String() != want {
			t.Fatalf("stderr %q, want %q", errb, want)
		}
	})
}
