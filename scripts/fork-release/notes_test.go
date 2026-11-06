package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRunLinePrefixIsShared(t *testing.T) {
	if runLinePrefix != "Run: " {
		t.Fatalf("prefix %q", runLinePrefix)
	}
	if notesFileName != "fork-release-notes.md" {
		t.Fatalf("notes file name %q", notesFileName)
	}
}

func TestParseRunLine(t *testing.T) {
	for name, tc := range map[string]struct {
		body    string
		id, att string
		ok      bool
	}{
		"one line":                    {"Run: 12/3", "12", "3", true},
		"line feed ending":            {"Run: 12/3\n", "12", "3", true},
		"between other lines":         {"a\nRun: 7/1\nCommit: x\n", "7", "1", true},
		"CRLF":                        {"a\r\nRun: 7/1\r\nCommit: x\r\n", "7", "1", true},
		"CRLF at the end of the body": {"Run: 7/1\r\n", "7", "1", true},
		"large numbers":               {"Run: 123456789012/42\n", "123456789012", "42", true},
		"none":                        {"Commit: x\n", "", "", false},
		"empty body":                  {"", "", "", false},
		"repeated, same":              {"Run: 1/1\nRun: 1/1\n", "", "", false},
		"repeated, different":         {"Run: 1/1\nRun: 2/1\n", "", "", false},
		"a malformed line and a good": {"Run: nope\nRun: 1/1\n", "", "", false},
		"no slash":                    {"Run: 11\n", "", "", false},
		"two slashes":                 {"Run: 1/1/1\n", "", "", false},
		"empty run id":                {"Run: /1\n", "", "", false},
		"empty attempt":               {"Run: 1/\n", "", "", false},
		"zero run id":                 {"Run: 0/1\n", "", "", false},
		"zero attempt":                {"Run: 1/0\n", "", "", false},
		"leading zero run id":         {"Run: 01/1\n", "", "", false},
		"leading zero attempt":        {"Run: 1/01\n", "", "", false},
		"negative":                    {"Run: -1/1\n", "", "", false},
		"plus sign":                   {"Run: +1/1\n", "", "", false},
		"letters":                     {"Run: a/1\n", "", "", false},
		"a space inside":              {"Run: 1 /1\n", "", "", false},
		"trailing text":               {"Run: 1/1 more\n", "", "", false},
		"trailing space":              {"Run: 1/1 \n", "", "", false},
		"two carriage returns":        {"Run: 1/1\r\r\n", "", "", false},
		"lower case key":              {"run: 1/1\n", "", "", false},
		"no space":                    {"Run:1/1\n", "", "", false},
		"indented":                    {" Run: 1/1\n", "", "", false},
		"quoted inside a line":        {"see Run: 1/1\n", "", "", false},
		"empty value":                 {"Run: \n", "", "", false},
		"a full width digit":          {"Run: １/1\n", "", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			id, att, ok := parseRunLine(tc.body)
			if id != tc.id || att != tc.att || ok != tc.ok {
				t.Fatalf("got (%q, %q, %v), want (%q, %q, %v)", id, att, ok, tc.id, tc.att, tc.ok)
			}
		})
	}
}

func TestRenderNotes(t *testing.T) {
	const tag = "v5.1.0-ezop.1"
	body := renderNotes(tag, "v5.1.0", relE, "987654321", "2")

	t.Run("names the base, the tag and warns against proton update", func(t *testing.T) {
		for _, want := range []string{"v5.1.0", tag, "5.1.0-ezop.1", "`proton update`", "Upstream"} {
			if !strings.Contains(body, want) {
				t.Errorf("the notes lack %q:\n%s", want, body)
			}
		}
	})
	t.Run("both parsers read the two lines back", func(t *testing.T) {
		if sha, ok := parseCommitLine(body); !ok || sha != relE {
			t.Fatalf("commit line: %q, %v", sha, ok)
		}
		if id, att, ok := parseRunLine(body); !ok || id != "987654321" || att != "2" {
			t.Fatalf("run line: %q/%q, %v", id, att, ok)
		}
	})
	t.Run("the lines are written from the shared constants, once each", func(t *testing.T) {
		var commits, runs []string
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, commitLinePrefix) {
				commits = append(commits, line)
			}
			if strings.HasPrefix(line, runLinePrefix) {
				runs = append(runs, line)
			}
		}
		if !slices.Equal(commits, []string{commitLinePrefix + relE}) || !slices.Equal(runs, []string{runLinePrefix + "987654321/2"}) {
			t.Fatalf("commit lines %q, run lines %q", commits, runs)
		}
		if strings.Count(body, commitLinePrefix) != 1 || strings.Count(body, runLinePrefix) != 1 {
			t.Fatalf("a prefix appears inside another line:\n%s", body)
		}
	})
	t.Run("the parsers tolerate CRLF", func(t *testing.T) {
		crlf := strings.ReplaceAll(body, "\n", "\r\n")
		if sha, ok := parseCommitLine(crlf); !ok || sha != relE {
			t.Fatalf("commit line: %q, %v", sha, ok)
		}
		if id, att, ok := parseRunLine(crlf); !ok || id != "987654321" || att != "2" {
			t.Fatalf("run line: %q/%q, %v", id, att, ok)
		}
	})
	t.Run("a repeated line makes the parsers reject the body", func(t *testing.T) {
		if _, ok := parseCommitLine(body + commitLinePrefix + relE + "\n"); ok {
			t.Fatal("a repeated Commit line was accepted")
		}
		if _, _, ok := parseRunLine(body + runLinePrefix + "987654321/2\n"); ok {
			t.Fatal("a repeated Run line was accepted")
		}
	})
	t.Run("the release lookup reads the rendered notes as Published-Same", func(t *testing.T) {
		wantRelease(t, lookupRelease(pagesOf(relPage(relEntry(tag, false, body))), tag, relE), releasePublishedSame)
	})
	t.Run("the body is plain text that ends in a line feed", func(t *testing.T) {
		if !strings.HasSuffix(body, "\n") || strings.Contains(body, "\r") || strings.Contains(body, "\x00") {
			t.Fatalf("body %q", body)
		}
	})
}

// notesWorld is one run of the notes command: a git fake that serves the tags
// of origin, the five environment variables and a record of which were read.
type notesWorld struct {
	git      *notesGit
	env      map[string]string
	envReads []string
	dir      string
}

// notesGit is a fakeGit that serves the tags of origin, or fails to.
type notesGit struct {
	*fakeGit
	tags    map[string]string
	tagsErr error
}

func (g *notesGit) OriginTags() (map[string]string, error) {
	g.calls = append(g.calls, "origin tags")
	return g.tags, g.tagsErr
}

func newNotesWorld(t *testing.T) *notesWorld {
	t.Helper()
	dir := t.TempDir()
	g := &notesGit{fakeGit: newFakeGit(cleanTree(t)), tags: map[string]string{inRunTag: targetSHA, "v5.1.0": helperSHA}}
	return &notesWorld{
		git: g,
		dir: dir,
		env: map[string]string{
			"GITHUB_REF_NAME":    inRunTag,
			"GITHUB_SHA":         targetSHA,
			"GITHUB_RUN_ID":      "987654321",
			"GITHUB_RUN_ATTEMPT": "2",
			"RUNNER_TEMP":        dir,
		},
	}
}

func (w *notesWorld) deps() (guardDeps, *bytes.Buffer, *bytes.Buffer) {
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
	}, &out, &errb
}

func (w *notesWorld) run(args ...string) (code int, stdout, stderr string) {
	d, out, errb := w.deps()
	code = run(append([]string{"notes"}, args...), d)
	return code, out.String(), errb.String()
}

func (w *notesWorld) notesPath() string { return filepath.Join(w.dir, notesFileName) }

// entries lists the names in the notes directory.
func entriesOf(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

// oneLineRefusal checks stderr is exactly one line of the check and stdout is
// empty.
func oneLineRefusal(t *testing.T, code int, stdout, stderr, check string) {
	t.Helper()
	if code != 1 || stdout != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !strings.HasPrefix(stderr, "fork-release: "+check+": ") || strings.Count(stderr, "\n") != 1 || !strings.HasSuffix(stderr, "\n") {
		t.Fatalf("stderr %q, want a one-line %s refusal", stderr, check)
	}
}

func TestRunNotes(t *testing.T) {
	t.Run("a valid tag writes exactly one new file with the notes and prints one line", func(t *testing.T) {
		w := newNotesWorld(t)
		code, out, errb := w.run("--in-run")
		if code != 0 || errb != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		if !strings.HasPrefix(out, "fork-release: notes: ok") || strings.Count(out, "\n") != 1 {
			t.Fatalf("stdout %q", out)
		}
		if names := entriesOf(t, w.dir); !slices.Equal(names, []string{"fork-release-notes.md"}) {
			t.Fatalf("files %v", names)
		}
		got, err := os.ReadFile(w.notesPath())
		if err != nil {
			t.Fatal(err)
		}
		if want := renderNotes(inRunTag, "v5.1.0", targetSHA, "987654321", "2"); string(got) != want {
			t.Fatalf("notes:\n%s\nwant:\n%s", got, want)
		}
		if sha, ok := parseCommitLine(string(got)); !ok || sha != targetSHA {
			t.Fatalf("commit line %q %v", sha, ok)
		}
		if id, att, ok := parseRunLine(string(got)); !ok || id != "987654321" || att != "2" {
			t.Fatalf("run line %q/%q %v", id, att, ok)
		}
		info, err := os.Stat(w.notesPath())
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
			t.Fatalf("mode %v, %v", info.Mode(), err)
		}
	})
	t.Run("only the five named variables are read, and no ref, fetch or write is made", func(t *testing.T) {
		w := newNotesWorld(t)
		if code, out, errb := w.run("--in-run"); code != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		allowed := []string{"GITHUB_REF_NAME", "GITHUB_SHA", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "RUNNER_TEMP"}
		for _, name := range w.envReads {
			if !slices.Contains(allowed, name) {
				t.Errorf("read %q", name)
			}
		}
		if w.git.did("fetch") || w.git.did("urls") || w.git.did("tree") || w.git.did("blob") {
			t.Fatalf("git calls %v", w.git.calls)
		}
		if !w.git.did("origin tags") {
			t.Fatalf("the tags of origin were not read: %v", w.git.calls)
		}
	})
	t.Run("the base is the highest Upstream tag that is an ancestor", func(t *testing.T) {
		w := newNotesWorld(t)
		w.env["GITHUB_REF_NAME"] = "v5.2.0-ezop.3"
		w.git.tags = map[string]string{"v5.2.0-ezop.3": targetSHA, "v5.1.0": helperSHA, "v5.2.0": ezopTipSHA, "v5.10.0-ezop.1": targetSHA}
		code, out, errb := w.run("--in-run")
		if code != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		got, _ := os.ReadFile(w.notesPath())
		if !strings.Contains(string(got), "v5.2.0") || strings.Contains(string(got), "v5.1.0") {
			t.Fatalf("notes:\n%s", got)
		}
	})
	t.Run("over a scratch origin with real git", func(t *testing.T) {
		r := newRelease(t, false)
		dir := t.TempDir()
		var out, errb bytes.Buffer
		env := map[string]string{
			"GITHUB_REF_NAME": "v5.1.0-ezop.1", "GITHUB_SHA": r.e,
			"GITHUB_RUN_ID": "55", "GITHUB_RUN_ATTEMPT": "1", "RUNNER_TEMP": dir,
		}
		d := guardDeps{git: r.work.adapter(), stdout: &out, stderr: &errb, getenv: func(n string) string { return env[n] }}
		before := r.refs()
		if code := run([]string{"notes", "--in-run"}, d); code != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, &out, &errb)
		}
		if after := r.refs(); after != before {
			t.Fatalf("a ref changed:\n%s\n-- became --\n%s", before, after)
		}
		got, err := os.ReadFile(filepath.Join(dir, notesFileName))
		if err != nil {
			t.Fatal(err)
		}
		if want := renderNotes("v5.1.0-ezop.1", "v5.1.0", r.e, "55", "1"); string(got) != want {
			t.Fatalf("notes:\n%s", got)
		}
	})
}

func TestRunNotesRefusals(t *testing.T) {
	type mutate func(w *notesWorld)
	cases := []struct {
		name  string
		check string
		edit  mutate
	}{
		{"the tag is not a Release Tag", "notes", func(w *notesWorld) { w.env["GITHUB_REF_NAME"] = "v5.1.0" }},
		{"the tag is empty", "notes", func(w *notesWorld) { w.env["GITHUB_REF_NAME"] = "" }},
		{"the tag has a trailing newline", "notes", func(w *notesWorld) { w.env["GITHUB_REF_NAME"] = inRunTag + "\n" }},
		{"GITHUB_SHA is missing", "notes", func(w *notesWorld) { delete(w.env, "GITHUB_SHA") }},
		{"GITHUB_SHA is short", "notes", func(w *notesWorld) { w.env["GITHUB_SHA"] = targetSHA[:39] }},
		{"GITHUB_SHA is upper case", "notes", func(w *notesWorld) { w.env["GITHUB_SHA"] = strings.ToUpper(strings.Repeat("ab", 20)) }},
		{"the tags of origin cannot be listed", "identity", func(w *notesWorld) { w.git.tagsErr = errors.New("boom") }},
		{"no Upstream tag exists", "base", func(w *notesWorld) { w.git.tags = map[string]string{inRunTag: targetSHA} }},
		{"the tag version is not the base", "base", func(w *notesWorld) { w.env["GITHUB_REF_NAME"] = "v5.2.0-ezop.1" }},
		{"the base tag is not an ancestor", "base", func(w *notesWorld) { w.git.ancestry = 1 }},
		{"ancestry cannot be decided", "base", func(w *notesWorld) { w.git.ancestry = 128 }},
		{"two Upstream tags share a version", "base", func(w *notesWorld) {
			w.git.tags = map[string]string{inRunTag: targetSHA, "v5.1.0": helperSHA, "v5.01.0": helperSHA}
		}},
		{"the run id is missing", "notes", func(w *notesWorld) { delete(w.env, "GITHUB_RUN_ID") }},
		{"the run id is not a number", "notes", func(w *notesWorld) { w.env["GITHUB_RUN_ID"] = "12a" }},
		{"the run id is zero", "notes", func(w *notesWorld) { w.env["GITHUB_RUN_ID"] = "0" }},
		{"the run id has a leading zero", "notes", func(w *notesWorld) { w.env["GITHUB_RUN_ID"] = "012" }},
		{"the run id is negative", "notes", func(w *notesWorld) { w.env["GITHUB_RUN_ID"] = "-3" }},
		{"the run id holds a slash", "notes", func(w *notesWorld) { w.env["GITHUB_RUN_ID"] = "1/2" }},
		{"the run id holds a line break", "notes", func(w *notesWorld) { w.env["GITHUB_RUN_ID"] = "12\nCommit: " + targetSHA }},
		{"the attempt is missing", "notes", func(w *notesWorld) { delete(w.env, "GITHUB_RUN_ATTEMPT") }},
		{"the attempt is not a number", "notes", func(w *notesWorld) { w.env["GITHUB_RUN_ATTEMPT"] = "x" }},
		{"the attempt is zero", "notes", func(w *notesWorld) { w.env["GITHUB_RUN_ATTEMPT"] = "0" }},
		{"the attempt has a leading zero", "notes", func(w *notesWorld) { w.env["GITHUB_RUN_ATTEMPT"] = "01" }},
		{"RUNNER_TEMP is missing", "notes", func(w *notesWorld) { delete(w.env, "RUNNER_TEMP") }},
		{"RUNNER_TEMP is relative", "notes", func(w *notesWorld) { w.env["RUNNER_TEMP"] = "relative/dir" }},
		{"RUNNER_TEMP does not exist", "notes", func(w *notesWorld) { w.env["RUNNER_TEMP"] = filepath.Join(w.dir, "missing") }},
		{"RUNNER_TEMP is a file", "notes", func(w *notesWorld) {
			p := filepath.Join(w.dir, "afile")
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				panic(err)
			}
			w.env["RUNNER_TEMP"] = p
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newNotesWorld(t)
			tc.edit(w)
			before := entriesOf(t, w.dir)
			code, out, errb := w.run("--in-run")
			oneLineRefusal(t, code, out, errb, tc.check)
			if after := entriesOf(t, w.dir); !slices.Equal(before, after) {
				t.Fatalf("files %v became %v", before, after)
			}
			for _, bad := range []string{"SECRET", "http"} {
				if strings.Contains(errb, bad) {
					t.Fatalf("the refusal quotes %q: %q", bad, errb)
				}
			}
		})
	}
	t.Run("control: the same world with no edit passes", func(t *testing.T) {
		w := newNotesWorld(t)
		if code, out, errb := w.run("--in-run"); code != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("a refusal of the tag is made before anything is asked of git", func(t *testing.T) {
		w := newNotesWorld(t)
		w.env["GITHUB_REF_NAME"] = "main"
		w.run("--in-run")
		if len(w.git.calls) != 0 {
			t.Fatalf("git calls %v", w.git.calls)
		}
	})
	t.Run("no reader of the environment is a set of missing values", func(t *testing.T) {
		w := newNotesWorld(t)
		d, out, errb := w.deps()
		d.getenv = nil
		oneLineRefusal(t, run([]string{"notes", "--in-run"}, d), out.String(), errb.String(), "notes")
	})
}

func TestRunNotesNeverOverwrites(t *testing.T) {
	t.Run("an existing file is kept as it is", func(t *testing.T) {
		w := newNotesWorld(t)
		if err := os.WriteFile(w.notesPath(), []byte("earlier\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, errb := w.run("--in-run")
		oneLineRefusal(t, code, out, errb, "notes")
		if got, _ := os.ReadFile(w.notesPath()); string(got) != "earlier\n" {
			t.Fatalf("the file became %q", got)
		}
	})
	t.Run("a second run refuses and keeps the first file", func(t *testing.T) {
		w := newNotesWorld(t)
		if code, out, errb := w.run("--in-run"); code != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		first, _ := os.ReadFile(w.notesPath())
		w.env["GITHUB_RUN_ATTEMPT"] = "3"
		code, out, errb := w.run("--in-run")
		oneLineRefusal(t, code, out, errb, "notes")
		if got, _ := os.ReadFile(w.notesPath()); !bytes.Equal(got, first) {
			t.Fatalf("the file changed:\n%s", got)
		}
	})
	t.Run("a symlink at the path is not followed", func(t *testing.T) {
		w := newNotesWorld(t)
		target := filepath.Join(t.TempDir(), "target")
		if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, w.notesPath()); err != nil {
			t.Fatal(err)
		}
		code, out, errb := w.run("--in-run")
		oneLineRefusal(t, code, out, errb, "notes")
		if got, _ := os.ReadFile(target); string(got) != "keep\n" {
			t.Fatalf("the target became %q", got)
		}
	})
	t.Run("a dangling symlink at the path is not followed either", func(t *testing.T) {
		w := newNotesWorld(t)
		target := filepath.Join(t.TempDir(), "absent")
		if err := os.Symlink(target, w.notesPath()); err != nil {
			t.Fatal(err)
		}
		code, out, errb := w.run("--in-run")
		oneLineRefusal(t, code, out, errb, "notes")
		if _, err := os.Lstat(target); err == nil {
			t.Fatal("the link target was created")
		}
	})
	t.Run("a result that cannot be printed is a refusal, and the notes written before it are whole", func(t *testing.T) {
		w := newNotesWorld(t)
		d, _, errb := w.deps()
		d.stdout = errWriter{}
		if code := run([]string{"notes", "--in-run"}, d); code != 1 {
			t.Fatalf("exit %d, stderr %q", code, errb)
		}
		if !strings.HasPrefix(errb.String(), "fork-release: output: ") {
			t.Fatalf("stderr %q", errb)
		}
		got, _ := os.ReadFile(w.notesPath())
		if string(got) != renderNotes(inRunTag, "v5.1.0", targetSHA, "987654321", "2") {
			t.Fatalf("notes %q", got)
		}
	})
}
