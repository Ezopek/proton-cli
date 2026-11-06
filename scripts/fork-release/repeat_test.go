package main

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

var (
	allRecordStates  = []recordState{recordAbsent, recordPresent, recordNotFile, recordUnknown}
	allReleaseStates = []releaseState{releaseNone, releasePublishedSame, releaseDraft, releaseOther, releaseUnknown}
)

func TestRepeatDecision(t *testing.T) {
	t.Run("only an absent record with no release proceeds", func(t *testing.T) {
		for _, rec := range allRecordStates {
			for _, rel := range allReleaseStates {
				err := repeatDecision(rec, rel)
				proceeds := rec == recordAbsent && rel == releaseNone
				if proceeds {
					if err != nil {
						t.Fatalf("(%v, %v) refused: %v", rec, rel, err)
					}
					continue
				}
				wantRefusal(t, err, "repeat")
			}
		}
	})
	t.Run("the realistic cases", func(t *testing.T) {
		for name, tc := range map[string]struct {
			rec     recordState
			rel     releaseState
			proceed bool
		}{
			"a first run":                        {recordAbsent, releaseNone, true},
			"a retry before any release object":  {recordAbsent, releaseNone, true},
			"a published release of this commit": {recordAbsent, releasePublishedSame, false},
			"a draft the lookup can see":         {recordAbsent, releaseDraft, false},
			"a release that is something else":   {recordAbsent, releaseOther, false},
			"a verified or abandoned record":     {recordPresent, releaseNone, false},
			"a record and its published release": {recordPresent, releasePublishedSame, false},
			"something that is not a file":       {recordNotFile, releaseNone, false},
			"an unreadable record":               {recordUnknown, releaseNone, false},
			"an unreadable release list":         {recordAbsent, releaseUnknown, false},
			"both unreadable":                    {recordUnknown, releaseUnknown, false},
		} {
			t.Run(name, func(t *testing.T) {
				err := repeatDecision(tc.rec, tc.rel)
				if tc.proceed != (err == nil) {
					t.Fatalf("(%v, %v): %v", tc.rec, tc.rel, err)
				}
			})
		}
	})
	t.Run("the zero values and unrecognized states refuse", func(t *testing.T) {
		var rec recordState
		var rel releaseState
		for _, tc := range []struct {
			rec recordState
			rel releaseState
		}{
			{rec, rel}, {rec, releaseNone}, {recordAbsent, rel},
			{recordState(99), releaseNone}, {recordAbsent, releaseState(99)},
		} {
			wantRefusal(t, repeatDecision(tc.rec, tc.rel), "repeat")
		}
	})
	t.Run("each cause has its own reason, and the record is named before the release", func(t *testing.T) {
		seen := map[string]string{}
		note := func(cause, reason string) {
			if reason == "" {
				t.Fatalf("%s: no reason", cause)
			}
			if other, dup := seen[reason]; dup {
				t.Fatalf("%s and %s share the reason %q", cause, other, reason)
			}
			seen[reason] = cause
		}
		reasonOf := func(rec recordState, rel releaseState) string {
			err := repeatDecision(rec, rel)
			wantRefusal(t, err, "repeat")
			return strings.TrimPrefix(err.Error(), "fork-release: repeat: ")
		}
		for _, rec := range []recordState{recordPresent, recordNotFile, recordUnknown} {
			note("record "+rec.String(), reasonOf(rec, releaseNone))
		}
		for _, rel := range []releaseState{releasePublishedSame, releaseDraft, releaseOther, releaseUnknown} {
			note("release "+rel.String(), reasonOf(recordAbsent, rel))
		}
		for _, rec := range []recordState{recordPresent, recordNotFile, recordUnknown} {
			for _, rel := range allReleaseStates {
				if got, want := reasonOf(rec, rel), reasonOf(rec, releaseNone); got != want {
					t.Fatalf("(%v, %v): reason %q, want the record's %q", rec, rel, got, want)
				}
			}
		}
	})
	t.Run("a refusal is one line and says nothing of the answers it came from", func(t *testing.T) {
		for _, rec := range allRecordStates {
			for _, rel := range allReleaseStates {
				err := repeatDecision(rec, rel)
				if err == nil {
					continue
				}
				line := err.Error()
				if !strings.HasPrefix(line, "fork-release: repeat: ") || strings.Contains(line, "\n") || strings.Contains(line, "http") {
					t.Fatalf("line %q", line)
				}
			}
		}
	})
}

func fstestFile(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

// ---- the optional step of checkTagBinding ----

func TestCheckTagBindingWithStep(t *testing.T) {
	const tag = "v5.1.0-ezop.1"
	t.Run("a nil step is checkTagBinding", func(t *testing.T) {
		r := newRelease(t, false)
		got, err := checkTagBindingWith(r.work.adapter(), tag, r.e, goodPin, nil)
		if err != nil || got != r.e {
			t.Fatalf("got %q, %v", got, err)
		}
		wantRefusal(t, func() error { _, err := checkTagBindingWith(r.work.adapter(), tag, r.e, "v2.99.0", nil); return err }(), "toolchain")
	})
	t.Run("the step runs once, with E, after the base and before the toolchain", func(t *testing.T) {
		r := newRelease(t, false)
		rg := &recGit{gitRepo: r.work.adapter()}
		var asked []string
		var before []string
		got, err := checkTagBindingWith(rg, tag, r.e, goodPin, func(e string) error {
			asked = append(asked, e)
			before = slices.Clone(rg.calls)
			return nil
		})
		if err != nil || got != r.e {
			t.Fatalf("got %q, %v", got, err)
		}
		if !slices.Equal(asked, []string{r.e}) {
			t.Fatalf("step calls %v", asked)
		}
		if slices.Contains(before, "tree") || slices.Contains(before, "blob") || !slices.Contains(before, "ancestor") {
			t.Fatalf("the step ran at the wrong place: %v", before)
		}
		if rg.count("tree") != 1 {
			t.Fatalf("the toolchain check did not run after the step: %v", rg.calls)
		}
	})
	t.Run("a refusal of the step ends the run before the toolchain", func(t *testing.T) {
		r := newRelease(t, false)
		rg := &recGit{gitRepo: r.work.adapter()}
		want := refusal{"profile", "held"}
		got, err := checkTagBindingWith(rg, tag, r.e, "v2.99.0", func(string) error { return want })
		if got != "" || err != want {
			t.Fatalf("got %q, %v", got, err)
		}
		if rg.count("tree") != 0 {
			t.Fatalf("the toolchain check ran: %v", rg.calls)
		}
	})
	t.Run("the step does not run when an earlier check refuses", func(t *testing.T) {
		for name, tc := range map[string]struct {
			tag, sha, check string
		}{
			"tag":       {"v5.1.0-ezop.0", "", "tag"},
			"identity":  {"v5.1.0-ezop.3", "", "identity"},
			"reachable": {"", "", "reachable"},
			"base":      {"", "", "base"},
		} {
			t.Run(name, func(t *testing.T) {
				r := newRelease(t, false)
				tagName, sha := tc.tag, tc.sha
				switch tc.check {
				case "reachable":
					r.work.write("stray.txt", "x")
					sha = r.work.commit("stray")
					r.work.git("push", "-q", "origin", sha+":refs/tags/v5.1.0-ezop.9")
					tagName = "v5.1.0-ezop.9"
				case "base":
					r.work.git("push", "-q", "origin", r.e+":refs/tags/v5.2.0-ezop.1")
					tagName = "v5.2.0-ezop.1"
				}
				if sha == "" {
					sha = r.e
				}
				ran := false
				_, err := checkTagBindingWith(r.work.adapter(), tagName, sha, goodPin, func(string) error { ran = true; return nil })
				wantRefusal(t, err, tc.check)
				if ran {
					t.Fatal("the step ran after a refusal")
				}
			})
		}
	})
}

// ---- guard --in-run and guard --recheck ----

// routedGH serves the record reads from a fakeAPI and the releases list from
// fakePages, and records every endpoint asked for, in order.
type routedGH struct {
	record *fakeAPI
	pages  *fakePages
	calls  []string
}

func (r *routedGH) Get(endpoint string) ([]byte, error) {
	r.calls = append(r.calls, endpoint)
	if strings.HasPrefix(endpoint, "releases?") {
		return r.pages.Get(endpoint)
	}
	return r.record.Get(endpoint)
}

// stepGit is a fakeGit that also serves the tags of origin.
type stepGit struct {
	*fakeGit
	tags map[string]string
}

func (g *stepGit) OriginTags() (map[string]string, error) {
	g.calls = append(g.calls, "origin tags")
	return g.tags, nil
}

// inRunWorld is one run of the release job: the git fake, the GitHub fake, the
// three environment variables and a record of which of them were read.
type inRunWorld struct {
	git      *stepGit
	gh       *routedGH
	env      map[string]string
	envReads []string
}

const inRunTag = recTag

func absentRecordWorld() *fakeAPI {
	return lkWorld([]lkEntry{lkBlobEntry("README.md", lkReadme)}, map[string][]byte{blobID(lkReadme): lkReadme})
}

func newInRunWorld(t *testing.T) *inRunWorld {
	t.Helper()
	tree := cleanTree(t)
	tree["go.mod"] = fstestFile(goodMod)
	tree["devbox.lock"] = fstestFile(goodLock)
	g := &stepGit{fakeGit: newFakeGit(tree), tags: map[string]string{inRunTag: targetSHA, "v5.1.0": helperSHA}}
	return &inRunWorld{
		git: g,
		gh:  &routedGH{record: absentRecordWorld(), pages: pagesOf("[]")},
		env: map[string]string{"GITHUB_REF_NAME": inRunTag, "GITHUB_SHA": targetSHA, "GORELEASER_PIN": goodPin},
	}
}

func (w *inRunWorld) deps() (guardDeps, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return guardDeps{
		git:    w.git,
		build:  cleanBuild,
		stdout: &out,
		stderr: &errb,
		getenv: func(name string) string {
			w.envReads = append(w.envReads, name)
			return w.env[name]
		},
		gh: w.gh,
	}, &out, &errb
}

func (w *inRunWorld) run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	d, out, errb := w.deps()
	code = run(append([]string{"guard"}, args...), d)
	return code, out.String(), errb.String()
}

// refusedAt runs the mode and requires exactly the lines of one check on
// standard error, nothing on standard output and exit 1; it returns the lines.
func (w *inRunWorld) refusedAt(t *testing.T, check string, args ...string) []string {
	t.Helper()
	code, out, errb := w.run(t, args...)
	if code != 1 || out != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
	}
	lines := strings.Split(strings.TrimSuffix(errb, "\n"), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "fork-release: "+check+": ") || strings.TrimSpace(line) != line {
			t.Fatalf("line %q is not a refusal of %q (stderr %q)", line, check, errb)
		}
	}
	return lines
}

func (w *inRunWorld) treeCalls() (profile, toolchain int) {
	for _, c := range w.git.calls {
		switch c {
		case "tree " + targetSHA + " " + strings.Join(profilePaths("release"), ","):
			profile++
		case "tree " + targetSHA + " " + strings.Join(toolchainPaths, ","):
			toolchain++
		}
	}
	return
}

var recordWorlds = map[string]func() *fakeAPI{
	"Absent": absentRecordWorld,
	"Present": func() *fakeAPI {
		f, _ := lkRecordWorld()
		return f
	},
	"NotFile": func() *fakeAPI {
		return lkWorld([]lkEntry{
			lkBlobEntry("README.md", lkReadme),
			{Path: recTag + ".md", Mode: "040000", Type: "tree", SHA: lkOtherSHA},
		}, map[string][]byte{blobID(lkReadme): lkReadme})
	},
	"Unknown": func() *fakeAPI {
		f := absentRecordWorld()
		f.fail[epVer] = true
		return f
	},
}

func releasePages(name string) *fakePages {
	switch name {
	case "None":
		return pagesOf("[]")
	case "Published-Same":
		return pagesOf(relPage(relEntry(inRunTag, false, relBody(targetSHA))))
	case "Draft":
		return pagesOf(relPage(relEntry(inRunTag, true, "")))
	case "Other":
		return pagesOf(relPage(relEntry(inRunTag, false, relBody(relOther))))
	}
	f := pagesOf("[]")
	f.fail[1] = true
	return f
}

func TestGuardInRunPasses(t *testing.T) {
	w := newInRunWorld(t)
	code, out, errb := w.run(t, "--in-run")
	if code != 0 || errb != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
	}
	if !strings.HasPrefix(out, "fork-release: guard: ok") || strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("stdout %q", out)
	}
	profile, toolchain := w.treeCalls()
	if profile != 1 || toolchain != 1 {
		t.Fatalf("tree reads: profile %d, toolchain %d (%v)", profile, toolchain, w.git.calls)
	}
	if len(w.gh.calls) != 6 || w.gh.calls[0] != epRef || w.gh.calls[5] != "releases?per_page=100&page=1" {
		t.Fatalf("GitHub reads %v", w.gh.calls)
	}
	allowed := []string{"GITHUB_REF_NAME", "GITHUB_SHA", "GORELEASER_PIN"}
	for _, name := range w.envReads {
		if !slices.Contains(allowed, name) {
			t.Fatalf("the environment variable %s was read", name)
		}
	}
	for _, name := range allowed {
		if !slices.Contains(w.envReads, name) {
			t.Fatalf("%s was not read", name)
		}
	}
	if w.git.did("urls") {
		t.Fatalf("the origin URLs were read: %v", w.git.calls)
	}
}

func TestGuardInRunRetryBeforeAnyRelease(t *testing.T) {
	// The same states as a first run: a retry of a tag whose earlier attempt
	// failed before it made a release object reads as (Absent, None).
	w := newInRunWorld(t)
	w.gh.pages = pagesOf(relPage(relEntry("v5.1.0-ezop.2", false, relBody(relOther))), relPage(relEntry("v5.1.0", false, "")))
	if code, out, errb := w.run(t, "--in-run"); code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
	}
}

func TestGuardInRunOrder(t *testing.T) {
	t.Run("tag pattern", func(t *testing.T) {
		for _, tag := range []string{"", "v5.1.0", "main", "v5.1.0-ezop.0", "v5.1.0-ezop.1\n", "refs/tags/v5.1.0-ezop.1"} {
			w := newInRunWorld(t)
			w.env["GITHUB_REF_NAME"] = tag
			w.refusedAt(t, "tag", "--in-run")
			if len(w.git.calls) != 0 || len(w.gh.calls) != 0 {
				t.Fatalf("%q: git %v, GitHub %v", tag, w.git.calls, w.gh.calls)
			}
		}
	})
	t.Run("identity", func(t *testing.T) {
		for name, mutate := range map[string]func(w *inRunWorld){
			"GITHUB_SHA unset":      func(w *inRunWorld) { delete(w.env, "GITHUB_SHA") },
			"GITHUB_SHA malformed":  func(w *inRunWorld) { w.env["GITHUB_SHA"] = "main" },
			"GITHUB_SHA other":      func(w *inRunWorld) { w.env["GITHUB_SHA"] = relOther; w.git.resolve[relOther] = relOther },
			"HEAD other":            func(w *inRunWorld) { w.git.resolve["HEAD"] = relOther },
			"tag moved on origin":   func(w *inRunWorld) { w.git.tags[inRunTag] = relOther },
			"tag missing on origin": func(w *inRunWorld) { delete(w.git.tags, inRunTag) },
		} {
			t.Run(name, func(t *testing.T) {
				w := newInRunWorld(t)
				mutate(w)
				w.refusedAt(t, "identity", "--in-run")
				if w.git.did("fetch") || w.git.did("ancestor") || w.git.did("tree") || len(w.gh.calls) != 0 {
					t.Fatalf("later reads: git %v, GitHub %v", w.git.calls, w.gh.calls)
				}
			})
		}
	})
	t.Run("reachable", func(t *testing.T) {
		w := newInRunWorld(t)
		w.git.ancestry = 1
		w.refusedAt(t, "reachable", "--in-run")
		if w.git.did("tree") || len(w.gh.calls) != 0 {
			t.Fatalf("later reads: git %v, GitHub %v", w.git.calls, w.gh.calls)
		}
	})
	t.Run("reachable, fetch failed", func(t *testing.T) {
		w := newInRunWorld(t)
		w.git.fetchErr = errors.New("boom " + secretURL)
		lines := w.refusedAt(t, "reachable", "--in-run")
		if strings.Contains(lines[0], secretURL) || w.git.did("tree") || len(w.gh.calls) != 0 {
			t.Fatalf("lines %v, git %v, GitHub %v", lines, w.git.calls, w.gh.calls)
		}
	})
	t.Run("base", func(t *testing.T) {
		w := newInRunWorld(t)
		delete(w.git.tags, "v5.1.0")
		w.refusedAt(t, "base", "--in-run")
		if w.git.did("tree") || len(w.gh.calls) != 0 {
			t.Fatalf("later reads: git %v, GitHub %v", w.git.calls, w.gh.calls)
		}
	})
	t.Run("base, the tag's X.Y.Z is not the base", func(t *testing.T) {
		w := newInRunWorld(t)
		w.git.tags["v5.2.0"] = helperSHA
		w.refusedAt(t, "base", "--in-run")
	})
	t.Run("profile", func(t *testing.T) {
		w := newInRunWorld(t)
		w.git.tree[".github/workflows/new.yml"] = fstestFile("x: 1\n")
		w.git.tree[".github/dependabot.yml"] = fstestFile("version: 2\n")
		lines := w.refusedAt(t, "profile", "--in-run")
		findings := CheckRelease(w.git.tree)
		if len(findings) < 2 || len(lines) != len(findings) {
			t.Fatalf("%d lines for %d findings: %v", len(lines), len(findings), lines)
		}
		for i, f := range findings {
			if lines[i] != "fork-release: profile: "+f.String() {
				t.Fatalf("line %d is %q, want the finding %q", i, lines[i], f)
			}
		}
		profile, toolchain := w.treeCalls()
		if profile != 1 || toolchain != 0 || len(w.gh.calls) != 0 {
			t.Fatalf("later reads: git %v, GitHub %v", w.git.calls, w.gh.calls)
		}
	})
	t.Run("profile, the tree cannot be read", func(t *testing.T) {
		w := newInRunWorld(t)
		w.git.treeErr = errors.New("boom " + secretURL)
		lines := w.refusedAt(t, "profile", "--in-run")
		if len(lines) != 1 || strings.Contains(lines[0], secretURL) || len(w.gh.calls) != 0 {
			t.Fatalf("lines %v, GitHub %v", lines, w.gh.calls)
		}
	})
	t.Run("toolchain", func(t *testing.T) {
		for name, mutate := range map[string]func(w *inRunWorld){
			"a different pin": func(w *inRunWorld) { w.env["GORELEASER_PIN"] = "v2.99.0" },
			"no pin":          func(w *inRunWorld) { delete(w.env, "GORELEASER_PIN") },
			"a pin without v": func(w *inRunWorld) { w.env["GORELEASER_PIN"] = "2.17.1" },
		} {
			t.Run(name, func(t *testing.T) {
				w := newInRunWorld(t)
				mutate(w)
				w.refusedAt(t, "toolchain", "--in-run")
				profile, toolchain := w.treeCalls()
				if profile != 1 || toolchain != 1 || len(w.gh.calls) != 0 {
					t.Fatalf("reads: git %v, GitHub %v", w.git.calls, w.gh.calls)
				}
			})
		}
	})
	t.Run("repeat", func(t *testing.T) {
		for rec, mkRec := range recordWorlds {
			for _, rel := range []string{"None", "Published-Same", "Draft", "Other", "Unknown"} {
				t.Run(rec+" and "+rel, func(t *testing.T) {
					w := newInRunWorld(t)
					w.gh.record = mkRec()
					w.gh.pages = releasePages(rel)
					if rec == "Absent" && rel == "None" {
						if code, out, errb := w.run(t, "--in-run"); code != 0 {
							t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
						}
						return
					}
					lines := w.refusedAt(t, "repeat", "--in-run")
					if len(lines) != 1 {
						t.Fatalf("lines %v", lines)
					}
					for _, bad := range []string{relMarker, "http", lkTip, recTag, inRunTag} {
						if strings.Contains(lines[0], bad) {
							t.Fatalf("the line quotes %q: %s", bad, lines[0])
						}
					}
				})
			}
		}
	})
	t.Run("repeat, a failure after the first page of the list", func(t *testing.T) {
		w := newInRunWorld(t)
		w.gh.pages = pagesOf(relPage(relEntry("v1.0.0", false, "")), relPage(relEntry("v1.0.1", false, "")))
		w.gh.pages.fail[2] = true
		w.refusedAt(t, "repeat", "--in-run")
	})
}

func TestGuardRecheck(t *testing.T) {
	t.Run("a first run passes with only the two reads", func(t *testing.T) {
		w := newInRunWorld(t)
		code, out, errb := w.run(t, "--recheck")
		if code != 0 || errb != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		if !strings.HasPrefix(out, "fork-release: guard: ok") || strings.Count(out, "\n") != 1 {
			t.Fatalf("stdout %q", out)
		}
		if !slices.Equal(w.git.calls, []string{"resolve HEAD"}) {
			t.Fatalf("git calls %v", w.git.calls)
		}
		if len(w.gh.calls) != 6 || w.gh.calls[0] != epRef || w.gh.calls[5] != "releases?per_page=100&page=1" {
			t.Fatalf("GitHub reads %v", w.gh.calls)
		}
		if !slices.Equal(slices.Compact(slices.Sorted(slices.Values(w.envReads))), []string{"GITHUB_REF_NAME"}) {
			t.Fatalf("environment reads %v", w.envReads)
		}
	})
	t.Run("the same decision over every combination", func(t *testing.T) {
		for rec, mkRec := range recordWorlds {
			for _, rel := range []string{"None", "Published-Same", "Draft", "Other", "Unknown"} {
				t.Run(rec+" and "+rel, func(t *testing.T) {
					w := newInRunWorld(t)
					w.gh.record = mkRec()
					w.gh.pages = releasePages(rel)
					inRun := newInRunWorld(t)
					inRun.gh.record = mkRec()
					inRun.gh.pages = releasePages(rel)
					wantCode, _, wantErr := inRun.run(t, "--in-run")
					code, out, errb := w.run(t, "--recheck")
					if code != wantCode || (code == 0) != (rec == "Absent" && rel == "None") {
						t.Fatalf("exit %d, in-run exit %d; stdout %q, stderr %q", code, wantCode, out, errb)
					}
					if code != 0 && (out != "" || errb != wantErr) {
						t.Fatalf("recheck %q, in-run %q", errb, wantErr)
					}
					if !slices.Equal(w.git.calls, []string{"resolve HEAD"}) {
						t.Fatalf("git calls %v", w.git.calls)
					}
				})
			}
		}
	})
	t.Run("a bad tag is refused before any read", func(t *testing.T) {
		w := newInRunWorld(t)
		w.env["GITHUB_REF_NAME"] = "main"
		w.refusedAt(t, "tag", "--recheck")
		if len(w.git.calls) != 0 || len(w.gh.calls) != 0 {
			t.Fatalf("git %v, GitHub %v", w.git.calls, w.gh.calls)
		}
	})
	t.Run("a HEAD that does not resolve is refused before the GitHub reads", func(t *testing.T) {
		w := newInRunWorld(t)
		delete(w.git.resolve, "HEAD")
		lines := w.refusedAt(t, "identity", "--recheck")
		if strings.Contains(lines[0], secretURL) || len(w.gh.calls) != 0 {
			t.Fatalf("lines %v, GitHub %v", lines, w.gh.calls)
		}
	})
	t.Run("a HEAD that is not a full SHA is refused", func(t *testing.T) {
		w := newInRunWorld(t)
		w.git.resolve["HEAD"] = "main"
		w.refusedAt(t, "identity", "--recheck")
		if len(w.gh.calls) != 0 {
			t.Fatalf("GitHub %v", w.gh.calls)
		}
	})
	t.Run("the states are read again, not remembered", func(t *testing.T) {
		w := newInRunWorld(t)
		if code, _, errb := w.run(t, "--in-run"); code != 0 {
			t.Fatalf("the first read: exit %d, %q", code, errb)
		}
		w.gh.pages = releasePages("Draft")
		w.git.calls, w.gh.calls = nil, nil
		w.refusedAt(t, "repeat", "--recheck")
		// The record chain, then the list: a page with the draft and the empty
		// page that ends it.
		if len(w.gh.calls) != 7 || w.gh.calls[0] != epRef {
			t.Fatalf("GitHub reads %v", w.gh.calls)
		}
	})
}

func TestGuardInRunModesTakeNoOtherArgument(t *testing.T) {
	for name, args := range map[string][]string{
		"in-run and a word":  {"--in-run", "x"},
		"in-run twice":       {"--in-run", "--in-run"},
		"both modes":         {"--in-run", "--recheck"},
		"recheck and a word": {"--recheck", "x"},
		"recheck and a tag":  {"--recheck", inRunTag},
		"in-run and profile": {"--in-run", "--profile", "release", "main"},
		"bare word":          {"in-run"},
		"equals form":        {"--in-run=1"},
		"upper case":         {"--IN-RUN"},
		"single dash":        {"-in-run"},
		"recheck bare word":  {"recheck"},
		"empty argument":     {""},
		"in-run and empty":   {"--in-run", ""},
		"unknown flag":       {"--other"},
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
	t.Run("guard with no mode is usage", func(t *testing.T) {
		w := newInRunWorld(t)
		code, out, errb := w.run(t)
		if code != 2 || out != "" || errb != guardUsage+"\n" || len(w.gh.calls) != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("the usage line names every mode", func(t *testing.T) {
		for _, want := range []string{"--profile release|mirror COMMIT", "--in-run", "--recheck"} {
			if !strings.Contains(guardUsage, want) {
				t.Fatalf("usage %q lacks %q", guardUsage, want)
			}
		}
		if !strings.HasPrefix(guardUsage, "fork-release: usage: ") || strings.Contains(guardUsage, "\n") {
			t.Fatalf("usage %q", guardUsage)
		}
	})
}

func TestGuardInRunWithoutProductionInputs(t *testing.T) {
	t.Run("no environment reader is an empty environment", func(t *testing.T) {
		for _, mode := range []string{"--in-run", "--recheck"} {
			g := newFakeGit(cleanTree(t))
			d, out, errb := testDeps(g)
			if code := run([]string{"guard", mode}, d); code != 1 || out.Len() != 0 || !strings.HasPrefix(errb.String(), "fork-release: tag: ") {
				t.Fatalf("%s: exit, stdout %q, stderr %q", mode, out, errb)
			}
		}
	})
	t.Run("no GitHub reader refuses at the repeat check and does not panic", func(t *testing.T) {
		w := newInRunWorld(t)
		d, out, errb := w.deps()
		d.gh = nil
		if code := run([]string{"guard", "--in-run"}, d); code != 1 || out.Len() != 0 || !strings.HasPrefix(errb.String(), "fork-release: repeat: ") {
			t.Fatalf("stdout %q, stderr %q", out, errb)
		}
		d, out, errb = w.deps()
		d.gh = nil
		if code := run([]string{"guard", "--recheck"}, d); code != 1 || out.Len() != 0 || !strings.HasPrefix(errb.String(), "fork-release: repeat: ") {
			t.Fatalf("stdout %q, stderr %q", out, errb)
		}
	})
	t.Run("a success line that cannot be written is a refusal", func(t *testing.T) {
		w := newInRunWorld(t)
		d, _, errb := w.deps()
		d.stdout = errWriter{}
		if code := run([]string{"guard", "--in-run"}, d); code != 1 || !strings.HasPrefix(errb.String(), "fork-release: output: ") {
			t.Fatalf("stderr %q", errb)
		}
	})
	t.Run("production wiring", func(t *testing.T) {
		d := productionDeps()
		if _, ok := d.gh.(execGH); !ok {
			t.Fatalf("production GitHub reader is %T", d.gh)
		}
		if d.getenv == nil {
			t.Fatal("no environment reader")
		}
	})
}

// A scratch repository, a scratch origin, the real git adapter and fakes for the
// GitHub reads: the first run passes, and writes nothing.
func TestGuardInRunEndToEnd(t *testing.T) {
	bareDir := newBare(t)
	src := newScratch(t)
	src.git("remote", "add", "origin", bareDir)
	src.write("go.mod", goodMod)
	src.write("devbox.lock", goodLock)
	c1 := src.commit("upstream release")
	src.git("tag", "-a", "-m", "upstream", "v5.1.0", c1)
	src.write(".goreleaser.yaml", string(fixture(t, "goreleaser.cleaned.yaml")))
	src.write(".github/workflows/ci.yml", string(fixture(t, "ci.yml")))
	e := src.commit("fork commit")
	src.git("branch", "-m", "ezop")
	src.git("push", "-q", "origin", "ezop")
	src.git("tag", inRunTag, e)
	src.git("push", "-q", "origin", "--tags")

	work := newScratch(t)
	work.git("remote", "add", "origin", bareDir)
	work.git("fetch", "-q", "origin", "+refs/heads/ezop:refs/remotes/origin/ezop", "+refs/tags/*:refs/tags/*")
	work.git("checkout", "-q", "-b", "ezop", "origin/ezop")
	bare := &scratch{t: t, dir: bareDir, env: work.env}

	runMode := func(mode string, pages *fakePages) (int, string, string, *routedGH) {
		gh := &routedGH{record: absentRecordWorld(), pages: pages}
		var out, errb bytes.Buffer
		env := map[string]string{"GITHUB_REF_NAME": inRunTag, "GITHUB_SHA": e, "GORELEASER_PIN": goodPin}
		d := guardDeps{
			git: work.adapter(), build: cleanBuild, stdout: &out, stderr: &errb,
			getenv: func(name string) string { return env[name] }, gh: gh,
		}
		return runGuard([]string{mode}, d), out.String(), errb.String(), gh
	}

	before := bare.git("for-each-ref") + "\n" + work.git("for-each-ref", "refs/tags", "refs/heads")
	code, out, errb, gh := runMode("--in-run", pagesOf("[]"))
	if code != 0 || errb != "" || !strings.HasPrefix(out, "fork-release: guard: ok") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
	}
	if after := bare.git("for-each-ref") + "\n" + work.git("for-each-ref", "refs/tags", "refs/heads"); after != before {
		t.Fatalf("a ref changed:\n%s\n-- became --\n%s", before, after)
	}
	if len(gh.calls) != 6 {
		t.Fatalf("GitHub reads %v", gh.calls)
	}

	code, out, errb, _ = runMode("--recheck", pagesOf("[]"))
	if code != 0 || errb != "" || out == "" {
		t.Fatalf("recheck: exit %d, stdout %q, stderr %q", code, out, errb)
	}

	// Once a release object exists, the same tree and tag are refused.
	code, out, errb, _ = runMode("--in-run", pagesOf(relPage(relEntry(inRunTag, true, ""))))
	if code != 1 || out != "" || !strings.HasPrefix(errb, "fork-release: repeat: ") {
		t.Fatalf("draft: exit %d, stdout %q, stderr %q", code, out, errb)
	}
}
