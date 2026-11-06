package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for the gh program: the exec adapter
// is pointed at this binary with FORK_RELEASE_FAKE_GH set, and no shell, no gh
// and no network are involved.
func TestMain(m *testing.M) {
	if os.Getenv("FORK_RELEASE_FAKE_GH") != "" {
		fakeGH()
		return
	}
	os.Exit(m.Run())
}

// fakeGH records its argv and two environment variables as one JSON line in
// FORK_RELEASE_FAKE_LOG, then prints FORK_RELEASE_FAKE_OUT and exits with
// FORK_RELEASE_FAKE_EXIT. FORK_RELEASE_FAKE_BYTES adds that many bytes of output
// and FORK_RELEASE_FAKE_SLEEP delays the answer.
func fakeGH() {
	line, _ := json.Marshal(map[string]any{
		"argv":    os.Args[1:],
		"gh_repo": os.Getenv("GH_REPO"),
		"gh_host": os.Getenv("GH_HOST"),
	})
	if log := os.Getenv("FORK_RELEASE_FAKE_LOG"); log != "" {
		f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.Write(append(line, '\n'))
			_ = f.Close()
		}
	}
	if d := os.Getenv("FORK_RELEASE_FAKE_SLEEP"); d != "" {
		if dur, err := time.ParseDuration(d); err == nil {
			time.Sleep(dur)
		}
	}
	fmt.Print(os.Getenv("FORK_RELEASE_FAKE_OUT"))
	if n, err := strconv.Atoi(os.Getenv("FORK_RELEASE_FAKE_BYTES")); err == nil {
		_, _ = os.Stdout.Write([]byte(strings.Repeat("x", n)))
	}
	fmt.Fprint(os.Stderr, "SECRET-DIAGNOSTIC https://example.invalid/token\n")
	if os.Getenv("FORK_RELEASE_FAKE_EXIT") != "" {
		os.Exit(3)
	}
}

const (
	lkTip      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	lkRoot     = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	lkFork     = "cccccccccccccccccccccccccccccccccccccccc"
	lkVer      = "dddddddddddddddddddddddddddddddddddddddd"
	lkOtherSHA = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

var lkReadme = []byte("# README\n")

type lkEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

func lkBlobEntry(path string, data []byte) lkEntry {
	return lkEntry{path, "100644", "blob", blobID(data)}
}

func lkTreeJSON(sha string, truncated bool, entries ...lkEntry) string {
	b, _ := json.Marshal(map[string]any{"sha": sha, "url": "https://example.invalid", "tree": entries, "truncated": truncated})
	return string(b)
}

func lkBlobJSON(sha string, data []byte) string {
	b, _ := json.Marshal(map[string]any{
		"sha": sha, "size": len(data), "encoding": "base64", "node_id": "x",
		"content": wrap60(base64.StdEncoding.EncodeToString(data)),
	})
	return string(b)
}

// wrap60 puts a line feed every 60 characters, as the API does.
func wrap60(s string) string {
	var b strings.Builder
	for len(s) > 60 {
		b.WriteString(s[:60] + "\n")
		s = s[60:]
	}
	return b.String() + s + "\n"
}

// fakeAPI serves canned bodies by endpoint and records every call.
type fakeAPI struct {
	resp  map[string]string
	fail  map[string]bool
	calls []string
}

func (f *fakeAPI) Get(endpoint string) ([]byte, error) {
	f.calls = append(f.calls, endpoint)
	if f.fail[endpoint] {
		return nil, errors.New("gh failed")
	}
	body, ok := f.resp[endpoint]
	if !ok {
		return nil, errors.New("gh failed")
	}
	return []byte(body), nil
}

const (
	epRef  = "git/ref/heads/ezop"
	epTip  = "git/commits/" + lkTip
	epRoot = "git/trees/" + lkRoot
	epFork = "git/trees/" + lkFork
	epVer  = "git/trees/" + lkVer
)

// lkWorld is the documented answers for a tip whose verification directory
// holds the README and the given entries; blobs maps an entry's id to its bytes.
func lkWorld(verEntries []lkEntry, blobs map[string][]byte) *fakeAPI {
	f := &fakeAPI{resp: map[string]string{
		epRef:  `{"ref":"refs/heads/ezop","node_id":"x","url":"u","object":{"type":"commit","sha":"` + lkTip + `","url":"u"}}`,
		epTip:  `{"sha":"` + lkTip + `","tree":{"sha":"` + lkRoot + `","url":"u"},"message":"m"}`,
		epRoot: lkTreeJSON(lkRoot, false, lkEntry{"README.md", "100644", "blob", lkOtherSHA}, lkEntry{"fork", "040000", "tree", lkFork}),
		epFork: lkTreeJSON(lkFork, false, lkEntry{"verification", "040000", "tree", lkVer}),
		epVer:  lkTreeJSON(lkVer, false, verEntries...),
	}, fail: map[string]bool{}}
	for sha, data := range blobs {
		f.resp["git/blobs/"+sha] = lkBlobJSON(sha, data)
	}
	return f
}

func lkRecordWorld() (*fakeAPI, []byte) {
	rec := recordFor(recTag, false)
	return lkWorld([]lkEntry{
		lkBlobEntry("README.md", lkReadme),
		lkBlobEntry(recTag+".md", rec),
	}, map[string][]byte{blobID(rec): rec, blobID(lkReadme): lkReadme}), rec
}

func TestLookupRecordPresent(t *testing.T) {
	api, rec := lkRecordWorld()
	got := lookupRecord(api, recTag)
	if got.State != recordPresent || got.Commit != lkTip || string(got.Data) != string(rec) {
		t.Fatalf("lookup = %+v", got)
	}
	want := []string{epRef, epTip, epRoot, epFork, epVer, "git/blobs/" + blobID(rec)}
	if !slices.Equal(api.calls, want) {
		t.Fatalf("calls = %v, want %v", api.calls, want)
	}
	for _, c := range api.calls {
		if strings.ContainsAny(c, "?&") {
			t.Fatalf("a call carries a query: %q", c)
		}
	}
}

func TestLookupRecordPresentWithoutReadme(t *testing.T) {
	rec := recordFor(recTag, false)
	api := lkWorld([]lkEntry{lkBlobEntry(recTag+".md", rec)}, map[string][]byte{blobID(rec): rec})
	if got := lookupRecord(api, recTag); got.State != recordPresent {
		t.Fatalf("lookup = %+v", got)
	}
}

func TestLookupRecordPresentExecutable(t *testing.T) {
	rec := recordFor(recTag, false)
	e := lkBlobEntry(recTag+".md", rec)
	e.Mode = "100755"
	api := lkWorld([]lkEntry{e}, map[string][]byte{blobID(rec): rec})
	if got := lookupRecord(api, recTag); got.State != recordPresent {
		t.Fatalf("lookup = %+v", got)
	}
}

func TestLookupRecordAbsent(t *testing.T) {
	api := lkWorld([]lkEntry{
		lkBlobEntry("README.md", lkReadme),
		lkBlobEntry("v5.1.0-ezop.2.md", []byte("other")),
	}, nil)
	got := lookupRecord(api, recTag)
	if got.State != recordAbsent || got.Commit != lkTip || got.Data != nil {
		t.Fatalf("lookup = %+v", got)
	}
	if want := []string{epRef, epTip, epRoot, epFork, epVer}; !slices.Equal(api.calls, want) {
		t.Fatalf("calls = %v, want %v", api.calls, want)
	}
}

func TestLookupRecordNotFile(t *testing.T) {
	for _, e := range []lkEntry{
		{recTag + ".md", "040000", "tree", lkOtherSHA},
		{recTag + ".md", "120000", "blob", lkOtherSHA},
		{recTag + ".md", "160000", "commit", lkOtherSHA},
	} {
		t.Run(e.Type+" "+e.Mode, func(t *testing.T) {
			api := lkWorld([]lkEntry{lkBlobEntry("README.md", lkReadme), e}, nil)
			got := lookupRecord(api, recTag)
			if got.State != recordNotFile || got.Commit != lkTip {
				t.Fatalf("lookup = %+v", got)
			}
			if len(api.calls) != 5 {
				t.Fatalf("calls = %v", api.calls)
			}
		})
	}
	t.Run("without a README", func(t *testing.T) {
		api := lkWorld([]lkEntry{{recTag + ".md", "040000", "tree", lkOtherSHA}}, nil)
		if got := lookupRecord(api, recTag); got.State != recordNotFile {
			t.Fatalf("lookup = %+v", got)
		}
	})
}

func TestLookupRecordUnknown(t *testing.T) {
	rec := recordFor(recTag, false)
	recEntry := lkBlobEntry(recTag+".md", rec)
	readme := lkBlobEntry("README.md", lkReadme)
	blobEp := "git/blobs/" + blobID(rec)

	// base builds the healthy Present world for a mutation.
	base := func() *fakeAPI {
		return lkWorld([]lkEntry{readme, recEntry}, map[string][]byte{blobID(rec): rec})
	}
	absent := func(entries ...lkEntry) *fakeAPI { return lkWorld(entries, nil) }
	ver := func(entries ...lkEntry) string { return lkTreeJSON(lkVer, false, entries...) }

	cases := []struct {
		name   string
		setup  func() *fakeAPI
		reason string
		commit string // the commit expected with the answer
	}{
		// The tip.
		{"tip call fails", func() *fakeAPI { a := base(); a.fail[epRef] = true; return a }, "tip: call failed", ""},
		{"tip is not json", func() *fakeAPI { a := base(); a.resp[epRef] = "not json"; return a }, "tip: malformed response", ""},
		{"tip is empty", func() *fakeAPI { a := base(); a.resp[epRef] = ""; return a }, "tip: malformed response", ""},
		{"tip is an array", func() *fakeAPI { a := base(); a.resp[epRef] = "[]"; return a }, "tip: malformed response", ""},
		{"tip has trailing data", func() *fakeAPI { a := base(); a.resp[epRef] += `{"x":1}`; return a }, "tip: malformed response", ""},
		{"tip is an annotated tag", func() *fakeAPI {
			a := base()
			a.resp[epRef] = `{"ref":"refs/heads/ezop","object":{"type":"tag","sha":"` + lkTip + `"}}`
			return a
		}, "tip: not a commit", ""},
		{"tip type missing", func() *fakeAPI {
			a := base()
			a.resp[epRef] = `{"ref":"refs/heads/ezop","object":{"sha":"` + lkTip + `"}}`
			return a
		}, "tip: not a commit", ""},
		{"tip sha is short", func() *fakeAPI {
			a := base()
			a.resp[epRef] = `{"ref":"refs/heads/ezop","object":{"type":"commit","sha":"abc"}}`
			return a
		}, "tip: malformed response", ""},
		{"tip sha is uppercase", func() *fakeAPI {
			a := base()
			a.resp[epRef] = `{"ref":"refs/heads/ezop","object":{"type":"commit","sha":"` + strings.ToUpper(lkTip) + `"}}`
			return a
		}, "tip: malformed response", ""},
		{"tip names another ref", func() *fakeAPI {
			a := base()
			a.resp[epRef] = `{"ref":"refs/heads/main","object":{"type":"commit","sha":"` + lkTip + `"}}`
			return a
		}, "tip: id mismatch", ""},

		// The commit.
		{"commit call fails", func() *fakeAPI { a := base(); a.fail[epTip] = true; return a }, "commit: call failed", lkTip},
		{"commit is not json", func() *fakeAPI { a := base(); a.resp[epTip] = "<html>"; return a }, "commit: malformed response", lkTip},
		{"commit has another id", func() *fakeAPI {
			a := base()
			a.resp[epTip] = `{"sha":"` + lkOtherSHA + `","tree":{"sha":"` + lkRoot + `"}}`
			return a
		}, "commit: id mismatch", lkTip},
		{"commit has no tree", func() *fakeAPI { a := base(); a.resp[epTip] = `{"sha":"` + lkTip + `"}`; return a }, "commit: malformed response", lkTip},
		{"commit tree id is short", func() *fakeAPI {
			a := base()
			a.resp[epTip] = `{"sha":"` + lkTip + `","tree":{"sha":"abc"}}`
			return a
		}, "commit: malformed response", lkTip},

		// The three trees: failure, malformed, another id, truncated, repeated entry.
		{"root call fails", func() *fakeAPI { a := base(); a.fail[epRoot] = true; return a }, "root tree: call failed", lkTip},
		{"fork call fails", func() *fakeAPI { a := base(); a.fail[epFork] = true; return a }, "fork tree: call failed", lkTip},
		{"verification call fails", func() *fakeAPI { a := base(); a.fail[epVer] = true; return a }, "verification tree: call failed", lkTip},
		{"root is not json", func() *fakeAPI { a := base(); a.resp[epRoot] = "{"; return a }, "root tree: malformed response", lkTip},
		{"fork is not json", func() *fakeAPI { a := base(); a.resp[epFork] = "null"; return a }, "fork tree: malformed response", lkTip},
		{"verification is not json", func() *fakeAPI { a := base(); a.resp[epVer] = `{"tree":"x"}`; return a }, "verification tree: malformed response", lkTip},
		{"root has another id", func() *fakeAPI {
			a := base()
			a.resp[epRoot] = lkTreeJSON(lkOtherSHA, false, lkEntry{"fork", "040000", "tree", lkFork})
			return a
		}, "root tree: id mismatch", lkTip},
		{"fork has another id", func() *fakeAPI {
			a := base()
			a.resp[epFork] = lkTreeJSON(lkOtherSHA, false, lkEntry{"verification", "040000", "tree", lkVer})
			return a
		}, "fork tree: id mismatch", lkTip},
		{"verification has another id", func() *fakeAPI {
			a := base()
			a.resp[epVer] = lkTreeJSON(lkOtherSHA, false, readme, recEntry)
			return a
		}, "verification tree: id mismatch", lkTip},
		{"root truncated", func() *fakeAPI {
			a := base()
			a.resp[epRoot] = lkTreeJSON(lkRoot, true, lkEntry{"fork", "040000", "tree", lkFork})
			return a
		}, "root tree: truncated", lkTip},
		{"fork truncated", func() *fakeAPI {
			a := base()
			a.resp[epFork] = lkTreeJSON(lkFork, true, lkEntry{"verification", "040000", "tree", lkVer})
			return a
		}, "fork tree: truncated", lkTip},
		{"verification truncated", func() *fakeAPI {
			a := base()
			a.resp[epVer] = lkTreeJSON(lkVer, true, readme, recEntry)
			return a
		}, "verification tree: truncated", lkTip},
		{"verification truncated without the entry", func() *fakeAPI {
			a := absent(readme)
			a.resp[epVer] = lkTreeJSON(lkVer, true, readme)
			return a
		}, "verification tree: truncated", lkTip},
		{"root repeats an entry", func() *fakeAPI {
			a := base()
			a.resp[epRoot] = lkTreeJSON(lkRoot, false, lkEntry{"fork", "040000", "tree", lkFork}, lkEntry{"fork", "040000", "tree", lkFork})
			return a
		}, "root tree: repeated entry", lkTip},
		{"fork repeats an entry", func() *fakeAPI {
			a := base()
			a.resp[epFork] = lkTreeJSON(lkFork, false, lkEntry{"verification", "040000", "tree", lkVer}, lkEntry{"verification", "040000", "tree", lkOtherSHA})
			return a
		}, "fork tree: repeated entry", lkTip},
		{"verification repeats the record", func() *fakeAPI {
			a := base()
			a.resp[epVer] = ver(readme, recEntry, recEntry)
			return a
		}, "verification tree: repeated entry", lkTip},
		{"verification repeats the README", func() *fakeAPI {
			a := base()
			a.resp[epVer] = ver(readme, readme)
			return a
		}, "verification tree: repeated entry", lkTip},
		{"an entry name with a slash", func() *fakeAPI {
			a := base()
			a.resp[epVer] = ver(readme, recEntry, lkEntry{"a/b", "100644", "blob", lkOtherSHA})
			return a
		}, "verification tree: malformed response", lkTip},
		{"an entry with an empty name", func() *fakeAPI {
			a := base()
			a.resp[epVer] = ver(readme, recEntry, lkEntry{"", "100644", "blob", lkOtherSHA})
			return a
		}, "verification tree: malformed response", lkTip},

		// The directories and the README.
		{"fork directory missing", func() *fakeAPI {
			a := base()
			a.resp[epRoot] = lkTreeJSON(lkRoot, false, lkEntry{"README.md", "100644", "blob", lkOtherSHA})
			return a
		}, "root tree: fork missing", lkTip},
		{"fork is a file", func() *fakeAPI {
			a := base()
			a.resp[epRoot] = lkTreeJSON(lkRoot, false, lkEntry{"fork", "100644", "blob", lkFork})
			return a
		}, "root tree: fork is not a directory", lkTip},
		{"fork is a symlink", func() *fakeAPI {
			a := base()
			a.resp[epRoot] = lkTreeJSON(lkRoot, false, lkEntry{"fork", "120000", "blob", lkFork})
			return a
		}, "root tree: fork is not a directory", lkTip},
		{"verification directory missing", func() *fakeAPI {
			a := base()
			a.resp[epFork] = lkTreeJSON(lkFork, false, lkEntry{"other", "040000", "tree", lkOtherSHA})
			return a
		}, "fork tree: verification missing", lkTip},
		{"verification is a submodule", func() *fakeAPI {
			a := base()
			a.resp[epFork] = lkTreeJSON(lkFork, false, lkEntry{"verification", "160000", "commit", lkVer})
			return a
		}, "fork tree: verification is not a directory", lkTip},
		{"README missing and no record", func() *fakeAPI { return absent(lkBlobEntry("v5.1.0-ezop.2.md", []byte("x"))) },
			"verification tree: README missing", lkTip},
		{"README is a directory", func() *fakeAPI {
			return absent(lkEntry{"README.md", "040000", "tree", lkOtherSHA})
		}, "verification tree: README is not a regular file", lkTip},
		{"README is a symlink", func() *fakeAPI {
			return absent(lkEntry{"README.md", "120000", "blob", lkOtherSHA})
		}, "verification tree: README is not a regular file", lkTip},
		{"README is a submodule", func() *fakeAPI {
			return absent(lkEntry{"README.md", "160000", "commit", lkOtherSHA})
		}, "verification tree: README is not a regular file", lkTip},

		// The entry.
		{"record entry of an unrecognized kind", func() *fakeAPI {
			return absent(readme, lkEntry{recTag + ".md", "100664", "blob", lkOtherSHA})
		}, "verification tree: unrecognized entry kind", lkTip},
		{"record entry with a mismatched type", func() *fakeAPI {
			return absent(readme, lkEntry{recTag + ".md", "100644", "tree", lkOtherSHA})
		}, "verification tree: unrecognized entry kind", lkTip},
		{"record entry id is malformed", func() *fakeAPI {
			return absent(readme, lkEntry{recTag + ".md", "100644", "blob", "xyz"})
		}, "verification tree: malformed response", lkTip},

		// The blob.
		{"blob call fails", func() *fakeAPI { a := base(); a.fail[blobEp] = true; return a }, "record blob: call failed", lkTip},
		{"blob is not json", func() *fakeAPI { a := base(); a.resp[blobEp] = "oops"; return a }, "record blob: malformed response", lkTip},
		{"blob names another id", func() *fakeAPI {
			a := base()
			a.resp[blobEp] = lkBlobJSON(lkOtherSHA, rec)
			return a
		}, "record blob: id mismatch", lkTip},
		{"blob encoding is utf-8", func() *fakeAPI {
			a := base()
			a.resp[blobEp] = `{"sha":"` + blobID(rec) + `","size":` + fmt.Sprint(len(rec)) + `,"encoding":"utf-8","content":"x"}`
			return a
		}, "record blob: bad encoding", lkTip},
		{"blob content is not base64", func() *fakeAPI {
			a := base()
			a.resp[blobEp] = `{"sha":"` + blobID(rec) + `","size":` + fmt.Sprint(len(rec)) + `,"encoding":"base64","content":"!!!!"}`
			return a
		}, "record blob: bad encoding", lkTip},
		{"blob size is wrong", func() *fakeAPI {
			a := base()
			a.resp[blobEp] = `{"sha":"` + blobID(rec) + `","size":3,"encoding":"base64","content":"` + base64.StdEncoding.EncodeToString(rec) + `"}`
			return a
		}, "record blob: size mismatch", lkTip},
		{"blob size is missing", func() *fakeAPI {
			a := base()
			a.resp[blobEp] = `{"sha":"` + blobID(rec) + `","encoding":"base64","content":"` + base64.StdEncoding.EncodeToString(rec) + `"}`
			return a
		}, "record blob: malformed response", lkTip},
		{"blob bytes do not hash to the entry id", func() *fakeAPI {
			other := []byte("different bytes\n")
			a := base()
			a.resp[blobEp] = `{"sha":"` + blobID(rec) + `","size":` + fmt.Sprint(len(other)) + `,"encoding":"base64","content":"` + base64.StdEncoding.EncodeToString(other) + `"}`
			return a
		}, "record blob: hash mismatch", lkTip},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api := c.setup()
			got := lookupRecord(api, recTag)
			if got.State != recordUnknown {
				t.Fatalf("state = %v, want Unknown (%+v)", got.State, got)
			}
			if got.Reason != c.reason {
				t.Fatalf("reason = %q, want %q", got.Reason, c.reason)
			}
			if got.Commit != c.commit {
				t.Fatalf("commit = %q, want %q", got.Commit, c.commit)
			}
			if got.Data != nil {
				t.Fatalf("data returned with Unknown")
			}
			for _, call := range api.calls {
				if strings.ContainsAny(call, "?&") {
					t.Fatalf("a call carries a query: %q", call)
				}
			}
		})
	}
}

func TestLookupRecordRefusesABadTagBeforeAnyCall(t *testing.T) {
	for _, tag := range []string{"", "v5.1.0", "v5.1.0-ezop.0", "../x", "v5.1.0-ezop.1.md", "v5.1.0-ezop.1\n", "v5.1.0-ezop.1/../../x"} {
		api, _ := lkRecordWorld()
		got := lookupRecord(api, tag)
		if got.State != recordUnknown || got.Reason != "tag: not a Release Tag" || len(api.calls) != 0 {
			t.Fatalf("tag %q: lookup = %+v, calls = %v", tag, got, api.calls)
		}
	}
}

func TestLookupRecordMessagesCarryNoRawResponse(t *testing.T) {
	api, _ := lkRecordWorld()
	api.resp[epTip] = "RAWBODY https://example.invalid/secret"
	got := lookupRecord(api, recTag)
	if got.State != recordUnknown || strings.Contains(got.Reason, "RAWBODY") || strings.Contains(got.Reason, "example") {
		t.Fatalf("lookup = %+v", got)
	}
}

func TestLookupRecordIgnoresUnknownFields(t *testing.T) {
	api, rec := lkRecordWorld()
	api.resp[epRef] = `{"ref":"refs/heads/ezop","extra":[1,2],"object":{"type":"commit","sha":"` + lkTip + `","extra":{}}}`
	if got := lookupRecord(api, recTag); got.State != recordPresent || string(got.Data) != string(rec) {
		t.Fatalf("lookup = %+v", got)
	}
}

func TestLookupRecordStateStrings(t *testing.T) {
	want := map[recordState]string{recordUnknown: "Unknown", recordPresent: "Present", recordAbsent: "Absent", recordNotFile: "NotFile"}
	for s, w := range want {
		if s.String() != w {
			t.Fatalf("%d.String() = %q, want %q", s, s.String(), w)
		}
	}
	var zero recordLookup
	if zero.State != recordUnknown {
		t.Fatalf("the zero lookup is %v, not Unknown", zero.State)
	}
}

// ---- the gh adapter ----

func TestGHArgsArePinned(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for endpoint, want := range map[string]string{
		"git/ref/heads/ezop": "repos/Ezopek/proton-cli/git/ref/heads/ezop",
		"git/commits/" + sha: "repos/Ezopek/proton-cli/git/commits/" + sha,
		"git/trees/" + sha:   "repos/Ezopek/proton-cli/git/trees/" + sha,
		"git/blobs/" + sha:   "repos/Ezopek/proton-cli/git/blobs/" + sha,
	} {
		got, err := ghArgs(endpoint)
		if err != nil {
			t.Fatalf("%q: %v", endpoint, err)
		}
		wantArgv := []string{"api", "--method", "GET", "-H", "X-GitHub-Api-Version: 2026-03-10", want}
		if !slices.Equal(got, wantArgv) {
			t.Fatalf("argv = %q, want %q", got, wantArgv)
		}
	}
}

func TestGHArgsRefuseAnUnvalidatedPath(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, endpoint := range []string{
		"", "git/ref/heads/main", "git/ref/heads/ezop/../main", "git/ref/heads/ezop?x=1",
		"git/commits/abc", "git/commits/" + strings.ToUpper(sha), "git/commits/" + sha + "/x",
		"git/trees/" + sha + "?recursive=1", "git/trees/" + sha + "?recursive=true", "git/trees/" + sha + "\n",
		"git/blobs/../" + sha, "/git/commits/" + sha, "repos/Other/repo/git/commits/" + sha,
		"contents/fork/verification/README.md", "git/commits/" + sha + " -X DELETE",
		"git/matching-refs/heads", "git/refs", "git/tags/" + sha,
	} {
		if _, err := ghArgs(endpoint); err == nil {
			t.Fatalf("%q was accepted", endpoint)
		}
	}
}

func fakeGHAdapter(t *testing.T, out string, extraEnv ...string) (execGH, string) {
	t.Helper()
	log := t.TempDir() + "/calls.jsonl"
	env := append([]string{"FORK_RELEASE_FAKE_GH=1", "FORK_RELEASE_FAKE_LOG=" + log, "FORK_RELEASE_FAKE_OUT=" + out}, extraEnv...)
	return execGH{program: os.Args[0], env: env, timeout: 10 * time.Second}, log
}

func readCalls(t *testing.T, log string) []struct {
	Argv   []string `json:"argv"`
	GHRepo string   `json:"gh_repo"`
	GHHost string   `json:"gh_host"`
} {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var out []struct {
		Argv   []string `json:"argv"`
		GHRepo string   `json:"gh_repo"`
		GHHost string   `json:"gh_host"`
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var c struct {
			Argv   []string `json:"argv"`
			GHRepo string   `json:"gh_repo"`
			GHHost string   `json:"gh_host"`
		}
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func TestExecGHRunsTheExactArgv(t *testing.T) {
	g, log := fakeGHAdapter(t, `{"ok":true}`)
	body, err := g.Get(epRef)
	if err != nil || string(body) != `{"ok":true}` {
		t.Fatalf("body = %q, err = %v", body, err)
	}
	calls := readCalls(t, log)
	want := []string{"api", "--method", "GET", "-H", "X-GitHub-Api-Version: 2026-03-10", "repos/Ezopek/proton-cli/git/ref/heads/ezop"}
	if len(calls) != 1 || !slices.Equal(calls[0].Argv, want) {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestExecGHDropsTheRepositoryAndHostOverrides(t *testing.T) {
	g, log := fakeGHAdapter(t, "{}", "GH_REPO=other/repo", "GH_HOST=ghe.example.invalid")
	if _, err := g.Get(epRef); err != nil {
		t.Fatal(err)
	}
	calls := readCalls(t, log)
	if len(calls) != 1 || calls[0].GHRepo != "" || calls[0].GHHost != "" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestExecGHRefusesBeforeRunningGH(t *testing.T) {
	g, log := fakeGHAdapter(t, "{}")
	for _, endpoint := range []string{"git/commits/zzz", "git/trees/" + strings.Repeat("a", 40) + "?recursive=1", ""} {
		if _, err := g.Get(endpoint); err == nil {
			t.Fatalf("%q was accepted", endpoint)
		}
	}
	if calls := readCalls(t, log); len(calls) != 0 {
		t.Fatalf("gh ran: %+v", calls)
	}
}

func TestExecGHErrorsCarryNoOutput(t *testing.T) {
	g, _ := fakeGHAdapter(t, "SECRET-STDOUT", "FORK_RELEASE_FAKE_EXIT=1")
	body, err := g.Get(epRef)
	if err == nil || body != nil {
		t.Fatalf("body = %q, err = %v", body, err)
	}
	for _, leak := range []string{"SECRET", "example.invalid", "token"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("error carries gh output: %v", err)
		}
	}
}

func TestExecGHProgramMissing(t *testing.T) {
	g := execGH{program: "/nonexistent/gh-for-fork-release-test", timeout: time.Second}
	if _, err := g.Get(epRef); err == nil {
		t.Fatal("a missing program was accepted")
	}
}

func TestExecGHTimeout(t *testing.T) {
	g, _ := fakeGHAdapter(t, "{}", "FORK_RELEASE_FAKE_SLEEP=20s")
	g.timeout = 300 * time.Millisecond
	start := time.Now()
	if _, err := g.Get(epRef); err == nil {
		t.Fatal("a call past its timeout was accepted")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("the timeout took %v", time.Since(start))
	}
}

func TestExecGHBoundsTheOutput(t *testing.T) {
	g, _ := fakeGHAdapter(t, "", "FORK_RELEASE_FAKE_BYTES="+strconv.Itoa(maxGHOutput+1))
	if _, err := g.Get(epRef); err == nil {
		t.Fatal("an oversized answer was accepted")
	}
}

func TestLookupRecordOverTheExecAdapter(t *testing.T) {
	// One canned answer for every call is enough to see the whole path run
	// through the adapter: the tip answer is a tip, and is not a commit object.
	g, log := fakeGHAdapter(t, `{"ref":"refs/heads/ezop","object":{"type":"commit","sha":"`+lkTip+`"}}`)
	got := lookupRecord(g, recTag)
	if got.State != recordUnknown || got.Reason != "commit: malformed response" {
		t.Fatalf("lookup = %+v", got)
	}
	calls := readCalls(t, log)
	if len(calls) != 2 || calls[1].Argv[5] != "repos/Ezopek/proton-cli/git/commits/"+lkTip {
		t.Fatalf("calls = %+v", calls)
	}
}
