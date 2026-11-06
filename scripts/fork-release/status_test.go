package main

import (
	"bytes"
	"errors"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

// The fixtures of verify --status. One stCase is one state of the world: the
// record at the tip of ezop, the releases list, the tag on origin and the runs
// of the workflow for a commit. build turns it into guard dependencies over
// fakes that log every read, in order, as a kind: record, tag, release,
// listing. Nothing here touches a network, a process or this repository.

const (
	stPeel     = rnE // the commit a tag with no record peels to
	stRun      = "123456789/1"
	stOtherRun = "123456789/2"
)

// stAPI is the GitHub reader of a status run: the fake with every read logged
// by kind. A download is a failure of the test, whenever it is asked for.
type stAPI struct {
	*fakeAPI
	t  *testing.T
	ev *[]string
}

func (a stAPI) Get(endpoint string) ([]byte, error) {
	kind := "other"
	switch {
	case strings.HasPrefix(endpoint, "git/"):
		kind = "record"
	case strings.HasPrefix(endpoint, "releases?"):
		kind = "release"
	case strings.Contains(endpoint, "/runs?"):
		kind = "listing"
	}
	*a.ev = append(*a.ev, kind)
	return a.fakeAPI.Get(endpoint)
}

func (a stAPI) Download(string) ([]byte, error) {
	a.t.Helper()
	a.t.Fatal("verify --status downloaded an asset")
	return nil, nil
}

// stGit is the fake git with a canned tag list, logged as the kind tag.
type stGit struct {
	*fakeGit
	tags    map[string]string
	tagsErr error
	ev      *[]string
}

func (g *stGit) OriginTags() (map[string]string, error) {
	*g.ev = append(*g.ev, "tag")
	g.calls = append(g.calls, "origin tags")
	return g.tags, g.tagsErr
}

type stCase struct {
	// record is the world of the record lookup; nil makes the tip unreadable.
	record func() *fakeAPI
	// releases are the entries of the one page of the releases list; releaseFail
	// fails the page.
	releases    []string
	releaseFail bool
	// e is the commit the listing is asked for; runs are its entries and total
	// its total_count (the count of runs when zero and runs is not empty).
	e        string
	runs     []string
	total    int
	runsFail bool
	// tags is the origin's tag list; tagsErr fails the read.
	tags    map[string]string
	tagsErr error

	// want
	out   string
	code  int
	reads []string
}

type stEnv struct {
	d    guardDeps
	api  stAPI
	git  *stGit
	out  *bytes.Buffer
	errb *bytes.Buffer
	ev   *[]string
}

func (c stCase) build(t *testing.T) stEnv {
	t.Helper()
	ev := &[]string{}
	var world *fakeAPI
	if c.record != nil {
		world = c.record()
	} else {
		world = lkWorld(nil, nil)
		world.fail[epRef] = true
	}
	page1 := "releases?per_page=100&page=1"
	world.resp[page1] = relPage(c.releases...)
	world.resp["releases?per_page=100&page=2"] = "[]"
	world.fail[page1] = c.releaseFail
	if c.e != "" {
		total := c.total
		if total == 0 {
			total = len(c.runs)
		}
		world.resp[runsEndpoint(c.e, 1)] = rnListing(total, c.runs...)
		world.fail[runsEndpoint(c.e, 1)] = c.runsFail
	}
	g := &stGit{fakeGit: newFakeGit(nil), tags: c.tags, tagsErr: c.tagsErr, ev: ev}
	d, out, errb := testDeps(g.fakeGit)
	d.git = g
	api := stAPI{fakeAPI: world, t: t, ev: ev}
	d.gh = api
	return stEnv{d: d, api: api, git: g, out: out, errb: errb, ev: ev}
}

// kinds collapses the log of reads to its runs of one kind.
func kinds(ev []string) []string {
	var out []string
	for _, k := range ev {
		if len(out) == 0 || out[len(out)-1] != k {
			out = append(out, k)
		}
	}
	return out
}

// ---- record worlds ----

func stAbsentWorld() *fakeAPI {
	return lkWorld([]lkEntry{lkBlobEntry("README.md", lkReadme)}, map[string][]byte{blobID(lkReadme): lkReadme})
}

func stRecordWorld(data []byte) func() *fakeAPI {
	return func() *fakeAPI {
		return lkWorld([]lkEntry{
			lkBlobEntry("README.md", lkReadme),
			lkBlobEntry(recTag+".md", data),
		}, map[string][]byte{blobID(data): data, blobID(lkReadme): lkReadme})
	}
}

func stEntryWorld(e lkEntry) func() *fakeAPI {
	return func() *fakeAPI {
		return lkWorld([]lkEntry{lkBlobEntry("README.md", lkReadme), e}, map[string][]byte{blobID(lkReadme): lkReadme})
	}
}

func stVerifiedRecord() []byte  { return recordFor(recTag, true) }
func stAbandonedRecord() []byte { return recordFor(recTag, false) }

// stBody is a notes body for the commit and the run.
func stBody(e, run string) string {
	return "Fork release.\n\nCommit: " + e + "\nRun: " + run + "\n"
}

// stRuns are runs of the commit with the given statuses; a run that is not
// completed has no conclusion.
func stRuns(e string, statuses ...string) []string {
	var out []string
	for i, s := range statuses {
		concl := ""
		if s == "completed" {
			concl = "success"
		}
		out = append(out, rnRun(300+i, 1, s, concl, e, "push", 77))
	}
	return out
}

func published(e, run string) []string { return []string{relEntry(recTag, false, stBody(e, run))} }

// stAbsent is a record Absent for recTag, the tag on origin at stPeel, a clean
// listing and no release.
func stAbsent() stCase {
	return stCase{
		record: func() *fakeAPI { return stAbsentWorld() },
		e:      stPeel,
		runs:   stRuns(stPeel, "completed"),
		tags:   map[string]string{recTag: stPeel},
	}
}

// stVerified is a valid verified record, the release Published-Same for its
// commit and run, the tag at its commit and a clean listing: row 9.
func stVerified() stCase {
	return stCase{
		record:   stRecordWorld(stVerifiedRecord()),
		releases: published(recCommit, stRun),
		e:        recCommit,
		runs:     stRuns(recCommit, "completed"),
		tags:     map[string]string{recTag: recCommit},
	}
}

func withWant(c stCase, out string, code int, reads ...string) stCase {
	c.out, c.code, c.reads = out, code, reads
	return c
}

func statusLines(token, release string) string {
	return "record: " + token + "\nrelease: " + release + "\n"
}

var allReads = []string{"record", "tag", "release", "listing"}

// stTable is one fixture per row of the status table, with variants and the
// order cases. Every case names the row it proves.
func stTable() map[string]stCase {
	t := map[string]stCase{}

	// Row 1: the record lookup is Unknown. Nothing else is read.
	t["row 1: the record lookup is Unknown"] = withWant(stCase{}, statusLines("unknown", "not read"), 4, "record")

	// Row 2: something that is not a file is at the record path.
	for name, e := range map[string]lkEntry{
		"a directory": {recTag + ".md", "040000", "tree", lkOtherSHA},
		"a submodule": {recTag + ".md", "160000", "commit", lkOtherSHA},
		"a symlink":   {recTag + ".md", "120000", "blob", lkOtherSHA},
	} {
		t["row 2: "+name+" at the record path"] = withWant(stCase{record: stEntryWorld(e)}, statusLines("invalid", "not read"), 6, "record")
	}

	// Row 3: Absent and a read that cannot be completed, or a run not over.
	c := stAbsent()
	c.tags, c.tagsErr = nil, errors.New("ls-remote failed")
	t["row 3: Absent and the read of TAG failed"] = withWant(c, statusLines("unknown", "None"), 4, "record", "tag", "release")
	c = stAbsent()
	c.runsFail = true
	t["row 3: Absent and the listing failed"] = withWant(c, statusLines("unknown", "None"), 4, allReads...)
	c = stAbsent()
	c.runs, c.total = stRuns(stPeel, "completed"), 150
	t["row 3: Absent and the listing is short"] = withWant(c, statusLines("unknown", "None"), 4, allReads...)
	c = stAbsent()
	c.runs, c.total = stRuns(stPeel, "completed"), 1001
	t["row 3: Absent and the listing is over the cap"] = withWant(c, statusLines("unknown", "None"), 4, allReads...)
	for _, s := range []string{"in_progress", "queued", "waiting", "mystery"} {
		c = stAbsent()
		c.runs = stRuns(stPeel, "completed", s)
		t["row 3: Absent and a run is "+s] = withWant(c, statusLines("unknown", "None"), 4, allReads...)
	}
	// A run in progress of another tag at the same commit counts.
	c = stAbsent()
	c.runs = stRuns(stPeel, "in_progress")
	c.releases = []string{relEntry("v5.1.0-ezop.2", false, stBody(stPeel, "999/1"))}
	t["row 3: Absent and a run of another tag at the commit is in progress"] = withWant(c, statusLines("unknown", "None"), 4, allReads...)

	// Row 4: Absent in any other case; the release state is printed beside.
	c = stAbsent()
	c.tags = map[string]string{}
	t["row 4: Absent and the tag is positively absent"] = withWant(c, statusLines("none", "None"), 3, "record", "tag", "release")
	c = stAbsent()
	t["row 4: Absent, the tag is at a commit, the listing is clean"] = withWant(c, statusLines("none", "None"), 3, allReads...)
	c = stAbsent()
	c.runs = nil
	t["row 4: Absent and the commit has no run at all"] = withWant(c, statusLines("none", "None"), 3, allReads...)
	c = stAbsent()
	c.releases = []string{relEntry(recTag, true, "")}
	t["row 4: a leftover Draft reads none with release Draft"] = withWant(c, statusLines("none", "Draft"), 3, allReads...)
	c = stAbsent()
	c.releases = published(stPeel, stRun)
	t["row 4: Absent beside a Published-Same release"] = withWant(c, statusLines("none", "Published-Same"), 3, allReads...)
	c = stAbsent()
	c.releases = published(relOther, stRun)
	t["row 4: Absent beside a release of another commit"] = withWant(c, statusLines("none", "Other"), 3, allReads...)
	c = stAbsent()
	c.tags = map[string]string{}
	c.releases = published(stPeel, stRun)
	// With no peel the release lookup gets the all-zero SHA: a published
	// release can only read Other, never Same.
	t["row 4: no peel, a published release reads Other"] = withWant(c, statusLines("none", "Other"), 3, "record", "tag", "release")

	// Row 4 beside AQ-1: Absent and the release lookup Unknown.
	c = stAbsent()
	c.releaseFail = true
	t["AQ-1: Absent and the release lookup Unknown"] = withWant(c, statusLines("none", "Unknown"), 3, allReads...)

	// Row 5: a regular file that is not a valid record of this tag.
	valid := string(stVerifiedRecord())
	lines := strings.SplitAfter(valid, "\n")
	for name, data := range map[string][]byte{
		"malformed":                  []byte("this is not a record\n"),
		"empty":                      {},
		"partial":                    []byte(strings.Join(lines[:8], "")),
		"no final line feed":         []byte(strings.TrimSuffix(valid, "\n")),
		"another tag":                recordFor("v5.1.0-ezop.2", true),
		"a run that did not succeed": renderRecord(setKey(validVerified(), "run-conclusion", "failure")),
		"a gate commit of another":   renderRecord(setKey(validVerified(), "attested.gate-commit", relOther)),
		"an unknown key":             []byte(valid + "extra: 1\n"),
		"too large":                  []byte(valid + strings.Repeat("x", maxRecordSize)),
	} {
		t["row 5: a file that is "+name] = withWant(stCase{record: stRecordWorld(data)}, statusLines("invalid", "not read"), 6, "record")
	}

	// Row 6: a valid abandoned record decides alone; every other read would fail.
	c = stCase{record: stRecordWorld(stAbandonedRecord()), releaseFail: true, runsFail: true, e: recCommit, tagsErr: errors.New("boom")}
	t["row 6: abandoned, whatever else fails"] = withWant(c, statusLines("abandoned", "not read"), 5, "record")
	c = stCase{record: stRecordWorld(stAbandonedRecord()), releases: published(recCommit, stRun), tags: map[string]string{recTag: recCommit}}
	t["row 6: abandoned beside a published release"] = withWant(c, statusLines("abandoned", "not read"), 5, "record")

	// Row 7: a valid verified record and a read that cannot be completed.
	c = stVerified()
	c.releaseFail = true
	t["row 7: verified and the release lookup is Unknown"] = withWant(c, statusLines("unknown", "Unknown"), 4, allReads...)
	c = stVerified()
	c.tags, c.tagsErr = nil, errors.New("ls-remote failed")
	t["row 7: verified and the fetch of TAG failed"] = withWant(c, statusLines("unknown", "Published-Same"), 4, allReads...)
	c = stVerified()
	c.runsFail = true
	t["row 7: verified and the listing failed"] = withWant(c, statusLines("unknown", "Published-Same"), 4, allReads...)
	c = stVerified()
	c.runs, c.total = stRuns(recCommit, "completed"), 150
	t["row 7: verified and the listing is short"] = withWant(c, statusLines("unknown", "Published-Same"), 4, allReads...)
	c = stVerified()
	c.runs = stRuns(recCommit, "completed", "in_progress")
	t["row 7: verified and a run is in progress"] = withWant(c, statusLines("unknown", "Published-Same"), 4, allReads...)

	// Row 8: a valid verified record and the reads disagree with it.
	c = stVerified()
	c.releases = nil
	t["row 8: verified and no release"] = withWant(c, statusLines("invalid", "None"), 6, allReads...)
	c = stVerified()
	c.releases = []string{relEntry(recTag, true, stBody(recCommit, stRun))}
	t["row 8: verified and the release is a draft"] = withWant(c, statusLines("invalid", "Draft"), 6, allReads...)
	c = stVerified()
	c.releases = published(relOther, stRun)
	t["row 8: verified and the Commit line differs"] = withWant(c, statusLines("invalid", "Other"), 6, allReads...)
	for name, body := range map[string]string{
		"missing":             "Commit: " + recCommit + "\n",
		"repeated":            "Commit: " + recCommit + "\nRun: " + stRun + "\nRun: " + stRun + "\n",
		"malformed":           "Commit: " + recCommit + "\nRun: 123456789\n",
		"another attempt":     "Commit: " + recCommit + "\nRun: " + stOtherRun + "\n",
		"another run":         "Commit: " + recCommit + "\nRun: 123456790/1\n",
		"with a leading zero": "Commit: " + recCommit + "\nRun: 0123456789/1\n",
	} {
		c = stVerified()
		c.releases = []string{relEntry(recTag, false, body)}
		t["row 8: verified and the Run line is "+name] = withWant(c, statusLines("invalid", "Published-Same"), 6, allReads...)
	}
	c = stVerified()
	c.tags = map[string]string{recTag: relOther}
	t["row 8: verified and the peel of TAG differs"] = withWant(c, statusLines("invalid", "Published-Same"), 6, allReads...)
	c = stVerified()
	c.tags = map[string]string{}
	t["row 8: verified and the tag is absent from origin"] = withWant(c, statusLines("invalid", "Published-Same"), 6, allReads...)

	// Row 9: everything agrees.
	t["row 9: verified"] = withWant(stVerified(), statusLines("verified", "Published-Same"), 0, allReads...)
	c = stVerified()
	c.releases = []string{
		relEntry("v5.1.0-ezop.2", false, stBody(recCommit, "999/1")),
		relEntry(recTag, false, stBody(recCommit, stRun)),
	}
	c.runs = stRuns(recCommit, "completed", "completed")
	t["row 9: verified beside a second release at the same commit"] = withWant(c, statusLines("verified", "Published-Same"), 0, allReads...)

	// The order cases.
	c = stAbsent()
	c.runs = stRuns(stPeel, "in_progress")
	t["order: Absent with a run in progress is unknown (row 3), not none"] = withWant(c, statusLines("unknown", "None"), 4, allReads...)
	t["order: a directory at the record path is invalid (row 2), not unknown"] = withWant(
		stCase{record: stEntryWorld(lkEntry{recTag + ".md", "040000", "tree", lkOtherSHA}), runsFail: true, tagsErr: errors.New("boom")},
		statusLines("invalid", "not read"), 6, "record")
	c = stVerified()
	c.releases = published(relOther, stRun)
	c.runs = stRuns(recCommit, "in_progress")
	t["order: verified, a run in progress and a disagreeing release is unknown (row 7)"] = withWant(c, statusLines("unknown", "Other"), 4, allReads...)
	c = stVerified()
	c.releases = published(relOther, stRun)
	c.runs = stRuns(recCommit, "completed")
	t["order: the run ends, and the same release reads invalid (row 8)"] = withWant(c, statusLines("invalid", "Other"), 6, allReads...)
	return t
}

func TestStatusTable(t *testing.T) {
	cases := stTable()
	if len(cases) < 40 {
		t.Fatalf("only %d fixtures", len(cases))
	}
	rowsSeen := map[string]bool{}
	for name, c := range cases {
		if row, _, ok := strings.Cut(name, ":"); ok {
			rowsSeen[row] = true
		}
		t.Run(name, func(t *testing.T) {
			env := c.build(t)
			code := run([]string{"verify", "--status", recTag}, env.d)
			if code != c.code {
				t.Fatalf("exit %d, want %d; stdout %q stderr %q", code, c.code, env.out, env.errb)
			}
			if env.out.String() != c.out {
				t.Fatalf("stdout %q, want %q", env.out, c.out)
			}
			if env.errb.Len() != 0 {
				t.Fatalf("stderr %q beside a status", env.errb)
			}
			if got := kinds(*env.ev); !slices.Equal(got, c.reads) {
				t.Fatalf("reads %v, want %v", got, c.reads)
			}
			assertReadCounts(t, *env.ev, env.api.calls, env.git.calls)
		})
	}
	for _, row := range []string{"row 1", "row 2", "row 3", "row 4", "row 5", "row 6", "row 7", "row 8", "row 9", "order", "AQ-1"} {
		if !rowsSeen[row] {
			t.Fatalf("no fixture for %s", row)
		}
	}
}

// assertReadCounts: at most one record lookup, one release lookup, one read of
// TAG and one listing, no asset read of any kind, no run read other than the
// listing, and one fetch of ezop.
func assertReadCounts(t *testing.T, ev, apiCalls, gitCalls []string) {
	t.Helper()
	count := func(kind string) int {
		n := 0
		for _, k := range ev {
			if k == kind {
				n++
			}
		}
		return n
	}
	if n := countOf(apiCalls, epRef); n > 1 {
		t.Fatalf("%d record lookups", n)
	}
	if n := countOf(apiCalls, "releases?per_page=100&page=1"); n > 1 {
		t.Fatalf("%d release lookups", n)
	}
	if n := count("tag"); n > 1 {
		t.Fatalf("%d reads of TAG", n)
	}
	pageOnes := 0
	for _, c := range apiCalls {
		if strings.Contains(c, "/runs?") && strings.HasSuffix(c, "&page=1") {
			pageOnes++
		}
		if strings.Contains(c, "assets") || strings.HasPrefix(c, "actions/runs/") || c == workflowEndpoint() {
			t.Fatalf("a read that a status does not make: %q", c)
		}
	}
	if pageOnes > 1 {
		t.Fatalf("%d run listings", pageOnes)
	}
	if count("other") != 0 {
		t.Fatal("a read of another kind")
	}
	if n := countOf(gitCalls, "fetch"); n != 1 {
		t.Fatalf("%d fetches", n)
	}
	if n := countOf(gitCalls, "origin tags"); n > 1 {
		t.Fatalf("%d tag listings", n)
	}
}

func TestStatusRecordDecidesAloneReadsNothingElse(t *testing.T) {
	// Rows 1, 2, 5 and 6 make no read after the record lookup: not the tag list,
	// not the releases, not the listing.
	for name, c := range stTable() {
		if !strings.HasPrefix(name, "row 1:") && !strings.HasPrefix(name, "row 2:") && !strings.HasPrefix(name, "row 5:") && !strings.HasPrefix(name, "row 6:") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			env := c.build(t)
			run([]string{"verify", "--status", recTag}, env.d)
			if slices.ContainsFunc(*env.ev, func(k string) bool { return k != "record" }) {
				t.Fatalf("reads %v", *env.ev)
			}
			if env.git.did("origin tags") {
				t.Fatal("the tags of origin were listed")
			}
		})
	}
}

func TestStatusExitCodes(t *testing.T) {
	want := map[statusToken]int{tokenVerified: 0, tokenNone: 3, tokenUnknown: 4, tokenAbandoned: 5, tokenInvalid: 6}
	seen := map[int]bool{}
	for tok, code := range want {
		if tok.exitCode() != code {
			t.Fatalf("%s exits %d, want %d", tok, tok.exitCode(), code)
		}
		if seen[code] {
			t.Fatalf("code %d is shared", code)
		}
		seen[code] = true
		if tok != tokenVerified && (code == 0 || code == exitHeld || code == exitUsage) {
			t.Fatalf("%s exits %d, which is a pass, a refusal or a usage error", tok, code)
		}
	}
	if string(tokenVerified) != "verified" || string(tokenNone) != "none" || string(tokenUnknown) != "unknown" ||
		string(tokenAbandoned) != "abandoned" || string(tokenInvalid) != "invalid" {
		t.Fatal("a token is spelled differently")
	}
	if statusToken("other").exitCode() == 0 {
		t.Fatal("an unknown token exits zero")
	}
}

// The AQ-1 fixture and the pre-re-run rule.
func TestPreRerunRule(t *testing.T) {
	env := func() stEnv {
		c := stAbsent()
		c.releaseFail = true
		return c.build(t)
	}()
	code := run([]string{"verify", "--status", recTag}, env.d)
	if code != 3 || env.out.String() != "record: none\nrelease: Unknown\n" {
		t.Fatalf("exit %d, stdout %q", code, env.out)
	}
	// The rule refuses exactly that pair.
	if err := preRerunDecision(tokenNone, releaseUnknown); err == nil {
		t.Fatal("the record-Absent, release-Unknown pair was allowed")
	}
	for _, tok := range []statusToken{tokenVerified, tokenNone, tokenUnknown, tokenAbandoned, tokenInvalid} {
		for _, rel := range []releaseState{releaseUnknown, releaseNone, releasePublishedSame, releaseDraft, releaseOther} {
			err := preRerunDecision(tok, rel)
			allowed := tok == tokenNone && rel == releaseNone
			if allowed != (err == nil) {
				t.Fatalf("token %s release %v: err %v", tok, rel, err)
			}
			if err != nil && !strings.HasPrefix(err.Error(), "fork-release: pre-rerun: ") {
				t.Fatalf("refusal %q", err)
			}
		}
	}
	if preRerunDecision(statusToken(""), releaseNone) == nil || preRerunDecision(statusToken("none "), releaseNone) == nil {
		t.Fatal("a token that is not one of the five was allowed")
	}
}

// Two releases at one commit are told apart by tag: each reads its own record,
// its own release and its own run, and prints its own answer.
func TestStatusTwoReleasesAtOneCommit(t *testing.T) {
	const otherTag = "v5.1.0-ezop.2"
	// mk is the world seen by a status of tag, where recTag has run 123456789 and
	// otherTag has run 999 in its record; relRun is the run otherTag's release
	// notes name, and statuses are the runs of the workflow at the commit.
	mk := func(tag, otherRelRun string, statuses ...string) stCase {
		c := stVerified()
		c.record = func() *fakeAPI {
			lines := setKey(setKey(setKey(validVerified(), "tag", tag), "proton-version", strings.TrimPrefix(tag, "v")), "commit", recCommit)
			if tag == otherTag {
				lines = setKey(lines, "run-id", "999")
			}
			data := renderRecord(lines)
			return lkWorld([]lkEntry{lkBlobEntry("README.md", lkReadme), lkBlobEntry(tag+".md", data)},
				map[string][]byte{blobID(data): data, blobID(lkReadme): lkReadme})
		}
		c.releases = []string{
			relEntry(recTag, false, stBody(recCommit, stRun)),
			relEntry(otherTag, false, stBody(recCommit, otherRelRun)),
		}
		c.runs = stRuns(recCommit, statuses...)
		c.tags = map[string]string{recTag: recCommit, otherTag: recCommit}
		return c
	}
	status := func(t *testing.T, c stCase, tag string) (int, string) {
		env := c.build(t)
		code := run([]string{"verify", "--status", tag}, env.d)
		return code, env.out.String()
	}
	for _, tag := range []string{recTag, otherTag} {
		if code, out := status(t, mk(tag, "999/1", "completed", "completed"), tag); code != 0 || out != statusLines("verified", "Published-Same") {
			t.Fatalf("%s: exit %d, stdout %q", tag, code, out)
		}
	}
	// The other tag's release was rewritten: it reads invalid, the first still verified.
	if code, out := status(t, mk(otherTag, "999/2", "completed", "completed"), otherTag); code != 6 || out != statusLines("invalid", "Published-Same") {
		t.Fatalf("B rewritten: exit %d, stdout %q", code, out)
	}
	if code, out := status(t, mk(recTag, "999/2", "completed", "completed"), recTag); code != 0 || out != statusLines("verified", "Published-Same") {
		t.Fatalf("A beside a rewritten B: exit %d, stdout %q", code, out)
	}
	// A run of the other tag in progress at the commit is not over for either.
	for _, tag := range []string{recTag, otherTag} {
		if code, out := status(t, mk(tag, "999/1", "completed", "in_progress"), tag); code != 4 || out != statusLines("unknown", "Published-Same") {
			t.Fatalf("%s beside a run in progress: exit %d, stdout %q", tag, code, out)
		}
	}
}

// ---- helper identity and the command line ----

func TestStatusIdentityRefusals(t *testing.T) {
	cases := map[string]struct {
		set   func(e *stEnv)
		check string
	}{
		"no build information": {func(e *stEnv) { e.d.build = func() (*debug.BuildInfo, bool) { return nil, false } }, "build"},
		"a modified tree": {func(e *stEnv) {
			e.d.build = func() (*debug.BuildInfo, bool) {
				return settings("vcs.revision", helperSHA, "vcs.modified", "true"), true
			}
		}, "build"},
		"an unlanded commit":         {func(e *stEnv) { e.git.ancestry = 1 }, "ancestry"},
		"an undecided ancestry":      {func(e *stEnv) { e.git.ancestry = 128 }, "ancestry"},
		"a failed ancestry":          {func(e *stEnv) { e.git.ancestryErr = errors.New("boom " + secretURL) }, "ancestry"},
		"a failed fetch":             {func(e *stEnv) { e.git.fetchErr = errors.New("boom " + secretURL) }, "fetch"},
		"a wrong fetch URL":          {func(e *stEnv) { e.git.fetchURLs = []string{secretURL} }, "origin"},
		"a wrong push URL":           {func(e *stEnv) { e.git.pushURLs = []string{secretURL} }, "origin"},
		"one bad push URL of many":   {func(e *stEnv) { e.git.pushURLs = []string{canonicalOrigins[0], secretURL} }, "origin"},
		"origin that cannot be read": {func(e *stEnv) { e.git.urlsErr = errors.New("No such remote " + secretURL) }, "origin"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := stVerified().build(t)
			c.set(&env)
			if code := run([]string{"verify", "--status", recTag}, env.d); code != 1 {
				t.Fatalf("exit %d; stdout %q stderr %q", code, env.out, env.errb)
			}
			line := env.errb.String()
			if !strings.HasPrefix(line, "fork-release: "+c.check+": ") || strings.Count(line, "\n") != 1 {
				t.Fatalf("stderr %q", line)
			}
			if env.out.Len() != 0 || strings.Contains(line, "SECRET-URL-MARKER") {
				t.Fatalf("stdout %q stderr %q", env.out, line)
			}
			// Before any network read of the API or of the tags.
			if len(*env.ev) != 0 {
				t.Fatalf("reads after a refused identity: %v", *env.ev)
			}
		})
	}
	t.Run("the healthy control", func(t *testing.T) {
		env := stVerified().build(t)
		if code := run([]string{"verify", "--status", recTag}, env.d); code != 0 {
			t.Fatalf("exit %d; stderr %q", code, env.errb)
		}
	})
	t.Run("the ancestry is asked of the fetched tip", func(t *testing.T) {
		env := stVerified().build(t)
		run([]string{"verify", "--status", recTag}, env.d)
		want := []string{"urls push=false", "urls push=true", "fetch", "ancestor " + helperSHA + " " + ezopTipSHA}
		if len(env.git.calls) < 4 || !slices.Equal(env.git.calls[:4], want) {
			t.Fatalf("calls %v", env.git.calls)
		}
	})
}

func TestStatusRefusesWhatIsNotAReleaseTagBeforeAnyRead(t *testing.T) {
	for _, tag := range []string{"v5.1.0", "5.1.0-ezop.1", "v5.1.0-ezop.0", "v5.1.0-ezop.01", "--help", "-x", "", "V5.1.0-ezop.1", "v5.1.0-ezop.1 ", "../v5.1.0-ezop.1"} {
		env := stVerified().build(t)
		code := run([]string{"verify", "--status", tag}, env.d)
		if code != 1 {
			t.Fatalf("%q: exit %d, stdout %q stderr %q", tag, code, env.out, env.errb)
		}
		if !strings.HasPrefix(env.errb.String(), "fork-release: status: ") || strings.Count(env.errb.String(), "\n") != 1 || env.out.Len() != 0 {
			t.Fatalf("%q: stdout %q stderr %q", tag, env.out, env.errb)
		}
		if len(env.git.calls) != 0 || len(*env.ev) != 0 {
			t.Fatalf("%q: git %v, reads %v", tag, env.git.calls, *env.ev)
		}
	}
}

func TestVerifyUsage(t *testing.T) {
	const usage = "fork-release: usage: fork-release verify --status TAG | verify TAG GATES\n"
	for name, args := range map[string][]string{
		"a bare verify":          {"verify"},
		"--status without a tag": {"verify", "--status"},
		"--status with two tags": {"verify", "--status", "A", "B"},
		"a single tag":           {"verify", recTag},
		"three words":            {"verify", "A", "B", "C"},
		"--status last":          {"verify", recTag, "--status"},
		"an unknown flag":        {"verify", "--confirm", recTag},
		"--status twice":         {"verify", "--status", "--status", recTag},
	} {
		t.Run(name, func(t *testing.T) {
			env := stVerified().build(t)
			if code := run(args, env.d); code != 2 {
				t.Fatalf("exit %d", code)
			}
			if env.errb.String() != usage || env.out.Len() != 0 {
				t.Fatalf("stdout %q stderr %q", env.out, env.errb)
			}
			if len(env.git.calls) != 0 || len(*env.ev) != 0 {
				t.Fatalf("a read after a usage error: git %v, reads %v", env.git.calls, *env.ev)
			}
		})
	}
}

func TestStatusNeedsAGitHubReader(t *testing.T) {
	env := stVerified().build(t)
	env.d.gh = nil
	if code := run([]string{"verify", "--status", recTag}, env.d); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(env.errb.String(), "fork-release: status: ") || env.out.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", env.out, env.errb)
	}
}

func TestStatusOutputFailure(t *testing.T) {
	env := stVerified().build(t)
	env.d.stdout = errWriter{}
	if code := run([]string{"verify", "--status", recTag}, env.d); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(env.errb.String(), "fork-release: output: ") {
		t.Fatalf("stderr %q", env.errb)
	}
}

// The status table is the one mapping: the pure decision gives the same token
// as the command, for the corner a fixture cannot reach (no listing at all).
func TestDecideStatusFailsClosedWithoutAListing(t *testing.T) {
	in := statusInputs{Record: recordAbsent, Tag: tagRead{State: tagPresent, Peel: stPeel}}
	if tok := decideStatus(in); tok != tokenUnknown {
		t.Fatalf("Absent with a peel and no listing read is %s", tok)
	}
	in = statusInputs{Record: recordPresent, Valid: true, Verified: true, RecordCommit: recCommit, RecordRun: stRun,
		Release: releaseLookup{State: releasePublishedSame, RunValue: stRun, RunValid: true},
		Tag:     tagRead{State: tagPresent, Peel: recCommit}}
	if tok := decideStatus(in); tok != tokenUnknown {
		t.Fatalf("verified with no listing read is %s", tok)
	}
	in.Listing = runListing{Runs: stRunsParsed(recCommit, "completed")}
	in.ListingRead = true
	if tok := decideStatus(in); tok != tokenVerified {
		t.Fatalf("verified with a clean listing is %s", tok)
	}
}

func stRunsParsed(e string, statuses ...string) []workflowRun {
	var out []workflowRun
	for i, s := range statuses {
		out = append(out, workflowRun{ID: int64(300 + i), Status: s, HeadSHA: e, Event: "push"})
	}
	return out
}
