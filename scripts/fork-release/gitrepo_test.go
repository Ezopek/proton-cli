package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// scratchEnv is the environment of every scratch git call: the inherited one
// without any GIT_* variable, with the global and system git configuration
// pointed at the null device, prompts off and a fixed identity. HOME is a
// scratch directory too.
func scratchEnv(home string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "HOME=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"HOME="+home,
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Scratch", "GIT_AUTHOR_EMAIL=scratch@example.invalid",
		"GIT_COMMITTER_NAME=Scratch", "GIT_COMMITTER_EMAIL=scratch@example.invalid",
	)
}

// scratch is a scratch git repository with its own environment.
type scratch struct {
	t   *testing.T
	dir string
	env []string
}

// newScratch makes an empty repository on branch main in a fresh directory.
func newScratch(t *testing.T) *scratch {
	t.Helper()
	s := &scratch{t: t, dir: t.TempDir(), env: scratchEnv(t.TempDir())}
	s.git("init", "-q", "-b", "main")
	return s
}

// newBare makes a bare repository, the stand-in for a remote.
func newBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	s := &scratch{t: t, dir: dir, env: scratchEnv(t.TempDir())}
	s.git("init", "-q", "--bare", "-b", "main")
	return dir
}

// git runs git in the repository and returns trimmed stdout. It fails the test
// on a non-zero exit.
func (s *scratch) git(args ...string) string {
	s.t.Helper()
	out, err := s.gitErr(args...)
	if err != nil {
		s.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

// gitErr is git without the failure check; it returns stdout and stderr
// together.
func (s *scratch) gitErr(args ...string) (string, error) {
	s.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = s.dir
	cmd.Env = s.env
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

// write puts a file in the working tree.
func (s *scratch) write(path, content string) {
	s.t.Helper()
	full := filepath.Join(s.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

// commit adds every change and commits it, returning the full SHA.
func (s *scratch) commit(msg string) string {
	s.t.Helper()
	s.git("add", "-A")
	s.git("commit", "-q", "-m", msg)
	return s.git("rev-parse", "HEAD")
}

// adapter returns the git adapter over a scratch repository.
func (s *scratch) adapter() execGit {
	return execGit{dir: s.dir, env: s.env}
}

func TestExecGitOriginURLs(t *testing.T) {
	const canonical = "https://github.com/Ezopek/proton-cli.git"
	t.Run("the effective URLs apply insteadOf and pushInsteadOf", func(t *testing.T) {
		s := newScratch(t)
		s.git("remote", "add", "origin", canonical)
		s.git("config", "url.file:///fetch-side/.insteadOf", "https://github.com/")
		s.git("config", "url.file:///push-side/.pushInsteadOf", "https://github.com/")
		g := s.adapter()
		fetch, err := g.OriginURLs(false)
		if err != nil || !reflect.DeepEqual(fetch, []string{"file:///fetch-side/Ezopek/proton-cli.git"}) {
			t.Fatalf("fetch %v, %v", fetch, err)
		}
		push, err := g.OriginURLs(true)
		if err != nil || !reflect.DeepEqual(push, []string{"file:///push-side/Ezopek/proton-cli.git"}) {
			t.Fatalf("push %v, %v", push, err)
		}
		if raw := s.git("config", "--get", "remote.origin.url"); raw != canonical {
			t.Fatalf("control: the raw configuration reads %q", raw)
		}
	})
	t.Run("several push URLs are all returned", func(t *testing.T) {
		s := newScratch(t)
		s.git("remote", "add", "origin", canonical)
		s.git("config", "--add", "remote.origin.pushurl", canonical)
		s.git("config", "--add", "remote.origin.pushurl", "https://example.invalid/other.git")
		push, err := s.adapter().OriginURLs(true)
		if err != nil || !reflect.DeepEqual(push, []string{canonical, "https://example.invalid/other.git"}) {
			t.Fatalf("push %v, %v", push, err)
		}
		fetch, err := s.adapter().OriginURLs(false)
		if err != nil || !reflect.DeepEqual(fetch, []string{canonical}) {
			t.Fatalf("fetch %v, %v", fetch, err)
		}
	})
	t.Run("a missing origin is an error that prints no URL", func(t *testing.T) {
		s := newScratch(t)
		s.git("remote", "add", "other", canonical)
		for _, push := range []bool{false, true} {
			urls, err := s.adapter().OriginURLs(push)
			if err == nil || len(urls) != 0 || strings.Contains(err.Error(), "github.com") {
				t.Fatalf("push=%v: %v, %v", push, urls, err)
			}
		}
	})
	t.Run("not a repository is an error", func(t *testing.T) {
		g := execGit{dir: t.TempDir(), env: scratchEnv(t.TempDir())}
		if _, err := g.OriginURLs(false); err == nil {
			t.Fatal("no error")
		}
	})
}

// fetchFixture makes a bare origin holding ezop at a commit, and a clone-like
// scratch repository that has origin set to it and no ezop of its own.
func fetchFixture(t *testing.T) (work *scratch, bare, ezopTip string) {
	t.Helper()
	bare = newBare(t)
	src := newScratch(t)
	src.write("a.txt", "a")
	ezopTip = src.commit("one")
	src.git("branch", "-m", "ezop")
	src.git("remote", "add", "origin", bare)
	src.git("push", "-q", "origin", "ezop")
	work = newScratch(t)
	work.git("remote", "add", "origin", bare)
	return work, bare, ezopTip
}

func TestExecGitFetchEzop(t *testing.T) {
	t.Run("fetches ezop and returns its tip", func(t *testing.T) {
		work, _, tip := fetchFixture(t)
		got, err := work.adapter().FetchEzop()
		if err != nil || got != tip {
			t.Fatalf("got %q, %v; want %q", got, err, tip)
		}
		if !fullSHA.MatchString(got) {
			t.Fatalf("not a full SHA: %q", got)
		}
	})
	t.Run("fetches no tags", func(t *testing.T) {
		work, bare, _ := fetchFixture(t)
		other := &scratch{t: t, dir: bare, env: work.env}
		other.git("tag", "stray", "ezop")
		if _, err := work.adapter().FetchEzop(); err != nil {
			t.Fatal(err)
		}
		if tags := work.git("tag", "--list"); tags != "" {
			t.Fatalf("tags were fetched: %q", tags)
		}
	})
	t.Run("returns the tip it just fetched, not an older local ref", func(t *testing.T) {
		work, bare, first := fetchFixture(t)
		if _, err := work.adapter().FetchEzop(); err != nil {
			t.Fatal(err)
		}
		src := newScratch(t)
		src.git("remote", "add", "origin", bare)
		src.git("fetch", "-q", "origin", "ezop")
		src.git("checkout", "-q", "-b", "ezop", "FETCH_HEAD")
		src.write("b.txt", "b")
		second := src.commit("two")
		src.git("push", "-q", "origin", "ezop")
		got, err := work.adapter().FetchEzop()
		if err != nil || got != second || got == first {
			t.Fatalf("got %q, %v; want %q", got, err, second)
		}
	})
	t.Run("a remote that does not exist fails without printing it", func(t *testing.T) {
		work := newScratch(t)
		missing := filepath.Join(t.TempDir(), "no-such-remote.git")
		work.git("remote", "add", "origin", missing)
		got, err := work.adapter().FetchEzop()
		if err == nil || got != "" || strings.Contains(err.Error(), "no-such-remote") {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("a remote without an ezop branch fails", func(t *testing.T) {
		work := newScratch(t)
		work.git("remote", "add", "origin", newBare(t))
		got, err := work.adapter().FetchEzop()
		if err == nil || got != "" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("a stale FETCH_HEAD is not returned after a failed fetch", func(t *testing.T) {
		work, _, _ := fetchFixture(t)
		if _, err := work.adapter().FetchEzop(); err != nil {
			t.Fatal(err)
		}
		work.git("remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
		if got, err := work.adapter().FetchEzop(); err == nil || got != "" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

func TestExecGitIsAncestor(t *testing.T) {
	s := newScratch(t)
	s.write("a", "1")
	a := s.commit("a")
	s.write("a", "2")
	b := s.commit("b")
	s.git("checkout", "-q", "-b", "side", a)
	s.write("c", "3")
	c := s.commit("c")
	g := s.adapter()
	missing := strings.Repeat("ab", 20)
	for _, tc := range []struct {
		name         string
		anc, descend string
		want         int
	}{
		{"an ancestor", a, b, 0},
		{"equal", b, b, 0},
		{"not an ancestor (descendant first)", b, a, 1},
		{"siblings", b, c, 1},
		{"unknown ancestor", missing, b, 128},
		{"unknown descendant", a, missing, 128},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := g.IsAncestor(tc.anc, tc.descend)
			if err != nil || got != tc.want {
				t.Fatalf("status %d, %v; want %d", got, err, tc.want)
			}
		})
	}
	t.Run("a revision that is not a full SHA is refused before git runs", func(t *testing.T) {
		for _, bad := range []string{"--all", "HEAD", "main", "", a[:7]} {
			if st, err := g.IsAncestor(bad, b); err == nil {
				t.Fatalf("%q accepted with status %d", bad, st)
			}
			if st, err := g.IsAncestor(a, bad); err == nil {
				t.Fatalf("%q accepted with status %d", bad, st)
			}
		}
	})
}

func TestExecGitResolveCommit(t *testing.T) {
	s := newScratch(t)
	s.write("f.txt", "1")
	first := s.commit("one")
	s.git("tag", "light", first)
	s.git("tag", "-a", "-m", "annotated", "annotated", first)
	s.write("f.txt", "2")
	second := s.commit("two")
	s.git("branch", "feature", first)
	g := s.adapter()

	ok := map[string]string{
		"main":                    second,
		"HEAD":                    second,
		"feature":                 first,
		"lightweight tag":         first,
		"annotated tag":           first,
		"full SHA":                second,
		"abbreviated SHA":         second,
		"relative":                first,
		"refs/heads/feature":      first,
		"annotated tag, explicit": first,
	}
	revs := map[string]string{
		"main": "main", "HEAD": "HEAD", "feature": "feature", "lightweight tag": "light", "annotated tag": "annotated",
		"full SHA": second, "abbreviated SHA": second[:10], "relative": "HEAD~1", "refs/heads/feature": "refs/heads/feature",
		"annotated tag, explicit": "refs/tags/annotated",
	}
	for name, want := range ok {
		t.Run("resolves "+name, func(t *testing.T) {
			got, err := g.ResolveCommit(revs[name])
			if err != nil || got != want || !fullSHA.MatchString(got) {
				t.Fatalf("got %q, %v; want %q", got, err, want)
			}
		})
	}
	t.Run("resolves a tag of a different checkout state, not the working tree", func(t *testing.T) {
		s.git("checkout", "-q", "feature")
		defer s.git("checkout", "-q", "main")
		if got, err := g.ResolveCommit("main"); err != nil || got != second {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	bad := map[string]string{
		"a blob":           "HEAD:f.txt",
		"a tree":           "HEAD^{tree}",
		"an unknown name":  "no-such-ref",
		"an unknown SHA":   strings.Repeat("ab", 20),
		"a leading dash":   "-x",
		"--all":            "--all",
		"an option with =": "--output=/tmp/x",
		"--end-of-options": "--end-of-options",
		"empty":            "",
		"a space":          " ",
		"a range":          "main..feature",
		"a newline":        "main\nfeature",
	}
	for name, rev := range bad {
		t.Run("rejects "+name, func(t *testing.T) {
			got, err := g.ResolveCommit(rev)
			if err == nil || got != "" {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
	t.Run("rejects a tag that peels to a blob", func(t *testing.T) {
		blob := s.git("rev-parse", "HEAD:f.txt")
		s.git("tag", "-a", "-m", "blob tag", "blobtag", blob)
		if got, err := g.ResolveCommit("blobtag"); err == nil || got != "" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

func TestExecGitTreeAndBlob(t *testing.T) {
	s := newScratch(t)
	s.git("config", "core.filemode", "false")
	s.write(".github/workflows/ci.yml", "name: ci\n")
	s.write(".github/ISSUE_TEMPLATE/a b.yml", "t")
	s.write(".github/ISSUE_TEMPLATE/\u00e9.yml", "u")
	s.write(".goreleaser.yaml", "version: 2\n")
	s.write("README.md", "readme")
	s.write("run.sh", "#!/bin/sh\n")
	// The executable bit is set in the index, so it does not depend on the
	// host file system or on core.filemode.
	s.git("add", "-A")
	s.git("update-index", "--chmod=+x", "run.sh")
	s.git("commit", "-q", "-m", "base")
	base := s.git("rev-parse", "HEAD")
	linkBlob := s.gitStdin("../../etc/passwd", "hash-object", "-w", "--stdin")
	s.git("update-index", "--add", "--cacheinfo", "120000,"+linkBlob+",.github/workflows/link.yml")
	s.git("update-index", "--add", "--cacheinfo", "160000,"+base+",.github/sub")
	s.git("commit", "-q", "-m", "with a symlink and a gitlink")
	tip := s.git("rev-parse", "HEAD")
	g := s.adapter()

	t.Run("lists the paths with their modes, and no others", func(t *testing.T) {
		entries, err := g.Tree(tip, []string{".github"})
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, e := range entries {
			if !fullSHA.MatchString(e.oid) {
				t.Fatalf("entry %v has no full oid", e)
			}
			got[e.path] = e.mode + " " + e.typ
		}
		want := map[string]string{
			".github/workflows/ci.yml":          "100644 blob",
			".github/workflows/link.yml":        "120000 blob",
			".github/sub":                       "160000 commit",
			".github/ISSUE_TEMPLATE/a b.yml":    "100644 blob",
			".github/ISSUE_TEMPLATE/\u00e9.yml": "100644 blob",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v\nwant %v", got, want)
		}
	})
	t.Run("several paths, and a path that does not exist", func(t *testing.T) {
		entries, err := g.Tree(tip, []string{".goreleaser.yaml", "no-such-file"})
		if err != nil || len(entries) != 1 || entries[0].path != ".goreleaser.yaml" {
			t.Fatalf("%v, %v", entries, err)
		}
	})
	t.Run("the whole tree when no path is given, executable bit included", func(t *testing.T) {
		entries, err := g.Tree(tip, nil)
		if err != nil {
			t.Fatal(err)
		}
		var modes []string
		for _, e := range entries {
			if e.path == "run.sh" {
				modes = append(modes, e.mode)
			}
		}
		if !slices.Equal(modes, []string{"100755"}) || len(entries) != 8 {
			t.Fatalf("%d entries, run.sh modes %v", len(entries), modes)
		}
	})
	t.Run("an unknown commit and a non-SHA are errors", func(t *testing.T) {
		if _, err := g.Tree(strings.Repeat("ab", 20), []string{".github"}); err == nil {
			t.Fatal("unknown commit accepted")
		}
		for _, bad := range []string{"HEAD", "--all", "", tip[:8]} {
			if _, err := g.Tree(bad, nil); err == nil {
				t.Fatalf("%q accepted", bad)
			}
		}
	})
	t.Run("reads a blob by oid", func(t *testing.T) {
		entries, err := g.Tree(tip, []string{".github/workflows/ci.yml"})
		if err != nil || len(entries) != 1 {
			t.Fatalf("%v, %v", entries, err)
		}
		data, err := g.Blob(entries[0].oid)
		if err != nil || string(data) != "name: ci\n" {
			t.Fatalf("%q, %v", data, err)
		}
	})
	t.Run("a bad or unknown oid is an error", func(t *testing.T) {
		for _, bad := range []string{strings.Repeat("ab", 20), "HEAD", "--batch", "", linkBlob[:8]} {
			if _, err := g.Blob(bad); err == nil {
				t.Fatalf("%q accepted", bad)
			}
		}
	})
	t.Run("the view is the commit, not the working tree", func(t *testing.T) {
		s.write(".github/workflows/ci.yml", "name: changed in the working tree\n")
		s.write(".github/workflows/evil.yml", "untracked")
		if err := os.Remove(filepath.Join(s.dir, ".goreleaser.yaml")); err != nil {
			t.Fatal(err)
		}
		view, err := commitTreeFS(g, tip, []string{".goreleaser.yaml", ".github"})
		if err != nil {
			t.Fatal(err)
		}
		data, err := fs.ReadFile(view, ".github/workflows/ci.yml")
		if err != nil || string(data) != "name: ci\n" {
			t.Fatalf("%q, %v", data, err)
		}
		if _, err := fs.Stat(view, ".github/workflows/evil.yml"); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("an untracked file is visible: %v", err)
		}
		if data, err := fs.ReadFile(view, ".goreleaser.yaml"); err != nil || string(data) != "version: 2\n" {
			t.Fatalf("%q, %v", data, err)
		}
		if _, err := fs.Stat(view, "README.md"); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("a path outside the profile is visible: %v", err)
		}
	})
	t.Run("a symlink and a gitlink are non-regular in the view and held by the rules", func(t *testing.T) {
		view, err := commitTreeFS(g, tip, []string{".github"})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{".github/workflows/link.yml", ".github/sub"} {
			info, err := fs.Stat(view, p)
			if err != nil || info.Mode().IsRegular() {
				t.Fatalf("%s: %v, %v", p, info, err)
			}
			if _, err := fs.ReadFile(view, p); err == nil {
				t.Fatalf("%s can be read as a file", p)
			}
		}
		got := CheckMirror(view)
		wantHeld(t, got, "github-non-regular", ".github/workflows/link.yml", ".github/sub")
	})
}

// gitStdin runs git with the given standard input and returns trimmed stdout.
func (s *scratch) gitStdin(stdin string, args ...string) string {
	s.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = s.dir
	cmd.Env = s.env
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		s.t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// A replacement ref changes what git reads under a SHA. Every hash-bound read
// of the adapter ignores it, with a control that proves the replacement works.
func TestExecGitIgnoresReplaceRefs(t *testing.T) {
	s := newScratch(t)
	s.write("f.txt", "original")
	s.write(".github/workflows/ci.yml", "name: original\n")
	first := s.commit("first")
	blobA := s.git("rev-parse", "HEAD:f.txt")
	blobB := s.gitStdin("swapped", "hash-object", "-w", "--stdin")
	s.git("checkout", "-q", "--orphan", "other")
	s.git("rm", "-rfq", ".")
	s.write(".github/workflows/ci.yml", "name: replaced\n")
	other := s.commit("unrelated root")
	s.git("checkout", "-q", "main")
	s.write("f.txt", "second")
	second := s.commit("second")
	g := s.adapter()

	t.Run("control: without a replacement the adapter and git agree", func(t *testing.T) {
		data, err := g.Blob(blobA)
		if err != nil || string(data) != "original" {
			t.Fatalf("%q, %v", data, err)
		}
		if st, err := g.IsAncestor(first, second); err != nil || st != 0 {
			t.Fatalf("%d, %v", st, err)
		}
	})

	s.git("replace", blobA, blobB)
	s.git("replace", "--force", second, other)

	t.Run("a replaced blob is read as the original", func(t *testing.T) {
		if got := s.git("cat-file", "blob", blobA); got != "swapped" {
			t.Fatalf("control: plain git reads %q, the replacement is not in effect", got)
		}
		data, err := g.Blob(blobA)
		if err != nil || string(data) != "original" {
			t.Fatalf("%q, %v", data, err)
		}
	})
	t.Run("a replaced commit has its own tree", func(t *testing.T) {
		if got := s.git("ls-tree", "-r", "--name-only", second); got != ".github/workflows/ci.yml" {
			t.Fatalf("control: plain git lists %q", got)
		}
		entries, err := g.Tree(second, nil)
		if err != nil || len(entries) != 2 {
			t.Fatalf("%v, %v", entries, err)
		}
	})
	t.Run("a replaced commit keeps its ancestry", func(t *testing.T) {
		if st := func() int {
			_, err := s.gitErr("merge-base", "--is-ancestor", first, second)
			if err == nil {
				return 0
			}
			return 1
		}(); st != 1 {
			t.Fatalf("control: plain git still sees %d", st)
		}
		if st, err := g.IsAncestor(first, second); err != nil || st != 0 {
			t.Fatalf("%d, %v", st, err)
		}
	})
	t.Run("the view of a replaced commit is built from the original", func(t *testing.T) {
		view, err := commitTreeFS(g, second, []string{".github"})
		if err != nil {
			t.Fatal(err)
		}
		data, err := fs.ReadFile(view, ".github/workflows/ci.yml")
		if err != nil || string(data) != "name: original\n" {
			t.Fatalf("%q, %v", data, err)
		}
	})
}

func TestBuildTreeFS(t *testing.T) {
	oid := func(n string) string { return strings.Repeat(n, 40)[:40] }
	entries := []treeEntry{
		{"100644", "blob", oid("1"), ".github/workflows/ci.yml"},
		{"100755", "blob", oid("2"), ".github/workflows/run.yml"},
		{"120000", "blob", oid("3"), ".github/workflows/link.yml"},
		{"160000", "commit", oid("4"), ".github/mod"},
		{"100644", "blob", oid("5"), ".github/ISSUE_TEMPLATE/x.md"},
		{"100644", "blob", oid("6"), ".goreleaser.yaml"},
		{"100644", "blob", oid("7"), "README.md"},
		{"100644", "blob", oid("8"), ".githubx/lookalike"},
		{"040000", "tree", oid("9"), ".github/odd"},
	}
	var read []string
	reader := func(o string) ([]byte, error) {
		read = append(read, o)
		return []byte("blob " + o[:1]), nil
	}

	t.Run("reads only the blobs of regular files inside the paths", func(t *testing.T) {
		read = nil
		view, err := buildTreeFS(entries, []string{".github"}, reader)
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(read)
		if want := []string{oid("1"), oid("2"), oid("5")}; !slices.Equal(read, want) {
			t.Fatalf("read %v, want %v", read, want)
		}
		if data, err := fs.ReadFile(view, ".github/workflows/ci.yml"); err != nil || string(data) != "blob 1" {
			t.Fatalf("%q, %v", data, err)
		}
		for _, p := range []string{".goreleaser.yaml", "README.md", ".githubx/lookalike"} {
			if _, err := fs.Stat(view, p); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("%s: %v", p, err)
			}
		}
	})
	t.Run("a path may be a file, and every named path is opened", func(t *testing.T) {
		read = nil
		view, err := buildTreeFS(entries, []string{".goreleaser.yaml", ".github/ISSUE_TEMPLATE"}, reader)
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(read)
		if want := []string{oid("5"), oid("6")}; !slices.Equal(read, want) {
			t.Fatalf("read %v, want %v", read, want)
		}
		if _, err := fs.Stat(view, ".github/workflows"); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("outside the paths: %v", err)
		}
	})
	t.Run("no path opens nothing", func(t *testing.T) {
		read = nil
		view, err := buildTreeFS(entries, nil, reader)
		if err != nil || len(read) != 0 {
			t.Fatalf("%v, read %v", err, read)
		}
		if got, err := fs.ReadDir(view, "."); err != nil || len(got) != 0 {
			t.Fatalf("%v, %v", got, err)
		}
	})
	t.Run("the kinds and modes of the entries", func(t *testing.T) {
		view, err := buildTreeFS(entries, []string{".github"}, reader)
		if err != nil {
			t.Fatal(err)
		}
		kinds := map[string]fs.FileMode{}
		err = fs.WalkDir(view, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				kinds[p] = d.Type()
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if kinds[".github/workflows/ci.yml"] != 0 || kinds[".github/workflows/run.yml"] != 0 {
			t.Fatalf("regular files: %v", kinds)
		}
		if kinds[".github/workflows/link.yml"]&fs.ModeSymlink == 0 {
			t.Fatalf("symlink: %v", kinds)
		}
		if kinds[".github/mod"] == 0 || kinds[".github/mod"]&fs.ModeSymlink != 0 {
			t.Fatalf("gitlink: %v", kinds)
		}
		if kinds[".github/odd"] == 0 {
			t.Fatalf("an unknown entry type must be non-regular: %v", kinds)
		}
	})
	t.Run("a blob that cannot be read is an error", func(t *testing.T) {
		_, err := buildTreeFS(entries, []string{".github"}, func(string) ([]byte, error) { return nil, errors.New("boom") })
		if err == nil {
			t.Fatal("no error")
		}
	})
	t.Run("a path that is not a valid file system path is an error", func(t *testing.T) {
		for _, p := range []string{"../x", "/abs", "a//b", ""} {
			_, err := buildTreeFS([]treeEntry{{"100644", "blob", oid("1"), p}}, []string{".github"}, reader)
			if err == nil {
				t.Fatalf("%q accepted", p)
			}
		}
	})
	t.Run("entries are served sorted, with directories synthesized", func(t *testing.T) {
		view, err := buildTreeFS(entries, []string{".github"}, reader)
		if err != nil {
			t.Fatal(err)
		}
		got, err := fs.ReadDir(view, ".github")
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range got {
			names = append(names, e.Name())
		}
		if want := []string{"ISSUE_TEMPLATE", "mod", "odd", "workflows"}; !slices.Equal(names, want) {
			t.Fatalf("%v, want %v", names, want)
		}
	})
}

// The effective URLs are the exact lines git prints: a URL with surrounding
// whitespace is not the canonical remote, on the fetch side and on the push
// side. Each case goes through the adapter and the origin check together.
func TestExecGitOriginURLsAreExact(t *testing.T) {
	const canonical = "https://github.com/Ezopek/proton-cli.git"
	check := func(t *testing.T, s *scratch) error {
		t.Helper()
		g := s.adapter()
		fetch, err := g.OriginURLs(false)
		if err != nil {
			t.Fatal(err)
		}
		push, err := g.OriginURLs(true)
		if err != nil {
			t.Fatal(err)
		}
		return originURLsOK(fetch, push, canonicalOrigins)
	}
	t.Run("control: a canonical fetch and a canonical push URL pass", func(t *testing.T) {
		s := newScratch(t)
		s.git("remote", "add", "origin", canonical)
		s.git("config", "remote.origin.pushurl", canonical)
		if err := check(t, s); err != nil {
			t.Fatal(err)
		}
	})
	for name, padded := range map[string]string{
		"leading space":  " " + canonical,
		"trailing space": canonical + " ",
		"both":           " " + canonical + " ",
		"a trailing tab": canonical + "\t",
	} {
		t.Run("a padded push URL is refused beside a canonical fetch URL: "+name, func(t *testing.T) {
			s := newScratch(t)
			s.git("remote", "add", "origin", canonical)
			s.git("config", "remote.origin.pushurl", padded)
			err := check(t, s)
			wantRefusal(t, err, "origin", "push")
			wantNoText(t, err, "github.com")
		})
		t.Run("a padded fetch URL is refused: "+name, func(t *testing.T) {
			s := newScratch(t)
			s.git("remote", "add", "origin", canonical)
			s.git("config", "remote.origin.url", padded)
			wantRefusal(t, check(t, s), "origin", "fetch")
		})
	}
	t.Run("the adapter returns the padded line unchanged", func(t *testing.T) {
		s := newScratch(t)
		s.git("remote", "add", "origin", canonical)
		s.git("config", "remote.origin.pushurl", " "+canonical+" ")
		push, err := s.adapter().OriginURLs(true)
		if err != nil || !reflect.DeepEqual(push, []string{" " + canonical + " "}) {
			t.Fatalf("%q, %v", push, err)
		}
	})
	t.Run("a carriage return at the end of a URL is kept, so it is refused", func(t *testing.T) {
		s := newScratch(t)
		s.git("remote", "add", "origin", canonical)
		s.git("config", "remote.origin.pushurl", canonical+"\r")
		push, err := s.adapter().OriginURLs(true)
		if err != nil || len(push) != 1 || push[0] != canonical+"\r" {
			t.Fatalf("git did not keep the carriage return: %q, %v", push, err)
		}
		wantRefusal(t, check(t, s), "origin", "push")
	})
}

// An executable file reads as mode 100755 whatever the repository's
// core.filemode says, because the scratch index sets the bit explicitly.
func TestExecGitExecutableModeIsSetInTheIndex(t *testing.T) {
	for _, filemode := range []string{"true", "false"} {
		t.Run("core.filemode="+filemode, func(t *testing.T) {
			s := newScratch(t)
			s.git("config", "core.filemode", filemode)
			s.write("run.sh", "#!/bin/sh\n")
			s.write("plain.txt", "x")
			s.git("add", "-A")
			s.git("update-index", "--chmod=+x", "run.sh")
			s.git("commit", "-q", "-m", "one")
			entries, err := s.adapter().Tree(s.git("rev-parse", "HEAD"), nil)
			if err != nil {
				t.Fatal(err)
			}
			modes := map[string]string{}
			for _, e := range entries {
				modes[e.path] = e.mode
			}
			if !reflect.DeepEqual(modes, map[string]string{"run.sh": "100755", "plain.txt": "100644"}) {
				t.Fatalf("modes %v", modes)
			}
		})
	}
}

// originTagsFixture is a bare origin and a work repository that has it as
// origin, with one commit pushed as ezop.
func originTagsFixture(t *testing.T) (work *scratch, bare, first string) {
	t.Helper()
	bare = newBare(t)
	work = newScratch(t)
	work.write("a.txt", "a")
	first = work.commit("one")
	work.git("branch", "-m", "ezop")
	work.git("remote", "add", "origin", bare)
	work.git("push", "-q", "origin", "ezop")
	return work, bare, first
}

func TestExecGitOriginTags(t *testing.T) {
	t.Run("lists lightweight, annotated and nested annotated tags by their commits", func(t *testing.T) {
		work, _, first := originTagsFixture(t)
		work.write("a.txt", "b")
		second := work.commit("two")
		work.git("tag", "light", first)
		work.git("tag", "-a", "-m", "annotated", "annotated", second)
		work.git("tag", "-a", "-m", "nested", "nested", "annotated")
		work.git("push", "-q", "origin", "--tags")
		// The annotated tags name tag objects, not commits: control.
		if obj := work.git("rev-parse", "annotated"); obj == second {
			t.Fatal("control: an annotated tag object has the commit's id")
		}
		got, err := work.adapter().OriginTags()
		want := map[string]string{"light": first, "annotated": second, "nested": second}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, %v; want %v", got, err, want)
		}
	})
	t.Run("no tags is an empty answer, not an error", func(t *testing.T) {
		work, _, _ := originTagsFixture(t)
		got, err := work.adapter().OriginTags()
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("tags that exist only locally are not listed", func(t *testing.T) {
		work, _, first := originTagsFixture(t)
		work.git("tag", "pushed", first)
		work.git("push", "-q", "origin", "refs/tags/pushed")
		work.git("tag", "local-only", first)
		work.git("tag", "-a", "-m", "local", "local-annotated", first)
		got, err := work.adapter().OriginTags()
		if err != nil || !reflect.DeepEqual(got, map[string]string{"pushed": first}) {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("a tag moved on origin is read as it is on origin", func(t *testing.T) {
		work, bare, first := originTagsFixture(t)
		work.write("a.txt", "b")
		second := work.commit("two")
		work.git("tag", "moving", first)
		work.git("push", "-q", "origin", "refs/tags/moving", "ezop:refs/heads/other")
		remote := &scratch{t: t, dir: bare, env: work.env}
		remote.git("update-ref", "refs/tags/moving", second)
		got, err := work.adapter().OriginTags()
		if err != nil || got["moving"] != second {
			t.Fatalf("got %v, %v; want %s", got, err, second)
		}
	})
	t.Run("writes no local ref", func(t *testing.T) {
		work, _, first := originTagsFixture(t)
		work.git("tag", "pushed", first)
		work.git("push", "-q", "origin", "refs/tags/pushed")
		work.git("tag", "-d", "pushed")
		before := work.git("for-each-ref")
		if _, err := work.adapter().OriginTags(); err != nil {
			t.Fatal(err)
		}
		if after := work.git("for-each-ref"); after != before {
			t.Fatalf("refs changed:\n%s\n--\n%s", before, after)
		}
	})
	t.Run("a remote that does not exist fails without printing it", func(t *testing.T) {
		work := newScratch(t)
		work.git("remote", "add", "origin", filepath.Join(t.TempDir(), "no-such-remote.git"))
		got, err := work.adapter().OriginTags()
		if err == nil || got != nil || strings.Contains(err.Error(), "no-such-remote") {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("no origin at all fails, even with a directory named origin", func(t *testing.T) {
		work := newScratch(t)
		work.git("init", "-q", "--bare", "-b", "main", "origin")
		got, err := work.adapter().OriginTags()
		if err == nil || got != nil {
			t.Fatalf("got %v, %v", got, err)
		}
	})
}

func TestParseOriginTags(t *testing.T) {
	const (
		a = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		b = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	ok := map[string]struct {
		in   string
		want map[string]string
	}{
		"empty":               {"", map[string]string{}},
		"lightweight":         {a + "\trefs/tags/v1\n", map[string]string{"v1": a}},
		"peeled line wins":    {a + "\trefs/tags/v1\n" + b + "\trefs/tags/v1^{}\n", map[string]string{"v1": b}},
		"peeled line first":   {b + "\trefs/tags/v1^{}\n" + a + "\trefs/tags/v1\n", map[string]string{"v1": b}},
		"no trailing newline": {a + "\trefs/tags/v1", map[string]string{"v1": a}},
		"a slash in the name": {a + "\trefs/tags/x/y\n", map[string]string{"x/y": a}},
	}
	for name, tc := range ok {
		t.Run(name, func(t *testing.T) {
			got, err := parseOriginTags([]byte(tc.in))
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	bad := map[string]string{
		"not a full SHA":      "abc\trefs/tags/v1\n",
		"upper case SHA":      strings.ToUpper(a) + "\trefs/tags/v1\n",
		"no tab":              a + " refs/tags/v1\n",
		"not under tags":      a + "\trefs/heads/v1\n",
		"empty name":          a + "\trefs/tags/\n",
		"empty peeled name":   a + "\trefs/tags/^{}\n",
		"a tag listed twice":  a + "\trefs/tags/v1\n" + b + "\trefs/tags/v1\n",
		"a peel listed twice": a + "\trefs/tags/v1\n" + a + "\trefs/tags/v1^{}\n" + b + "\trefs/tags/v1^{}\n",
		"a peel with no tag":  a + "\trefs/tags/v1^{}\n",
		"a blank line":        a + "\trefs/tags/v1\n\n",
		"carriage return":     a + "\trefs/tags/v1\r\n",
	}
	for name, in := range bad {
		t.Run("refuses "+name, func(t *testing.T) {
			if got, err := parseOriginTags([]byte(in)); err == nil || got != nil {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func TestFetchTagsArgs(t *testing.T) {
	t.Run("the argument list is pinned", func(t *testing.T) {
		got, err := fetchTagsArgs([]string{"v1.0.0", "v2.10.3"})
		want := []string{"fetch", "--no-tags", "origin", "refs/tags/v1.0.0", "refs/tags/v2.10.3"}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, %v; want %v", got, err, want)
		}
	})
	t.Run("a single name has no destination", func(t *testing.T) {
		got, err := fetchTagsArgs([]string{"v5.1.0"})
		if err != nil || !reflect.DeepEqual(got, []string{"fetch", "--no-tags", "origin", "refs/tags/v5.1.0"}) {
			t.Fatalf("got %v, %v", got, err)
		}
		for _, a := range got {
			if strings.ContainsAny(a, ":+*") && a != "--no-tags" {
				t.Fatalf("argument %q has a destination, a force or a glob", a)
			}
		}
	})
	t.Run("an empty list is refused", func(t *testing.T) {
		for _, names := range [][]string{nil, {}} {
			if got, err := fetchTagsArgs(names); err == nil || got != nil {
				t.Fatalf("got %v, %v", got, err)
			}
		}
	})
	t.Run("only Upstream release tag names are accepted", func(t *testing.T) {
		for _, bad := range []string{
			"", "v5.1.0-ezop.1", "v5.1", "5.1.0", "v5.1.0-rc.1", "v5.1.0\n", " v5.1.0", "v5.1.0 ", "v*", "*",
			"refs/tags/v5.1.0", "+v5.1.0", "v5.1.0:refs/heads/x", "--force", "-v5.1.0", "v5.1.0^{}", "V5.1.0", "v1.2.3.4",
		} {
			if got, err := fetchTagsArgs([]string{"v1.0.0", bad}); err == nil || got != nil {
				t.Fatalf("%q: got %v, %v", bad, got, err)
			}
		}
	})
}

func TestExecGitFetchTags(t *testing.T) {
	// A bare origin holding ezop (with v1.0.0 lightweight and v1.1.0 annotated on
	// it) and a tag v2.0.0 on a commit that ezop does not contain. The work
	// repository reaches it over file:// so that objects really travel.
	type world struct {
		work                          *scratch
		bareURL                       string
		light, annotated, side, v2Obj string
	}
	setup := func(t *testing.T) world {
		t.Helper()
		bare := newBare(t)
		src := newScratch(t)
		src.write("a.txt", "a")
		light := src.commit("one")
		src.git("branch", "-m", "ezop")
		src.git("tag", "v1.0.0", light)
		src.write("a.txt", "b")
		annotated := src.commit("two")
		src.git("tag", "-a", "-m", "annotated", "v1.1.0", annotated)
		src.git("checkout", "-q", "-b", "side", light)
		src.write("s.txt", "s")
		side := src.commit("side")
		src.git("tag", "-a", "-m", "side", "v2.0.0", side)
		v2Obj := src.git("rev-parse", "v2.0.0")
		src.git("remote", "add", "origin", bare)
		src.git("push", "-q", "origin", "ezop", "refs/tags/v1.0.0", "refs/tags/v1.1.0", "refs/tags/v2.0.0")
		work := newScratch(t)
		url := "file://" + bare
		work.git("remote", "add", "origin", url)
		return world{work, url, light, annotated, side, v2Obj}
	}
	has := func(w world, oid string) bool {
		_, err := w.work.gitErr("cat-file", "-e", oid)
		return err == nil
	}

	t.Run("the objects arrive and only FETCH_HEAD changes", func(t *testing.T) {
		w := setup(t)
		g := w.work.adapter()
		if _, err := g.FetchEzop(); err != nil {
			t.Fatal(err)
		}
		// Control: the commit of a tag that is not in ezop's history is absent,
		// and the ancestry question has no answer.
		if has(w, w.side) || has(w, w.v2Obj) {
			t.Fatal("control: the side commit is already in the clone")
		}
		if status, _ := g.IsAncestor(w.side, w.annotated); status == 0 || status == 1 {
			t.Fatalf("control: ancestry answered %d without the object", status)
		}
		refs := w.work.git("for-each-ref")
		tags := w.work.git("tag", "--list")
		if err := g.FetchTags([]string{"v1.0.0", "v1.1.0", "v2.0.0"}); err != nil {
			t.Fatal(err)
		}
		if !has(w, w.side) || !has(w, w.v2Obj) || !has(w, w.light) {
			t.Fatal("the objects did not arrive")
		}
		if after := w.work.git("for-each-ref"); after != refs {
			t.Fatalf("the ref list changed:\n%s\n--\n%s", refs, after)
		}
		if after := w.work.git("tag", "--list"); after != tags || after != "" {
			t.Fatalf("the tag list changed: %q", after)
		}
		if status, err := g.IsAncestor(w.light, w.annotated); err != nil || status != 0 {
			t.Fatalf("a tag merged into ezop: status %d, %v", status, err)
		}
		if status, err := g.IsAncestor(w.side, w.annotated); err != nil || status != 1 {
			t.Fatalf("a tag not merged into ezop: status %d, %v", status, err)
		}
		if head := w.work.git("rev-parse", "--verify", "FETCH_HEAD"); !fullSHA.MatchString(head) {
			t.Fatalf("FETCH_HEAD %q", head)
		}
	})
	t.Run("a name missing on the remote fails the fetch without printing the remote", func(t *testing.T) {
		w := setup(t)
		err := w.work.adapter().FetchTags([]string{"v1.0.0", "v9.9.9"})
		if err == nil || strings.Contains(err.Error(), w.bareURL) || strings.Contains(err.Error(), "v9.9.9") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a remote that does not exist fails without printing it", func(t *testing.T) {
		work := newScratch(t)
		work.git("remote", "add", "origin", filepath.Join(t.TempDir(), "no-such-remote.git"))
		err := work.adapter().FetchTags([]string{"v1.0.0"})
		if err == nil || strings.Contains(err.Error(), "no-such-remote") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("an invalid list runs no git", func(t *testing.T) {
		w := setup(t)
		before := w.work.git("for-each-ref")
		for _, names := range [][]string{nil, {"*"}, {"v5.1.0-ezop.1"}} {
			if err := w.work.adapter().FetchTags(names); err == nil {
				t.Fatalf("%v accepted", names)
			}
		}
		if _, err := w.work.gitErr("rev-parse", "--verify", "FETCH_HEAD"); err == nil {
			t.Fatal("a fetch ran")
		}
		if w.work.git("for-each-ref") != before {
			t.Fatal("refs changed")
		}
	})
	t.Run("the real adapter offers the capability", func(t *testing.T) {
		var g gitRepo = execGit{}
		if _, ok := g.(tagFetcher); !ok {
			t.Fatal("execGit has no FetchTags")
		}
	})
}
