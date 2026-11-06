package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	relE      = "3333333333333333333333333333333333333333"
	relOther  = "4444444444444444444444444444444444444444"
	relMarker = "SECRET-RELEASE-MARKER"
)

// relEntry renders one entry of the releases list the way the API does, with
// fields the lookup does not read.
func relEntry(tag string, draft bool, body string) string {
	b, _ := json.Marshal(map[string]any{
		"id": 1, "tag_name": tag, "draft": draft, "prerelease": true,
		"name": relMarker, "body": body, "assets": []any{},
		"html_url": "https://example.invalid/" + relMarker,
	})
	return string(b)
}

func relPage(entries ...string) string { return "[" + strings.Join(entries, ",") + "]" }

// relBody is a notes body with the Commit line for sha.
func relBody(sha string) string {
	return "Fork release of the Upstream Base.\n\nCommit: " + sha + "\nRun: 1/1\n"
}

// fakePages serves the pages of the releases list by number, answers an empty
// page past the last one given, and records every endpoint asked for.
type fakePages struct {
	pages map[int]string
	fail  map[int]bool
	// endless answers a non-empty page for every number without one.
	endless bool
	calls   []string
}

func (f *fakePages) Get(endpoint string) ([]byte, error) {
	f.calls = append(f.calls, endpoint)
	num, ok := strings.CutPrefix(endpoint, "releases?per_page=100&page=")
	if !ok {
		return nil, errors.New("gh failed")
	}
	n, err := strconv.Atoi(num)
	if err != nil || f.fail[n] {
		return nil, errors.New("gh failed " + relMarker)
	}
	if page, ok := f.pages[n]; ok {
		return []byte(page), nil
	}
	if f.endless {
		return []byte(relPage(relEntry("v0.0."+num, false, ""))), nil
	}
	return []byte("[]"), nil
}

func pagesOf(pages ...string) *fakePages {
	f := &fakePages{pages: map[int]string{}, fail: map[int]bool{}}
	for i, p := range pages {
		f.pages[i+1] = p
	}
	return f
}

func wantRelease(t *testing.T, got releaseLookup, state releaseState) {
	t.Helper()
	if got.State != state {
		t.Fatalf("state %v (%q), want %v", got.State, got.Reason, state)
	}
	if state == releaseUnknown && got.Reason == "" {
		t.Fatal("an Unknown without a reason")
	}
	if state != releaseUnknown && got.Reason != "" {
		t.Fatalf("a reason %q on a settled state", got.Reason)
	}
	if strings.Contains(got.Reason, relMarker) || strings.Contains(got.Reason, "http") {
		t.Fatalf("the reason quotes an answer: %q", got.Reason)
	}
}

func TestReleaseStateStrings(t *testing.T) {
	for s, want := range map[releaseState]string{
		releaseNone: "None", releasePublishedSame: "Published-Same", releaseDraft: "Draft",
		releaseOther: "Other", releaseUnknown: "Unknown",
	} {
		if s.String() != want {
			t.Errorf("%d prints %q, want %q", s, s.String(), want)
		}
	}
	if releaseState(0) != releaseUnknown {
		t.Fatal("the zero value of a release state must be Unknown")
	}
	if releaseState(99).String() != "Unknown" {
		t.Fatal("an unrecognized state must print as Unknown")
	}
}

func TestCommitLinePrefixIsShared(t *testing.T) {
	if commitLinePrefix != "Commit: " {
		t.Fatalf("prefix %q", commitLinePrefix)
	}
}

func TestParseCommitLine(t *testing.T) {
	good := strings.Repeat("ab", 20)
	for name, tc := range map[string]struct {
		body string
		sha  string
		ok   bool
	}{
		"one line":                        {"Commit: " + good, good, true},
		"line feed ending":                {"Commit: " + good + "\n", good, true},
		"between other lines":             {"a\nCommit: " + good + "\nRun: 1/1\n", good, true},
		"CRLF":                            {"a\r\nCommit: " + good + "\r\nRun: 1/1\r\n", good, true},
		"CRLF at the end of the body":     {"Commit: " + good + "\r\n", good, true},
		"none":                            {"Run: 1/1\n", "", false},
		"empty body":                      {"", "", false},
		"repeated, same":                  {"Commit: " + good + "\nCommit: " + good + "\n", "", false},
		"repeated, different":             {"Commit: " + good + "\nCommit: " + strings.Repeat("cd", 20) + "\n", "", false},
		"short":                           {"Commit: " + good[:39] + "\n", "", false},
		"long":                            {"Commit: " + good + "a\n", "", false},
		"upper case hex":                  {"Commit: " + strings.ToUpper(good) + "\n", "", false},
		"not hex":                         {"Commit: " + strings.Repeat("zz", 20) + "\n", "", false},
		"trailing text":                   {"Commit: " + good + " more\n", "", false},
		"trailing space":                  {"Commit: " + good + " \n", "", false},
		"two carriage returns":            {"Commit: " + good + "\r\r\n", "", false},
		"lower case key":                  {"commit: " + good + "\n", "", false},
		"no space":                        {"Commit:" + good + "\n", "", false},
		"indented":                        {" Commit: " + good + "\n", "", false},
		"quoted inside a line":            {"see Commit: " + good + "\n", "", false},
		"empty value":                     {"Commit: \n", "", false},
		"a malformed line and a good one": {"Commit: nope\nCommit: " + good + "\n", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			sha, ok := parseCommitLine(tc.body)
			if sha != tc.sha || ok != tc.ok {
				t.Fatalf("got (%q, %v), want (%q, %v)", sha, ok, tc.sha, tc.ok)
			}
		})
	}
}

func TestLookupReleaseNone(t *testing.T) {
	const tag = "v5.1.0-ezop.1"
	t.Run("an empty list", func(t *testing.T) {
		f := pagesOf("[]")
		wantRelease(t, lookupRelease(f, tag, relE), releaseNone)
		if want := []string{"releases?per_page=100&page=1"}; !slices.Equal(f.calls, want) {
			t.Fatalf("calls %v", f.calls)
		}
	})
	t.Run("other tags only, over two pages", func(t *testing.T) {
		f := pagesOf(
			relPage(relEntry("v5.1.0-ezop.2", false, relBody(relE)), relEntry("v5.1.0", false, "")),
			relPage(relEntry("v5.1.0-ezop.10", true, "")),
		)
		wantRelease(t, lookupRelease(f, tag, relE), releaseNone)
		want := []string{"releases?per_page=100&page=1", "releases?per_page=100&page=2", "releases?per_page=100&page=3"}
		if !slices.Equal(f.calls, want) {
			t.Fatalf("calls %v", f.calls)
		}
	})
	t.Run("a tag that differs only in case or by a suffix is not the tag", func(t *testing.T) {
		f := pagesOf(relPage(relEntry("V5.1.0-ezop.1", false, ""), relEntry(tag+" ", false, ""), relEntry("x"+tag, false, "")))
		wantRelease(t, lookupRelease(f, tag, relE), releaseNone)
	})
	t.Run("a long list that ends on an empty page", func(t *testing.T) {
		f := pagesOf(relPage(relEntry("v1.0.0", false, "")), relPage(relEntry("v1.0.1", false, "")), relPage(relEntry("v1.0.2", false, "")))
		wantRelease(t, lookupRelease(f, tag, relE), releaseNone)
		if len(f.calls) != 4 {
			t.Fatalf("calls %v", f.calls)
		}
	})
}

func TestLookupReleasePublishedSame(t *testing.T) {
	const tag = "v5.1.0-ezop.1"
	for name, body := range map[string]string{
		"line feeds":  relBody(relE),
		"CRLF":        strings.ReplaceAll(relBody(relE), "\n", "\r\n"),
		"only a line": "Commit: " + relE,
	} {
		t.Run(name, func(t *testing.T) {
			wantRelease(t, lookupRelease(pagesOf(relPage(relEntry(tag, false, body))), tag, relE), releasePublishedSame)
		})
	}
	t.Run("found on page 3, other tags around it", func(t *testing.T) {
		f := pagesOf(
			relPage(relEntry("v5.1.0-ezop.2", false, "")),
			relPage(relEntry("v5.1.0", false, "")),
			relPage(relEntry("v4.0.0", false, ""), relEntry(tag, false, relBody(relE))),
		)
		wantRelease(t, lookupRelease(f, tag, relE), releasePublishedSame)
		if len(f.calls) != 4 {
			t.Fatalf("the list was not read to its end: %v", f.calls)
		}
	})
	t.Run("the pre-release flag does not matter", func(t *testing.T) {
		entry := strings.Replace(relEntry(tag, false, relBody(relE)), `"prerelease":true`, `"prerelease":false`, 1)
		wantRelease(t, lookupRelease(pagesOf(relPage(entry)), tag, relE), releasePublishedSame)
	})
}

func TestLookupReleaseDraft(t *testing.T) {
	const tag = "v5.1.0-ezop.1"
	t.Run("a draft, whatever its body", func(t *testing.T) {
		for _, body := range []string{"", relBody(relE), relBody(relOther), "Commit: nope"} {
			wantRelease(t, lookupRelease(pagesOf(relPage(relEntry(tag, true, body))), tag, relE), releaseDraft)
		}
	})
	t.Run("a draft on page 3", func(t *testing.T) {
		f := pagesOf(relPage(relEntry("v1.0.0", false, "")), relPage(relEntry("v1.0.1", false, "")), relPage(relEntry(tag, true, "")))
		wantRelease(t, lookupRelease(f, tag, relE), releaseDraft)
	})
}

func TestLookupReleaseOther(t *testing.T) {
	const tag = "v5.1.0-ezop.1"
	for name, body := range map[string]string{
		"no Commit line":         "Notes only\n",
		"empty body":             "",
		"a different commit":     relBody(relOther),
		"a repeated line":        "Commit: " + relE + "\nCommit: " + relE + "\n",
		"two different lines":    "Commit: " + relE + "\nCommit: " + relOther + "\n",
		"a short commit":         "Commit: " + relE[:39] + "\n",
		"an upper case commit":   "Commit: " + strings.ToUpper(strings.Repeat("ab", 20)) + "\n",
		"trailing text":          "Commit: " + relE + " tail\n",
		"a lower case key":       "commit: " + relE + "\n",
		"the line inside a line": "see Commit: " + relE + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			wantRelease(t, lookupRelease(pagesOf(relPage(relEntry(tag, false, body))), tag, relE), releaseOther)
		})
	}
	t.Run("a null body", func(t *testing.T) {
		entry := `{"tag_name":"` + tag + `","draft":false,"body":null}`
		wantRelease(t, lookupRelease(pagesOf(relPage(entry)), tag, relE), releaseOther)
	})
	t.Run("a missing body", func(t *testing.T) {
		entry := `{"tag_name":"` + tag + `","draft":false}`
		wantRelease(t, lookupRelease(pagesOf(relPage(entry)), tag, relE), releaseOther)
	})
	t.Run("a body that is not text", func(t *testing.T) {
		entry := `{"tag_name":"` + tag + `","draft":false,"body":5}`
		wantRelease(t, lookupRelease(pagesOf(relPage(entry)), tag, relE), releaseOther)
	})
}

func TestLookupReleaseUnknown(t *testing.T) {
	const tag = "v5.1.0-ezop.1"
	ok := relEntry("v1.0.0", false, "")
	cases := map[string]func() *fakePages{
		"the first call fails": func() *fakePages {
			f := pagesOf(relPage(ok))
			f.fail[1] = true
			return f
		},
		"the second page fails, with no tag on the first": func() *fakePages {
			f := pagesOf(relPage(ok), relPage(ok))
			f.fail[2] = true
			return f
		},
		"the third page fails after the tag was found": func() *fakePages {
			f := pagesOf(relPage(relEntry(tag, false, relBody(relE))), relPage(ok), relPage(ok))
			f.fail[3] = true
			return f
		},
		"the page is an object":        func() *fakePages { return pagesOf(`{"message":"Not Found"}`) },
		"the page is null":             func() *fakePages { return pagesOf("null") },
		"the page is a string":         func() *fakePages { return pagesOf(`"[]"`) },
		"the page is a number":         func() *fakePages { return pagesOf("0") },
		"the page is empty text":       func() *fakePages { return pagesOf("") },
		"the page is not JSON":         func() *fakePages { return pagesOf("<html>") },
		"the page has trailing data":   func() *fakePages { return pagesOf("[] []") },
		"the page is cut":              func() *fakePages { return pagesOf(`[{"tag_name":"v1","draft":false`) },
		"the second page is malformed": func() *fakePages { return pagesOf(relPage(ok), "{}") },
		"an entry is not an object":    func() *fakePages { return pagesOf(`["v5.1.0-ezop.1"]`) },
		"an entry is null":             func() *fakePages { return pagesOf(`[null]`) },
		"an entry has no tag_name":     func() *fakePages { return pagesOf(`[{"draft":false}]`) },
		"a tag_name is null":           func() *fakePages { return pagesOf(`[{"tag_name":null,"draft":false}]`) },
		"a tag_name is a number":       func() *fakePages { return pagesOf(`[{"tag_name":5,"draft":false}]`) },
		"an entry has no draft":        func() *fakePages { return pagesOf(`[{"tag_name":"v1.0.0"}]`) },
		"a draft is a string":          func() *fakePages { return pagesOf(`[{"tag_name":"v1.0.0","draft":"false"}]`) },
		"a draft is null":              func() *fakePages { return pagesOf(`[{"tag_name":"v1.0.0","draft":null}]`) },
		"a bad entry beside good ones": func() *fakePages { return pagesOf(relPage(ok, `{"tag_name":"v2.0.0"}`, ok)) },
		"a bad entry on a later page":  func() *fakePages { return pagesOf(relPage(ok), relPage(ok), relPage(`{"draft":true}`)) },
		"two entries with the tag": func() *fakePages {
			return pagesOf(relPage(relEntry(tag, false, relBody(relE)), relEntry(tag, false, relBody(relE))))
		},
		"a draft and a published entry": func() *fakePages {
			return pagesOf(relPage(relEntry(tag, true, ""), relEntry(tag, false, relBody(relE))))
		},
		"the tag on two pages": func() *fakePages {
			return pagesOf(relPage(relEntry(tag, false, "")), relPage(relEntry(tag, false, "")))
		},
		"the list does not end": func() *fakePages { return &fakePages{endless: true, pages: map[int]string{}, fail: map[int]bool{}} },
		"the list ends only after the ceiling": func() *fakePages {
			f := &fakePages{endless: true, pages: map[int]string{}, fail: map[int]bool{}}
			f.pages[51] = "[]"
			return f
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			wantRelease(t, lookupRelease(mk(), tag, relE), releaseUnknown)
		})
	}
	t.Run("the reasons come from a closed set and tell the causes apart", func(t *testing.T) {
		reasons := map[string]bool{}
		for _, name := range []string{
			"the first call fails", "the page is an object", "an entry has no tag_name", "an entry has no draft",
			"two entries with the tag", "the list does not end",
		} {
			reasons[lookupRelease(cases[name](), tag, relE).Reason] = true
		}
		if len(reasons) != 6 {
			t.Fatalf("reasons %v", reasons)
		}
	})
	t.Run("a failure on page 2 is never None", func(t *testing.T) {
		f := pagesOf(relPage(ok), relPage(relEntry(tag, false, "")))
		f.fail[2] = true
		if got := lookupRelease(f, tag, relE); got.State == releaseNone {
			t.Fatal("a partial list read as None")
		}
	})
	t.Run("the ceiling is 50 pages and page 51 is never asked for", func(t *testing.T) {
		f := cases["the list does not end"]()
		wantRelease(t, lookupRelease(f, tag, relE), releaseUnknown)
		if len(f.calls) != releasePageCeiling || releasePageCeiling != 50 {
			t.Fatalf("%d calls, ceiling %d", len(f.calls), releasePageCeiling)
		}
		if last := f.calls[len(f.calls)-1]; last != "releases?per_page=100&page=50" {
			t.Fatalf("last call %q", last)
		}
	})
	t.Run("a list that ends on page 50 with an empty page is not a ceiling hit", func(t *testing.T) {
		f := &fakePages{endless: true, pages: map[int]string{50: "[]"}, fail: map[int]bool{}}
		wantRelease(t, lookupRelease(f, tag, relE), releaseNone)
	})
	t.Run("a commit that is not a full SHA is Unknown and nothing is asked", func(t *testing.T) {
		for _, e := range []string{"", "abc", strings.Repeat("A", 40), relE + "0"} {
			f := pagesOf(relPage(relEntry(tag, false, relBody(relE))))
			wantRelease(t, lookupRelease(f, tag, e), releaseUnknown)
			if len(f.calls) != 0 {
				t.Fatalf("calls %v", f.calls)
			}
		}
	})
	t.Run("a tag that is not a Release Tag is Unknown and nothing is asked", func(t *testing.T) {
		for _, bad := range []string{"", "main", "v5.1.0", "v5.1.0-ezop.0", "v5.1.0-ezop.1\n"} {
			f := pagesOf("[]")
			wantRelease(t, lookupRelease(f, bad, relE), releaseUnknown)
			if len(f.calls) != 0 {
				t.Fatalf("calls %v", f.calls)
			}
		}
	})
	t.Run("an entry's fields never reach the reason", func(t *testing.T) {
		f := pagesOf(relPage(relEntry(relMarker, false, relMarker)), `{"message":"`+relMarker+`"}`)
		got := lookupRelease(f, tag, relE)
		wantRelease(t, got, releaseUnknown)
		if strings.Contains(fmt.Sprint(got), relMarker) {
			t.Fatalf("%+v", got)
		}
	})
}

func TestGHArgsReleasesPath(t *testing.T) {
	for _, page := range []int{1, 2, 9, 10, 49, 50} {
		endpoint := "releases?per_page=100&page=" + strconv.Itoa(page)
		got, err := ghArgs(endpoint)
		if err != nil {
			t.Fatalf("%q: %v", endpoint, err)
		}
		want := []string{"api", "--method", "GET", "-H", "X-GitHub-Api-Version: 2026-03-10", "repos/Ezopek/proton-cli/" + endpoint}
		if !slices.Equal(got, want) {
			t.Fatalf("argv = %q, want %q", got, want)
		}
	}
}

func TestGHArgsRefuseOtherReleasesPaths(t *testing.T) {
	for _, endpoint := range []string{
		"releases", "releases?", "releases?page=1&per_page=100", "releases?per_page=100",
		"releases?per_page=100&page=", "releases?per_page=100&page=0", "releases?per_page=100&page=01",
		"releases?per_page=100&page=51", "releases?per_page=100&page=100", "releases?per_page=100&page=-1",
		"releases?per_page=30&page=1", "releases?per_page=101&page=1", "releases?per_page=1000&page=1",
		"releases?per_page=100&page=1&x=1", "releases?per_page=100&page=1&per_page=100",
		"releases?per_page=100&page=1\n", "releases?per_page=100&page=1 ", " releases?per_page=100&page=1",
		"/releases?per_page=100&page=1", "releases/?per_page=100&page=1", "releases/latest", "releases/tags/v5.1.0-ezop.1",
		"releases/1", "releases/1/assets", "releases?per_page=100&page=1#x", "releases?per_page=100&page=a",
		"releases?per_page=100&page=1/../..", "repos/Other/repo/releases?per_page=100&page=1",
		"RELEASES?per_page=100&page=1", "releases?per_page=100&page=%31",
	} {
		if _, err := ghArgs(endpoint); err == nil {
			t.Fatalf("%q was accepted", endpoint)
		}
	}
}

func TestExecGHRunsTheExactReleasesArgv(t *testing.T) {
	g, log := fakeGHAdapter(t, `[]`)
	for _, page := range []string{"1", "50"} {
		if _, err := g.Get("releases?per_page=100&page=" + page); err != nil {
			t.Fatal(err)
		}
	}
	calls := readCalls(t, log)
	if len(calls) != 2 {
		t.Fatalf("calls = %+v", calls)
	}
	for i, page := range []string{"1", "50"} {
		want := []string{"api", "--method", "GET", "-H", "X-GitHub-Api-Version: 2026-03-10", "repos/Ezopek/proton-cli/releases?per_page=100&page=" + page}
		if !slices.Equal(calls[i].Argv, want) {
			t.Fatalf("call %d argv = %q", i, calls[i].Argv)
		}
	}
}

func TestExecGHRefusesAReleasesPathBeforeRunningGH(t *testing.T) {
	g, log := fakeGHAdapter(t, `[]`)
	for _, endpoint := range []string{"releases?per_page=100&page=51", "releases/latest", "releases?per_page=100&page=1&x=1"} {
		if _, err := g.Get(endpoint); err == nil {
			t.Fatalf("%q was run", endpoint)
		}
	}
	if calls := readCalls(t, log); len(calls) != 0 {
		t.Fatalf("gh ran: %+v", calls)
	}
}

func TestLookupReleaseOverTheExecAdapter(t *testing.T) {
	const tag = "v5.1.0-ezop.1"
	// One canned answer for every page is an endless list: the adapter path runs
	// to the ceiling and the answer is Unknown, with exactly 50 calls.
	g, log := fakeGHAdapter(t, relPage(relEntry("v1.0.0", false, "")))
	got := lookupRelease(g, tag, relE)
	wantRelease(t, got, releaseUnknown)
	calls := readCalls(t, log)
	if len(calls) != 50 {
		t.Fatalf("%d calls", len(calls))
	}
	if calls[0].Argv[5] != "repos/Ezopek/proton-cli/releases?per_page=100&page=1" || calls[49].Argv[5] != "repos/Ezopek/proton-cli/releases?per_page=100&page=50" {
		t.Fatalf("first %q last %q", calls[0].Argv[5], calls[49].Argv[5])
	}

	// An empty list is None, in one call, with the same adapter.
	g, log = fakeGHAdapter(t, `[]`)
	wantRelease(t, lookupRelease(g, tag, relE), releaseNone)
	if calls := readCalls(t, log); len(calls) != 1 {
		t.Fatalf("calls %+v", calls)
	}
}
