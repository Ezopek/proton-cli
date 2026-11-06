package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckTagPattern(t *testing.T) {
	for _, tag := range []string{"v5.1.0-ezop.1", "v10.20.30-ezop.12", "v0.0.0-ezop.9", "v5.1.0-ezop.10"} {
		t.Run("accepts "+tag, func(t *testing.T) {
			if err := checkTagPattern(tag); err != nil {
				t.Fatal(err)
			}
		})
	}
	refused := map[string]string{
		"counter zero":           "v5.1.0-ezop.0",
		"trailing letter":        "v5.1.0-ezop.1x",
		"leading zero counter":   "v5.1.0-ezop.01",
		"no v":                   "5.1.0-ezop.1",
		"two components":         "v5.1-ezop.1",
		"four components":        "v5.1.0.1-ezop.1",
		"empty":                  "",
		"leading space":          " v5.1.0-ezop.1",
		"trailing space":         "v5.1.0-ezop.1 ",
		"trailing newline":       "v5.1.0-ezop.1\n",
		"leading newline":        "\nv5.1.0-ezop.1",
		"embedded newline":       "v5.1.0-ezop.1\nv5.1.0-ezop.2",
		"upstream release":       "v5.1.0",
		"upstream pre-release":   "v5.1.0-rc.1",
		"other suffix":           "v5.1.0-fork.1",
		"upper case v":           "V5.1.0-ezop.1",
		"upper case suffix":      "v5.1.0-EZOP.1",
		"no counter":             "v5.1.0-ezop.",
		"no dot":                 "v5.1.0-ezop1",
		"negative counter":       "v5.1.0-ezop.-1",
		"prefix of a longer tag": "refs/tags/v5.1.0-ezop.1",
		"non ASCII digit":        "v5.1.0-ezop.١",
	}
	for name, tag := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			wantRefusal(t, checkTagPattern(tag), "tag")
		})
	}
}

func TestCheckIdentity(t *testing.T) {
	const (
		e     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		other = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	t.Run("accepts three equal commits and returns E", func(t *testing.T) {
		got, err := checkIdentity(e, e, e)
		if err != nil || got != e {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	cases := []struct {
		name                string
		head, github, onTag string
		fragment            string
	}{
		{"HEAD missing", "", e, e, "HEAD"},
		{"GITHUB_SHA missing", e, "", e, "GITHUB_SHA"},
		{"tag missing on origin", e, e, "", "origin"},
		{"all missing", "", "", "", "HEAD"},
		{"GITHUB_SHA differs", e, other, e, "GITHUB_SHA"},
		{"tag on origin differs (moved tag)", e, e, other, "origin"},
		{"HEAD differs", other, e, e, "HEAD"},
		{"short SHA", e[:10], e[:10], e[:10], "HEAD"},
		{"upper case SHA", strings.ToUpper(e), strings.ToUpper(e), strings.ToUpper(e), "HEAD"},
		{"padded SHA", e + "\n", e + "\n", e + "\n", "HEAD"},
		{"not hex", strings.Repeat("g", 40), strings.Repeat("g", 40), strings.Repeat("g", 40), "HEAD"},
	}
	for _, tc := range cases {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			got, err := checkIdentity(tc.head, tc.github, tc.onTag)
			if got != "" {
				t.Fatalf("returned %q with a refusal", got)
			}
			wantRefusal(t, err, "identity", tc.fragment)
		})
	}
	t.Run("a differing input is named, not the others", func(t *testing.T) {
		_, err := checkIdentity(e, other, e)
		if err == nil || strings.Contains(err.Error(), "origin") || strings.Contains(err.Error(), "HEAD is") {
			t.Fatalf("got %v", err)
		}
	})
}

// chain is a scratch work repository with a bare origin. Its linear history
// a, b, c, d, e is on branch ezop, which is pushed; u is a commit on a side
// branch that branches off b and is not part of ezop.
type chain struct {
	work             *scratch
	bare             string
	a, b, c, d, e, u string
}

func newChain(t *testing.T) *chain {
	t.Helper()
	c := &chain{work: newScratch(t), bare: newBare(t)}
	c.work.git("remote", "add", "origin", c.bare)
	var shas []string
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		c.work.write("f.txt", name)
		shas = append(shas, c.work.commit("commit "+name))
	}
	c.a, c.b, c.c, c.d, c.e = shas[0], shas[1], shas[2], shas[3], shas[4]
	c.work.git("checkout", "-q", "-b", "side", c.b)
	c.work.write("side.txt", "u")
	c.u = c.work.commit("commit u")
	c.work.git("checkout", "-q", "main")
	c.work.git("branch", "-m", "ezop")
	c.work.git("push", "-q", "origin", "ezop")
	return c
}

type tagKind int

const (
	lightweight tagKind = iota
	annotated
	nested
)

// makeTag creates a tag in the work repository only.
func (c *chain) makeTag(kind tagKind, name, rev string) {
	switch kind {
	case lightweight:
		c.work.git("tag", name, rev)
	case annotated:
		c.work.git("tag", "-a", "-m", "annotated", name, rev)
	case nested:
		c.work.git("tag", "-a", "-m", "inner", name+"-inner", rev)
		c.work.git("tag", "-a", "-m", "outer", name, name+"-inner")
		c.work.git("tag", "-d", name+"-inner")
	}
}

// pushTag creates a tag and pushes it to the scratch origin.
func (c *chain) pushTag(kind tagKind, name, rev string) {
	c.makeTag(kind, name, rev)
	c.work.git("push", "-q", "origin", "refs/tags/"+name)
}

func (c *chain) originTags(t *testing.T) map[string]string {
	t.Helper()
	tags, err := c.work.adapter().OriginTags()
	if err != nil {
		t.Fatal(err)
	}
	return tags
}

func TestReachableDecision(t *testing.T) {
	if err := reachableDecision(0); err != nil {
		t.Fatal(err)
	}
	wantRefusal(t, reachableDecision(1), "reachable", "not reachable")
	for _, status := range []int{128, 2, -1} {
		err := reachableDecision(status)
		wantRefusal(t, err, "reachable", "cannot decide")
		if strings.Contains(err.Error(), "not reachable") {
			t.Fatalf("an undecidable status reads as a definite no: %v", err)
		}
	}
}

func TestCheckReachable(t *testing.T) {
	c := newChain(t)
	g := c.work.adapter()
	t.Run("the tip of origin ezop passes", func(t *testing.T) {
		if err := checkReachable(g, c.e); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("an ancestor of the tip passes", func(t *testing.T) {
		if err := checkReachable(g, c.b); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a commit that is not on origin ezop is not reachable", func(t *testing.T) {
		wantRefusal(t, checkReachable(g, c.u), "reachable", "not reachable")
	})
	t.Run("a commit ahead of the fetched tip is not reachable", func(t *testing.T) {
		c.work.write("f.txt", "ahead")
		ahead := c.work.commit("ahead, never pushed")
		wantRefusal(t, checkReachable(g, ahead), "reachable", "not reachable")
	})
	t.Run("a commit git does not know cannot be decided", func(t *testing.T) {
		err := checkReachable(g, strings.Repeat("ab", 20))
		wantRefusal(t, err, "reachable", "cannot decide")
	})
	t.Run("a failed fetch is told apart from both", func(t *testing.T) {
		broken := newScratch(t)
		broken.git("remote", "add", "origin", filepath.Join(t.TempDir(), "gone.git"))
		err := checkReachable(broken.adapter(), c.e)
		wantRefusal(t, err, "reachable", "fetch")
		if strings.Contains(err.Error(), "not reachable") || strings.Contains(err.Error(), "gone.git") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a SHA that is not a full SHA is refused before git is asked", func(t *testing.T) {
		wantRefusal(t, checkReachable(g, "main"), "reachable", "cannot decide")
	})
}

func TestCompareTriples(t *testing.T) {
	cases := []struct {
		a, b [3]string
		want int
	}{
		{[3]string{"5", "10", "0"}, [3]string{"5", "9", "0"}, 1},
		{[3]string{"5", "9", "0"}, [3]string{"5", "10", "0"}, -1},
		{[3]string{"6", "0", "0"}, [3]string{"5", "99", "99"}, 1},
		{[3]string{"5", "1", "2"}, [3]string{"5", "1", "10"}, -1},
		{[3]string{"5", "1", "0"}, [3]string{"5", "1", "0"}, 0},
		{[3]string{"05", "1", "0"}, [3]string{"5", "1", "0"}, 0},
		{[3]string{"0", "0", "0"}, [3]string{"00", "0", "0"}, 0},
		{[3]string{"99999999999999999999999", "0", "0"}, [3]string{"99999999999999999999998", "0", "0"}, 1},
		{[3]string{"100000000000000000000000", "0", "0"}, [3]string{"99999999999999999999999", "0", "0"}, 1},
	}
	for _, tc := range cases {
		if got := compareTriples(tc.a, tc.b); got != tc.want {
			t.Errorf("compareTriples(%v, %v) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := compareTriples(tc.b, tc.a); got != -tc.want {
			t.Errorf("compareTriples(%v, %v) = %d, want %d", tc.b, tc.a, got, -tc.want)
		}
	}
}

func TestUpstreamBase(t *testing.T) {
	t.Run("the highest numeric triple wins, not the latest or the lexical one", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.0.0", c.a)
		c.pushTag(annotated, "v5.1.0", c.b)
		c.pushTag(nested, "v5.9.0", c.c)
		c.pushTag(lightweight, "v5.10.0", c.d)
		got, err := upstreamBase(c.work.adapter(), c.e, c.originTags(t))
		if err != nil || got != "v5.10.0" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("the highest tag wins even when an older commit carries it", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.10.0", c.a)
		c.pushTag(lightweight, "v5.9.0", c.d)
		got, err := upstreamBase(c.work.adapter(), c.e, c.originTags(t))
		if err != nil || got != "v5.10.0" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("a tag on E itself is a candidate", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(annotated, "v5.1.0", c.b)
		got, err := upstreamBase(c.work.adapter(), c.b, c.originTags(t))
		if err != nil || got != "v5.1.0" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("a tag that is not an ancestor of E is ignored", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.1.0", c.b)
		c.pushTag(lightweight, "v5.20.0", c.u)
		c.pushTag(lightweight, "v5.30.0", c.e)
		// E is c: the later tag is ahead of it, the side one is a sibling.
		got, err := upstreamBase(c.work.adapter(), c.c, c.originTags(t))
		if err != nil || got != "v5.1.0" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("fork tags, pre-releases and other names never match", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.1.0", c.b)
		for _, name := range []string{"v9.0.0-ezop.1", "v9.0.0-ezop.9", "v9.0.0-rc.1", "9.0.0", "v9.0", "v9.0.0.1", "V9.0.0", "release-9.0.0", "v9.0.0-"} {
			c.pushTag(lightweight, name, c.d)
		}
		got, err := upstreamBase(c.work.adapter(), c.e, c.originTags(t))
		if err != nil || got != "v5.1.0" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("a tag that exists only locally is not a candidate", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.0.0", c.a)
		c.makeTag(lightweight, "v5.1.0", c.b)
		c.makeTag(annotated, "v5.2.0", c.c)
		got, err := upstreamBase(c.work.adapter(), c.e, c.originTags(t))
		if err != nil || got != "v5.0.0" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("a base that exists only locally leaves no candidate", func(t *testing.T) {
		c := newChain(t)
		c.makeTag(lightweight, "v5.1.0", c.b)
		got, err := upstreamBase(c.work.adapter(), c.e, c.originTags(t))
		wantRefusal(t, err, "base", "no")
		if got != "" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("no tag at all leaves no candidate", func(t *testing.T) {
		c := newChain(t)
		_, err := upstreamBase(c.work.adapter(), c.e, c.originTags(t))
		wantRefusal(t, err, "base", "no")
	})
	t.Run("only excluded names leave no candidate", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.1.0-ezop.1", c.b)
		c.pushTag(lightweight, "v5.1.0-rc.1", c.b)
		_, err := upstreamBase(c.work.adapter(), c.e, c.originTags(t))
		wantRefusal(t, err, "base", "no")
	})
	t.Run("only unmerged tags leave no candidate", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.20.0", c.u)
		_, err := upstreamBase(c.work.adapter(), c.e, c.originTags(t))
		wantRefusal(t, err, "base", "no")
	})
	t.Run("two names with one numeric triple refuse", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.1.0", c.b)
		c.pushTag(lightweight, "v05.1.0", c.a)
		_, err := upstreamBase(c.work.adapter(), c.e, c.originTags(t))
		wantRefusal(t, err, "base", "same")
	})
	// A control of existing behavior, green from the start: a tie below a higher
	// untied tag still refuses, so the rule cannot shrink to a tie at the top.
	t.Run("a tie below a higher untied tag refuses", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.0.0", c.a)
		c.pushTag(lightweight, "v05.0.0", c.b)
		c.pushTag(lightweight, "v5.9.0", c.c)
		got, err := upstreamBase(c.work.adapter(), c.e, c.originTags(t))
		wantRefusal(t, err, "base", "same")
		if got != "" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("two names on one commit with different triples are not a tie", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.1.0", c.b)
		c.pushTag(lightweight, "v5.1.1", c.b)
		got, err := upstreamBase(c.work.adapter(), c.e, c.originTags(t))
		if err != nil || got != "v5.1.1" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("a candidate whose commit is missing is unreadable, not ignored", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.1.0", c.b)
		c.pushTag(lightweight, "v5.30.0", c.u)
		// A clone that fetched only ezop has the tag's name but not its commit.
		clone := newScratch(t)
		clone.git("remote", "add", "origin", c.bare)
		clone.git("fetch", "-q", "--no-tags", "origin", "ezop")
		if _, err := clone.gitErr("cat-file", "-e", c.u+"^{commit}"); err == nil {
			t.Fatal("control: the clone has the side commit")
		}
		tags, err := clone.adapter().OriginTags()
		if err != nil || tags["v5.30.0"] != c.u {
			t.Fatalf("control: %v, %v", tags, err)
		}
		got, err := upstreamBase(clone.adapter(), c.e, tags)
		wantRefusal(t, err, "base", "cannot decide")
		if got != "" || strings.Contains(err.Error(), "no candidate") {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("a candidate that names a tree is unreadable", func(t *testing.T) {
		c := newChain(t)
		tree := c.work.git("rev-parse", c.b+"^{tree}")
		_, err := upstreamBase(c.work.adapter(), c.e, map[string]string{"v5.1.0": tree})
		wantRefusal(t, err, "base", "cannot decide")
	})
	t.Run("git that cannot run is unreadable", func(t *testing.T) {
		g := execGit{dir: filepath.Join(t.TempDir(), "no-such-dir")}
		_, err := upstreamBase(g, strings.Repeat("a", 40), map[string]string{"v5.1.0": strings.Repeat("b", 40)})
		wantRefusal(t, err, "base", "cannot decide")
	})
	t.Run("a candidate undecidable among decidable ones still refuses", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.1.0", c.b)
		tags := c.originTags(t)
		tags["v5.2.0"] = strings.Repeat("ab", 20)
		_, err := upstreamBase(c.work.adapter(), c.e, tags)
		wantRefusal(t, err, "base", "cannot decide")
	})
	t.Run("E that is not a full SHA is refused", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.1.0", c.b)
		_, err := upstreamBase(c.work.adapter(), "main", c.originTags(t))
		wantRefusal(t, err, "base")
	})
	t.Run("pushes and writes nothing", func(t *testing.T) {
		c := newChain(t)
		c.pushTag(lightweight, "v5.1.0", c.b)
		c.makeTag(lightweight, "v5.2.0", c.c)
		bare := &scratch{t: t, dir: c.bare, env: c.work.env}
		beforeRemote, beforeLocal := bare.git("for-each-ref"), c.work.git("for-each-ref")
		if _, err := upstreamBase(c.work.adapter(), c.e, c.originTags(t)); err != nil {
			t.Fatal(err)
		}
		if bare.git("for-each-ref") != beforeRemote || c.work.git("for-each-ref") != beforeLocal {
			t.Fatal("a ref changed")
		}
	})
}

func TestCheckBaseMatch(t *testing.T) {
	for _, tc := range []struct{ tag, base string }{
		{"v5.1.0-ezop.1", "v5.1.0"},
		{"v5.1.0-ezop.12", "v5.1.0"},
		{"v10.20.30-ezop.2", "v10.20.30"},
	} {
		if err := checkBaseMatch(tc.tag, tc.base); err != nil {
			t.Errorf("%s on %s: %v", tc.tag, tc.base, err)
		}
	}
	for name, tc := range map[string]struct{ tag, base string }{
		"a later minor on a v5.1.0 base":    {"v5.2.0-ezop.1", "v5.1.0"},
		"an earlier major on a v5.1.0 base": {"v5.0.0-ezop.1", "v5.1.0"},
		"a later patch":                     {"v5.1.1-ezop.1", "v5.1.0"},
		"a leading zero is another string":  {"v5.1.0-ezop.1", "v05.1.0"},
		"a leading zero in the tag":         {"v05.1.0-ezop.1", "v5.1.0"},
		"an empty base":                     {"v5.1.0-ezop.1", ""},
		"a base without v":                  {"v5.1.0-ezop.1", "5.1.0"},
		"a base that is a fork tag":         {"v5.1.0-ezop.1", "v5.1.0-ezop.1"},
		"a tag that is not a Release Tag":   {"v5.1.0", "v5.1.0"},
		"a padded tag":                      {"v5.1.0-ezop.1\n", "v5.1.0"},
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			wantRefusal(t, checkBaseMatch(tc.tag, tc.base), "base")
		})
	}
}

// recGit records the calls that reach the git adapter, in order, and passes
// them on to the real one.
type recGit struct {
	gitRepo
	calls []string
}

func (r *recGit) OriginURLs(push bool) ([]string, error) {
	r.calls = append(r.calls, "urls")
	return r.gitRepo.OriginURLs(push)
}

func (r *recGit) FetchEzop() (string, error) {
	r.calls = append(r.calls, "fetch")
	return r.gitRepo.FetchEzop()
}

func (r *recGit) ResolveCommit(rev string) (string, error) {
	r.calls = append(r.calls, "resolve "+rev)
	return r.gitRepo.ResolveCommit(rev)
}

func (r *recGit) IsAncestor(a, d string) (int, error) {
	r.calls = append(r.calls, "ancestor")
	return r.gitRepo.IsAncestor(a, d)
}

func (r *recGit) Tree(commit string, paths []string) ([]treeEntry, error) {
	r.calls = append(r.calls, "tree")
	return r.gitRepo.Tree(commit, paths)
}

func (r *recGit) Blob(oid string) ([]byte, error) {
	r.calls = append(r.calls, "blob")
	return r.gitRepo.Blob(oid)
}

func (r *recGit) OriginTags() (map[string]string, error) {
	r.calls = append(r.calls, "origin tags")
	return r.gitRepo.OriginTags()
}

func (r *recGit) count(prefix string) int {
	n := 0
	for _, c := range r.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// release is a scratch origin with the history c1 (tagged v5.1.0) and e (the
// tip of ezop, tagged v5.1.0-ezop.1 and v5.1.0-ezop.2), and a work clone that
// has fetched ezop and the tags and has e checked out.
type release struct {
	bare  *scratch
	work  *scratch
	c1, e string
}

func newRelease(t *testing.T, baseOnE bool) *release {
	t.Helper()
	bareDir := newBare(t)
	src := newScratch(t)
	src.git("remote", "add", "origin", bareDir)
	src.write("go.mod", goodMod)
	src.write("devbox.lock", goodLock)
	c1 := src.commit("upstream release")
	src.write("fork.txt", "release tooling")
	e := src.commit("fork commit")
	src.git("branch", "-m", "ezop")
	src.git("push", "-q", "origin", "ezop")
	if baseOnE {
		src.git("tag", "-a", "-m", "upstream", "v5.1.0", e)
	} else {
		src.git("tag", "-a", "-m", "upstream", "v5.1.0", c1)
	}
	src.git("tag", "v5.1.0-ezop.1", e)
	src.git("tag", "-a", "-m", "fork", "v5.1.0-ezop.2", e)
	src.git("push", "-q", "origin", "--tags")

	work := newScratch(t)
	work.git("remote", "add", "origin", bareDir)
	work.git("fetch", "-q", "origin", "+refs/heads/ezop:refs/remotes/origin/ezop", "+refs/tags/*:refs/tags/*")
	work.git("checkout", "-q", "-b", "ezop", "origin/ezop")
	return &release{
		bare: &scratch{t: t, dir: bareDir, env: work.env},
		work: work, c1: c1, e: e,
	}
}

func (r *release) refs() string {
	return r.bare.git("for-each-ref") + "\n--\n" + r.work.git("for-each-ref") + "\n--\n" + r.work.git("rev-parse", "HEAD")
}

func TestCheckTagBinding(t *testing.T) {
	const tag = "v5.1.0-ezop.1"
	t.Run("a Release Tag that agrees everywhere passes, and no tag or remote ref is written", func(t *testing.T) {
		r := newRelease(t, false)
		before := r.refs()
		rg := &recGit{gitRepo: r.work.adapter()}
		got, err := checkTagBinding(rg, tag, r.e, goodPin)
		if err != nil || got != r.e {
			t.Fatalf("got %q, %v", got, err)
		}
		if after := r.refs(); after != before {
			t.Fatalf("a ref changed:\n%s\n-- became --\n%s", before, after)
		}
		for prefix, want := range map[string]int{"resolve HEAD": 1, "resolve " + r.e: 1, "origin tags": 1, "fetch": 1, "urls": 0} {
			if got := rg.count(prefix); got != want {
				t.Errorf("%d calls to %q, want %d (%v)", got, prefix, want, rg.calls)
			}
		}
		if !strings.HasPrefix(rg.calls[0], "resolve ") || rg.calls[len(rg.calls)-1] != "blob" && rg.calls[len(rg.calls)-1] != "tree" {
			t.Errorf("calls out of order: %v", rg.calls)
		}
	})
	// The clone of newRelease already has refs/remotes/origin/ezop, so the case
	// above cannot show what the required fetch writes. Here the tracking ref is
	// deleted first: the fetch recreates it and FETCH_HEAD, and nothing else
	// local or remote changes. (A control of existing behavior, green from the
	// start.)
	t.Run("the required fetch updates the local tracking ref and FETCH_HEAD and nothing else", func(t *testing.T) {
		r := newRelease(t, false)
		r.work.git("update-ref", "-d", "refs/remotes/origin/ezop")
		if _, err := r.work.gitErr("rev-parse", "--verify", "-q", "refs/remotes/origin/ezop"); err == nil {
			t.Fatal("control: the tracking ref is still there")
		}
		beforeRemote := r.bare.git("for-each-ref")
		beforeTags := r.work.git("for-each-ref", "refs/tags", "refs/heads")
		if _, err := checkTagBinding(r.work.adapter(), tag, r.e, goodPin); err != nil {
			t.Fatal(err)
		}
		if got := r.work.git("rev-parse", "refs/remotes/origin/ezop"); got != r.e {
			t.Fatalf("tracking ref %q, want %q", got, r.e)
		}
		if got := r.work.git("rev-parse", "FETCH_HEAD"); got != r.e {
			t.Fatalf("FETCH_HEAD %q, want %q", got, r.e)
		}
		if got := r.work.git("for-each-ref", "refs/tags", "refs/heads"); got != beforeTags {
			t.Fatalf("a local tag or branch changed:\n%s\n-- became --\n%s", beforeTags, got)
		}
		if got := r.bare.git("for-each-ref"); got != beforeRemote {
			t.Fatalf("a ref of origin changed:\n%s\n-- became --\n%s", beforeRemote, got)
		}
	})
	t.Run("the second Release Tag on the commit passes too", func(t *testing.T) {
		r := newRelease(t, false)
		got, err := checkTagBinding(r.work.adapter(), "v5.1.0-ezop.2", r.e, goodPin)
		if err != nil || got != r.e {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("an Upstream tag on the same commit passes", func(t *testing.T) {
		r := newRelease(t, true)
		got, err := checkTagBinding(r.work.adapter(), tag, r.e, goodPin)
		if err != nil || got != r.e {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("GITHUB_SHA may name the annotated tag object", func(t *testing.T) {
		r := newRelease(t, false)
		object := r.work.git("rev-parse", "v5.1.0-ezop.2")
		if object == r.e {
			t.Fatal("control: the annotated tag object is the commit")
		}
		if _, err := checkTagBinding(r.work.adapter(), tag, object, goodPin); err != nil {
			t.Fatal(err)
		}
	})

	// refuses runs one failing case: it must refuse with the check and stop
	// before every call named in notCalled; no ref may change.
	refuses := func(t *testing.T, r *release, tag, githubSHA, pin, check string, notCalled ...string) {
		t.Helper()
		before := r.refs()
		rg := &recGit{gitRepo: r.work.adapter()}
		got, err := checkTagBinding(rg, tag, githubSHA, pin)
		wantRefusal(t, err, check)
		if got != "" {
			t.Fatalf("returned %q with a refusal", got)
		}
		for _, prefix := range notCalled {
			if rg.count(prefix) != 0 {
				t.Fatalf("%q was called after the refusal: %v", prefix, rg.calls)
			}
		}
		if after := r.refs(); after != before {
			t.Fatalf("a ref changed:\n%s\n-- became --\n%s", before, after)
		}
	}

	t.Run("a bad tag name stops before git is asked anything", func(t *testing.T) {
		r := newRelease(t, false)
		rg := &recGit{gitRepo: r.work.adapter()}
		_, err := checkTagBinding(rg, "v5.1.0-ezop.0", r.e, goodPin)
		wantRefusal(t, err, "tag")
		if len(rg.calls) != 0 {
			t.Fatalf("calls %v", rg.calls)
		}
	})
	t.Run("a tag that is not on origin refuses at identity", func(t *testing.T) {
		r := newRelease(t, false)
		refuses(t, r, "v5.1.0-ezop.3", r.e, goodPin, "identity", "fetch", "ancestor", "tree")
	})
	t.Run("a tag that exists only locally refuses at identity", func(t *testing.T) {
		r := newRelease(t, false)
		r.work.git("tag", "v5.1.0-ezop.3", r.e)
		refuses(t, r, "v5.1.0-ezop.3", r.e, goodPin, "identity", "fetch", "ancestor", "tree")
	})
	t.Run("a tag moved on origin refuses at identity", func(t *testing.T) {
		r := newRelease(t, false)
		r.bare.git("update-ref", "refs/tags/"+tag, r.c1)
		refuses(t, r, tag, r.e, goodPin, "identity", "fetch", "ancestor", "tree")
	})
	t.Run("a tag moved to a different commit than the annotated peel refuses", func(t *testing.T) {
		r := newRelease(t, false)
		r.bare.git("update-ref", "refs/tags/v5.1.0-ezop.2", r.c1)
		refuses(t, r, "v5.1.0-ezop.2", r.e, goodPin, "identity", "fetch", "ancestor", "tree")
	})
	t.Run("a GITHUB_SHA of another commit refuses at identity", func(t *testing.T) {
		r := newRelease(t, false)
		refuses(t, r, tag, r.c1, goodPin, "identity", "fetch", "ancestor", "tree")
	})
	t.Run("an empty or unknown GITHUB_SHA refuses at identity", func(t *testing.T) {
		r := newRelease(t, false)
		refuses(t, r, tag, "", goodPin, "identity", "fetch", "ancestor", "tree")
		refuses(t, r, tag, strings.Repeat("ab", 20), goodPin, "identity", "fetch", "ancestor", "tree")
	})
	t.Run("a HEAD of another commit refuses at identity", func(t *testing.T) {
		r := newRelease(t, false)
		r.work.git("checkout", "-q", "--detach", r.c1)
		refuses(t, r, tag, r.e, goodPin, "identity", "fetch", "ancestor", "tree")
	})
	t.Run("origin that cannot be read refuses at identity", func(t *testing.T) {
		r := newRelease(t, false)
		r.work.git("remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
		refuses(t, r, tag, r.e, goodPin, "identity", "fetch", "ancestor", "tree")
	})
	t.Run("a commit that is not on origin ezop refuses at reachable", func(t *testing.T) {
		r := newRelease(t, false)
		r.work.write("stray.txt", "x")
		stray := r.work.commit("stray")
		r.work.git("push", "-q", "origin", stray+":refs/tags/v5.1.0-ezop.9")
		refuses(t, r, "v5.1.0-ezop.9", stray, goodPin, "reachable", "tree")
		rg := &recGit{gitRepo: r.work.adapter()}
		_, _ = checkTagBinding(rg, "v5.1.0-ezop.9", stray, goodPin)
		if rg.count("ancestor") != 1 || rg.count("fetch") != 1 {
			t.Fatalf("the run went on after the refusal: %v", rg.calls)
		}
	})
	t.Run("a tag on a base that is not the tag's X.Y.Z refuses at base", func(t *testing.T) {
		for _, name := range []string{"v5.2.0-ezop.1", "v5.0.0-ezop.1"} {
			r := newRelease(t, false)
			r.work.git("push", "-q", "origin", r.e+":refs/tags/"+name)
			refuses(t, r, name, r.e, goodPin, "base", "tree")
		}
	})
	t.Run("no Upstream tag at all refuses at base", func(t *testing.T) {
		r := newRelease(t, false)
		r.bare.git("tag", "-d", "v5.1.0")
		refuses(t, r, tag, r.e, goodPin, "base", "tree")
	})
	t.Run("a base that exists only locally refuses at base", func(t *testing.T) {
		r := newRelease(t, false)
		r.bare.git("tag", "-d", "v5.1.0")
		r.work.git("tag", "-f", "v5.1.0", r.c1)
		refuses(t, r, tag, r.e, goodPin, "base", "tree")
	})
	t.Run("an unreadable base refuses at base", func(t *testing.T) {
		r := newRelease(t, false)
		side := newScratch(t)
		side.git("remote", "add", "origin", r.bare.dir)
		side.write("other.txt", "unmerged upstream work")
		unmerged := side.commit("unmerged")
		side.git("push", "-q", "origin", unmerged+":refs/tags/v5.9.0")
		if _, err := r.work.gitErr("cat-file", "-e", unmerged+"^{commit}"); err == nil {
			t.Fatal("control: the work clone has the commit")
		}
		refuses(t, r, tag, r.e, goodPin, "base", "tree")
	})
	t.Run("a different GoReleaser pin refuses at toolchain", func(t *testing.T) {
		r := newRelease(t, false)
		refuses(t, r, tag, r.e, "v2.99.0", "toolchain")
	})
	t.Run("a malformed pin refuses at toolchain", func(t *testing.T) {
		r := newRelease(t, false)
		refuses(t, r, tag, r.e, "2.17.1", "toolchain")
	})
}
