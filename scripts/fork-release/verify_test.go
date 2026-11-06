package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/roman-16/proton-cli/internal/selfmanage"
)

// ---- the two-way checksum comparison ----

func sha256Of(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// vfDownloaded is the hash of each downloaded asset of a healthy release: the
// six binaries, six archives and checksums.txt.
func vfDownloaded() map[string]string {
	hashes := map[string]string{}
	for _, n := range append(slices.Clone(wantBinaries), archiveNames()...) {
		hashes[n] = sha256Of("bytes of " + n)
	}
	hashes[checksumsName] = sha256Of("the checksum list")
	return hashes
}

// vfListed is the checksums.txt of a healthy release: every asset but itself.
func vfListed() map[string]string {
	sums := vfDownloaded()
	delete(sums, checksumsName)
	return sums
}

func TestCompareChecksums(t *testing.T) {
	t.Run("a healthy release has no exception", func(t *testing.T) {
		ex, err := compareChecksums(vfDownloaded(), vfListed(), wantBinaries)
		if err != nil || len(ex) != 0 {
			t.Fatalf("exceptions %v, err %v", ex, err)
		}
	})
	t.Run("assets that checksums.txt does not list are the exceptions, sorted", func(t *testing.T) {
		hashes := vfDownloaded()
		for _, n := range []string{"sbom.json", "Notes.txt", "extra_1"} {
			hashes[n] = sha256Of(n)
		}
		ex, err := compareChecksums(hashes, vfListed(), wantBinaries)
		if err != nil || !slices.Equal(ex, []string{"Notes.txt", "extra_1", "sbom.json"}) {
			t.Fatalf("exceptions %v, err %v", ex, err)
		}
	})
	t.Run("checksums.txt is never an exception", func(t *testing.T) {
		ex, err := compareChecksums(vfDownloaded(), vfListed(), wantBinaries)
		if err != nil || slices.Contains(ex, checksumsName) {
			t.Fatalf("exceptions %v, err %v", ex, err)
		}
	})

	refusals := map[string]func(hashes, sums map[string]string) (map[string]string, map[string]string){
		"a listed name that was not downloaded": func(h, s map[string]string) (map[string]string, map[string]string) {
			s["ghost.tar.gz"] = sha256Of("ghost")
			return h, s
		},
		"a hash that differs": func(h, s map[string]string) (map[string]string, map[string]string) {
			s[archiveNames()[0]] = sha256Of("another")
			return h, s
		},
		"a binary that is not listed": func(h, s map[string]string) (map[string]string, map[string]string) {
			delete(s, wantBinaries[3])
			return h, s
		},
		"every binary unlisted": func(h, s map[string]string) (map[string]string, map[string]string) {
			return h, map[string]string{archiveNames()[0]: h[archiveNames()[0]]}
		},
		"checksums.txt not downloaded": func(h, s map[string]string) (map[string]string, map[string]string) {
			delete(h, checksumsName)
			return h, s
		},
		"checksums.txt listing itself with another hash": func(h, s map[string]string) (map[string]string, map[string]string) {
			s[checksumsName] = sha256Of("another")
			return h, s
		},
		"a binary downloaded but unlisted and listed under a case difference": func(h, s map[string]string) (map[string]string, map[string]string) {
			delete(s, wantBinaries[0])
			s["Proton-cli_linux_amd64"] = h[wantBinaries[0]]
			return h, s
		},
		"an empty checksum list": func(h, s map[string]string) (map[string]string, map[string]string) {
			return h, map[string]string{}
		},
	}
	for name, mutate := range refusals {
		t.Run("refuses "+name, func(t *testing.T) {
			h, s := mutate(vfDownloaded(), vfListed())
			ex, err := compareChecksums(h, s, wantBinaries)
			if err == nil || ex != nil {
				t.Fatalf("accepted: %v", ex)
			}
			if !strings.HasPrefix(err.Error(), "fork-release: verify: ") {
				t.Fatalf("error %q", err)
			}
			for _, n := range []string{"ghost", "Proton-cli", sha256Of("another")} {
				if strings.Contains(err.Error(), n) {
					t.Fatalf("the error quotes %q: %v", n, err)
				}
			}
		})
	}
}

// ---- the tool identity from the tree of E ----

const (
	vfGoMod  = goodMod
	vfLock   = goodLock
	vfConfig = "version: 2\nproject_name: proton-cli\n"
	vfPin    = "v2.17.1"
)

func vfWorkflow(pin string) string {
	return "name: Fork Release\non:\n  push:\n    tags: ['v*-ezop.*']\nenv:\n  GORELEASER_PIN: " + pin + "\njobs:\n  release:\n    runs-on: x\n    steps: []\n"
}

// vfTree writes the four files into a scratch repository and commits them; mutate
// edits the map of files first. It answers the repository and the commit.
func vfTree(t *testing.T, mutate func(files map[string]string)) (*scratch, string) {
	t.Helper()
	files := map[string]string{
		"go.mod":                             vfGoMod,
		"devbox.lock":                        vfLock,
		".goreleaser.yaml":                   vfConfig,
		".github/workflows/fork-release.yml": vfWorkflow(vfPin),
		"README.md":                          "unrelated\n",
	}
	if mutate != nil {
		mutate(files)
	}
	s := newScratch(t)
	for path, content := range files {
		s.write(path, content)
	}
	return s, s.commit("tree")
}

func TestReadToolIdentity(t *testing.T) {
	t.Run("a healthy tree gives the versions the toolchain check vouches for and the two blob ids", func(t *testing.T) {
		s, e := vfTree(t, nil)
		got, err := readToolIdentity(s.adapter(), e)
		if err != nil {
			t.Fatal(err)
		}
		want := toolIdentity{
			GoVersion:         "1.27.0",
			GoreleaserVersion: "2.17.1",
			ConfigBlob:        s.git("rev-parse", e+":.goreleaser.yaml"),
			LockBlob:          s.git("rev-parse", e+":devbox.lock"),
		}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		if !blobHexPattern.MatchString(got.ConfigBlob) || !blobHexPattern.MatchString(got.LockBlob) || got.ConfigBlob == got.LockBlob {
			t.Fatalf("blobs %+v", got)
		}
	})
	t.Run("the tree of the commit is read, never the working tree", func(t *testing.T) {
		s, e := vfTree(t, nil)
		s.write("go.mod", "module m\ngo 9.9.9\n")
		s.write("devbox.lock", "{}")
		s.write(".goreleaser.yaml", "changed\n")
		s.write(".github/workflows/fork-release.yml", vfWorkflow("v9.9.9"))
		got, err := readToolIdentity(s.adapter(), e)
		if err != nil || got.GoVersion != "1.27.0" || got.GoreleaserVersion != "2.17.1" || got.ConfigBlob != s.git("rev-parse", e+":.goreleaser.yaml") {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("another commit gives its own blobs", func(t *testing.T) {
		s, e1 := vfTree(t, nil)
		s.write(".goreleaser.yaml", "version: 2\nproject_name: other\n")
		e2 := s.commit("config")
		a, err1 := readToolIdentity(s.adapter(), e1)
		b, err2 := readToolIdentity(s.adapter(), e2)
		if err1 != nil || err2 != nil || a.ConfigBlob == b.ConfigBlob || a.LockBlob != b.LockBlob {
			t.Fatalf("%+v %v / %+v %v", a, err1, b, err2)
		}
	})

	broken := map[string]func(files map[string]string){
		"no workflow":         func(f map[string]string) { delete(f, ".github/workflows/fork-release.yml") },
		"no go.mod":           func(f map[string]string) { delete(f, "go.mod") },
		"no devbox.lock":      func(f map[string]string) { delete(f, "devbox.lock") },
		"no .goreleaser.yaml": func(f map[string]string) { delete(f, ".goreleaser.yaml") },
		"a pin different from the lock": func(f map[string]string) {
			f[".github/workflows/fork-release.yml"] = vfWorkflow("v2.17.2")
		},
		"a pin without a v": func(f map[string]string) { f[".github/workflows/fork-release.yml"] = vfWorkflow("2.17.1") },
		"a lock that disagrees with go.mod": func(f map[string]string) {
			f["go.mod"] = "module m\n\ngo 1.27.1\n"
		},
		"a lock with two goreleaser packages": func(f map[string]string) {
			f["devbox.lock"] = lockWith(`"go@latest":{"version":"1.27.0"},"goreleaser@latest":{"version":"2.17.1"},"goreleaser@2":{"version":"2.17.1"}`)
		},
		"a malformed lock": func(f map[string]string) { f["devbox.lock"] = "{" },
		"a workflow that is not YAML": func(f map[string]string) {
			f[".github/workflows/fork-release.yml"] = "env: [\n"
		},
	}
	for name, mutate := range broken {
		t.Run("refuses "+name, func(t *testing.T) {
			s, e := vfTree(t, mutate)
			got, err := readToolIdentity(s.adapter(), e)
			if err == nil || got != (toolIdentity{}) {
				t.Fatalf("accepted: %+v", got)
			}
			if !strings.HasPrefix(err.Error(), "fork-release: verify: ") || strings.Contains(err.Error(), "\n") {
				t.Fatalf("error %q", err)
			}
		})
	}
	t.Run("a symlink in place of a file is not followed", func(t *testing.T) {
		s, e := vfTree(t, nil)
		// Replace the committed file with a symlink in a new commit.
		if err := os.Remove(filepath.Join(s.dir, ".goreleaser.yaml")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("devbox.lock", filepath.Join(s.dir, ".goreleaser.yaml")); err != nil {
			t.Fatal(err)
		}
		e2 := s.commit("symlink")
		if _, err := readToolIdentity(s.adapter(), e2); err == nil {
			t.Fatal("a symlink was accepted for .goreleaser.yaml")
		}
		if _, err := readToolIdentity(s.adapter(), e); err != nil {
			t.Fatalf("control: %v", err)
		}
	})
	t.Run("an unknown commit refuses", func(t *testing.T) {
		s, _ := vfTree(t, nil)
		if _, err := readToolIdentity(s.adapter(), strings.Repeat("a", 40)); err == nil {
			t.Fatal("accepted")
		}
	})
}

func TestWorkflowPin(t *testing.T) {
	t.Run("the top-level env entry is the pin", func(t *testing.T) {
		pin, err := workflowPin([]byte(vfWorkflow(vfPin)))
		if err != nil || pin != vfPin {
			t.Fatalf("%q, %v", pin, err)
		}
	})
	t.Run("a quoted string is the same string", func(t *testing.T) {
		for _, v := range []string{`"v2.17.1"`, `'v2.17.1'`} {
			if pin, err := workflowPin([]byte(vfWorkflow(v))); err != nil || pin != vfPin {
				t.Fatalf("%s: %q, %v", v, pin, err)
			}
		}
	})
	bad := map[string]string{
		"an empty file":                 "",
		"no env":                        "name: x\n",
		"an empty env":                  "env: {}\n",
		"env without the pin":           "env:\n  OTHER: v2.17.1\n",
		"the pin only in a job":         "jobs:\n  a:\n    env:\n      GORELEASER_PIN: v2.17.1\n",
		"the pin in a step":             "env:\n  OTHER: 1\njobs:\n  a:\n    steps:\n      - env:\n          GORELEASER_PIN: v2.17.1\n",
		"another case":                  "env:\n  goreleaser_pin: v2.17.1\n",
		"an empty pin":                  "env:\n  GORELEASER_PIN:\n",
		"a number":                      "env:\n  GORELEASER_PIN: 2.17\n",
		"a list":                        "env:\n  GORELEASER_PIN: [v2.17.1]\n",
		"a map":                         "env:\n  GORELEASER_PIN:\n    v: v2.17.1\n",
		"no v":                          "env:\n  GORELEASER_PIN: 2.17.1\n",
		"two parts":                     "env:\n  GORELEASER_PIN: v2.17\n",
		"a prerelease":                  "env:\n  GORELEASER_PIN: v2.17.1-rc.1\n",
		"a range":                       "env:\n  GORELEASER_PIN: ~v2.17.1\n",
		"latest":                        "env:\n  GORELEASER_PIN: latest\n",
		"a template":                    "env:\n  GORELEASER_PIN: ${{ vars.PIN }}\n",
		"a repeated pin":                "env:\n  GORELEASER_PIN: v2.17.1\n  GORELEASER_PIN: v2.17.1\n",
		"a repeated env":                "env:\n  GORELEASER_PIN: v2.17.1\nenv:\n  GORELEASER_PIN: v2.17.1\n",
		"a merge key":                   "x: &a\n  GORELEASER_PIN: v2.17.1\nenv:\n  <<: *a\n",
		"a merge key beside the pin":    "x: &a\n  OTHER: 1\nenv:\n  <<: *a\n  GORELEASER_PIN: v2.17.1\n",
		"two documents":                 "env:\n  GORELEASER_PIN: v2.17.1\n---\nenv:\n  GORELEASER_PIN: v2.17.1\n",
		"a document that is a sequence": "- GORELEASER_PIN\n",
		"not YAML":                      "env: [\n",
		"a trailing space in the pin":   "env:\n  GORELEASER_PIN: \"v2.17.1 \"\n",
	}
	for name, text := range bad {
		t.Run("refuses "+name, func(t *testing.T) {
			pin, err := workflowPin([]byte(text))
			if err == nil || pin != "" {
				t.Fatalf("accepted %q", pin)
			}
			if !strings.HasPrefix(err.Error(), "fork-release: verify: ") {
				t.Fatalf("error %q", err)
			}
		})
	}
}

// ---- the record renderer and the round trip ----

func goodFacts() verifiedFacts {
	assets := map[string]string{checksumsName: recHash}
	for _, n := range wantBinaries {
		assets[n] = recHash
	}
	return verifiedFacts{
		Tag:           recTag,
		Commit:        recCommit,
		Time:          "2026-11-06T10:20:30Z",
		Assets:        assets,
		HostPlatform:  "linux/amd64",
		ProtonVersion: strings.TrimPrefix(recTag, "v"),
		Tools: toolIdentity{
			GoVersion: "1.27.0", GoreleaserVersion: "2.17.1",
			ConfigBlob: strings.Repeat("a", 40), LockBlob: strings.Repeat("b", 40),
		},
		RunID: "123456789", RunAttempt: "2", RunConclusion: "success",
		Attested: map[string]string{
			"gate-commit": recCommit, "gates": "test-fast,lint,build,snapshot", "lint-diff": "none",
			"go-version": "1.27.0", "goreleaser-version": "2.17.1", "snapshot-version": "5.1.0-ezop.1-SNAPSHOT-0123456",
		},
	}
}

func TestRenderVerifiedRecord(t *testing.T) {
	t.Run("the record of the Story 1.4 grammar, in its field order", func(t *testing.T) {
		f := goodFacts()
		f.Exceptions = []string{"sbom.json"}
		f.Assets["sbom.json"] = recHash
		got, err := renderVerifiedRecord(f)
		if err != nil {
			t.Fatal(err)
		}
		want := renderRecord([]kv{
			{"schema", "1"}, {"tag", recTag}, {"status", "verified"}, {"time", "2026-11-06T10:20:30Z"},
			{"commit", recCommit},
			{"asset.checksums.txt", recHash},
			{"asset.proton-cli_darwin_amd64", recHash},
			{"asset.proton-cli_darwin_arm64", recHash},
			{"asset.proton-cli_linux_amd64", recHash},
			{"asset.proton-cli_linux_arm64", recHash},
			{"asset.proton-cli_windows_amd64.exe", recHash},
			{"asset.proton-cli_windows_arm64.exe", recHash},
			{"asset.sbom.json", recHash},
			{"exceptions", "sbom.json"},
			{"host-platform", "linux/amd64"},
			{"proton-version", "5.1.0-ezop.1"},
			{"goreleaser-config-blob", strings.Repeat("a", 40)},
			{"devbox-lock-blob", strings.Repeat("b", 40)},
			{"go-version", "1.27.0"},
			{"goreleaser-version", "2.17.1"},
			{"run-id", "123456789"},
			{"run-attempt", "2"},
			{"run-conclusion", "success"},
			{"attested.gate-commit", recCommit},
			{"attested.gates", "test-fast,lint,build,snapshot"},
			{"attested.lint-diff", "none"},
			{"attested.go-version", "1.27.0"},
			{"attested.goreleaser-version", "2.17.1"},
			{"attested.snapshot-version", "5.1.0-ezop.1-SNAPSHOT-0123456"},
		})
		if string(got) != string(want) {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("no exception renders an empty list", func(t *testing.T) {
		got, err := renderVerifiedRecord(goodFacts())
		if err != nil || !strings.Contains(string(got), "\nexceptions:\n") {
			t.Fatalf("%s, %v", got, err)
		}
	})
	t.Run("the record passes the Story 1.4 parser and the directory validator", func(t *testing.T) {
		f := goodFacts()
		f.Assets["Zed.txt"] = recHash
		got, err := renderVerifiedRecord(f)
		if err != nil {
			t.Fatal(err)
		}
		rec, found := parseRecord(verDir+recTag+".md", recTag, got)
		if len(found) != 0 || rec.Status != statusVerified {
			t.Fatalf("findings %v", found)
		}
		tree := fstest.MapFS{verDir + "README.md": file([]byte("# README\n")), verDir + recTag + ".md": file(got)}
		if fs := validateVerificationDir(tree); len(fs) != 0 {
			t.Fatalf("findings %v", fs)
		}
		// The status judgment over the same bytes: a valid verified record with the run it names.
		valid, verified, commit, run := judgeRecord(recTag, got)
		if !valid || !verified || commit != recCommit || run != "123456789/2" {
			t.Fatalf("judge: %v %v %q %q", valid, verified, commit, run)
		}
	})
	t.Run("asset lines are sorted bytewise", func(t *testing.T) {
		f := goodFacts()
		f.Assets["Zed.txt"] = recHash
		f.Assets["aaa"] = recHash
		got, err := renderVerifiedRecord(f)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, l := range strings.Split(string(got), "\n") {
			if n, ok := strings.CutPrefix(l, "asset."); ok {
				names = append(names, strings.SplitN(n, ":", 2)[0])
			}
		}
		if !slices.IsSorted(names) || names[0] != "Zed.txt" || len(names) != 9 {
			t.Fatalf("names %v", names)
		}
	})
	t.Run("the same facts give the same bytes", func(t *testing.T) {
		a, _ := renderVerifiedRecord(goodFacts())
		b, _ := renderVerifiedRecord(goodFacts())
		if string(a) != string(b) {
			t.Fatal("two renderings differ")
		}
	})

	refusals := map[string]func(f *verifiedFacts){
		"an asset name the grammar rejects": func(f *verifiedFacts) { f.Assets["bad name"] = recHash },
		"an asset name with a line feed":    func(f *verifiedFacts) { f.Assets["a\nexceptions: x"] = recHash },
		"an asset name that is too long":    func(f *verifiedFacts) { f.Assets[strings.Repeat("a", 101)] = recHash },
		"an asset name starting with a dot": func(f *verifiedFacts) { f.Assets[".hidden"] = recHash },
		"a hash that is not sha256":         func(f *verifiedFacts) { f.Assets[checksumsName] = "abc" },
		"an exception that is no asset":     func(f *verifiedFacts) { f.Exceptions = []string{"ghost"} },
		"unsorted exceptions": func(f *verifiedFacts) {
			f.Assets["b"], f.Assets["a"] = recHash, recHash
			f.Exceptions = []string{"b", "a"}
		},
		"a version that is not the tag's": func(f *verifiedFacts) { f.ProtonVersion = "5.1.0" },
		"a conclusion other than success": func(f *verifiedFacts) { f.RunConclusion = "failure" },
		"a gate-commit other than commit": func(f *verifiedFacts) { f.Attested["gate-commit"] = strings.Repeat("c", 40) },
		"gates other than the fixed list": func(f *verifiedFacts) { f.Attested["gates"] = "test-fast,lint,build" },
		"a lint diff other than none":     func(f *verifiedFacts) { f.Attested["lint-diff"] = "some" },
		"a missing attested key":          func(f *verifiedFacts) { delete(f.Attested, "snapshot-version") },
		"an extra attested key":           func(f *verifiedFacts) { f.Attested["extra"] = "x" },
		"an attested value with a line feed": func(f *verifiedFacts) {
			f.Attested["snapshot-version"] = "x\nrun-id: 1"
		},
		"a bad time":             func(f *verifiedFacts) { f.Time = "2026-11-06 10:20:30" },
		"a tag that is no tag":   func(f *verifiedFacts) { f.Tag = "v5.1.0" },
		"a commit that is short": func(f *verifiedFacts) { f.Commit = "abc" },
		"a run id of zero":       func(f *verifiedFacts) { f.RunID = "0" },
		"an empty host":          func(f *verifiedFacts) { f.HostPlatform = "" },
		"a bad go version":       func(f *verifiedFacts) { f.Tools.GoVersion = "go1.27" },
		"a bad blob":             func(f *verifiedFacts) { f.Tools.LockBlob = "xyz" },
	}
	for name, mutate := range refusals {
		t.Run("refuses "+name, func(t *testing.T) {
			f := goodFacts()
			mutate(&f)
			got, err := renderVerifiedRecord(f)
			if err == nil || got != nil {
				t.Fatalf("rendered:\n%s", got)
			}
			if !strings.HasPrefix(err.Error(), "fork-release: verify: ") || strings.Contains(err.Error(), "\n") {
				t.Fatalf("error %q", err)
			}
			for _, secret := range []string{"bad name", "SNAPSHOT", "exceptions: x"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("the error quotes %q: %v", secret, err)
				}
			}
		})
	}
	t.Run("control: the facts without a mutation pass", func(t *testing.T) {
		if _, err := renderVerifiedRecord(goodFacts()); err != nil {
			t.Fatal(err)
		}
	})
}

// ---- the verify TAG GATES command over fakes ----

const (
	vfE       = recCommit // the commit the tag peels to
	vfRunID   = "123456789"
	vfWFID    = 77
	vfSumsID  = 200
	vfExtraID = 400
	// vfFail as the body of a release read makes that read fail.
	vfFail = "FAIL"
)

func vfClock() time.Time { return time.Date(2026, 11, 6, 10, 20, 30, 0, time.UTC) }

// vfAPI is the GitHub reader of a verify run: the fake of the record lookup, the
// releases list (the page-1 answer of each successive read, the last repeating),
// the run reads, and the downloads. Every read is logged by kind.
type vfAPI struct {
	*fakeAPI
	files     map[int64][]byte
	failDL    map[int64]bool
	downloads []string
	rel       []string
	relCalls  int
	ev        *[]string
}

func (a *vfAPI) Get(endpoint string) ([]byte, error) {
	kind := "other"
	switch {
	case strings.HasPrefix(endpoint, "git/"):
		kind = "record"
	case strings.HasPrefix(endpoint, "releases?"):
		kind = "release"
	case endpoint == workflowEndpoint():
		kind = "workflow"
	case strings.Contains(endpoint, "/attempts/"):
		kind = "attempt"
	case strings.Contains(endpoint, "/runs?"):
		kind = "listing"
	case strings.HasPrefix(endpoint, "actions/runs/"):
		kind = "run"
	}
	*a.ev = append(*a.ev, kind)
	if endpoint == "releases?per_page=100&page=1" {
		a.calls = append(a.calls, endpoint)
		body := a.rel[min(a.relCalls, len(a.rel)-1)]
		a.relCalls++
		if body == vfFail {
			return nil, errors.New("gh failed " + relMarker)
		}
		return []byte(body), nil
	}
	return a.fakeAPI.Get(endpoint)
}

func (a *vfAPI) Download(endpoint string) ([]byte, error) {
	*a.ev = append(*a.ev, "download")
	a.downloads = append(a.downloads, endpoint)
	id, err := strconv.ParseInt(strings.TrimPrefix(endpoint, "releases/assets/"), 10, 64)
	if err != nil || a.failDL[id] {
		return nil, errors.New("gh failed " + relMarker)
	}
	data, ok := a.files[id]
	if !ok {
		return nil, errors.New("no such asset " + relMarker)
	}
	return data, nil
}

// vfGit is the fake git with the tags of origin, the ancestry of E apart from the
// ancestry of the helper's own commit, and the reads logged.
type vfGit struct {
	*fakeGit
	tags    map[string]string
	tagsErr error
	eStatus int
	eErr    error
	ev      *[]string
}

func (g *vfGit) OriginTags() (map[string]string, error) {
	*g.ev = append(*g.ev, "tag")
	g.calls = append(g.calls, "origin tags")
	return g.tags, g.tagsErr
}

func (g *vfGit) IsAncestor(ancestor, descendant string) (int, error) {
	if ancestor == helperSHA {
		return g.fakeGit.IsAncestor(ancestor, descendant)
	}
	*g.ev = append(*g.ev, "ancestry")
	g.calls = append(g.calls, "ancestor "+ancestor+" "+descendant)
	return g.eStatus, g.eErr
}

func (g *vfGit) Tree(commit string, paths []string) ([]treeEntry, error) {
	*g.ev = append(*g.ev, "tools")
	return g.fakeGit.Tree(commit, paths)
}

// vfWorld is one state of the world for verify TAG GATES: the record at the tip,
// the release of the tag with its assets, the run, the tree of E and the capture
// file. Fields are changed before build; the built environment's fakes can be
// changed after it.
type vfWorld struct {
	t        *testing.T
	tag      string
	runID    string
	named    string // the attempt the Run line names
	current  int    // the attempt the run is at
	body     string // the notes; empty means the default for E, the run and the attempt
	record   func() *fakeAPI
	assets   []assetSpec
	files    map[int64][]byte
	unlisted map[string]bool // assets checksums.txt does not list
	noSums   bool
	sums     *string // the raw checksums.txt
	others   []string
	listing  []string // runs of the listing; nil means the run itself
	gates    string
}

func newVfWorld(t *testing.T) *vfWorld {
	t.Helper()
	w := &vfWorld{t: t, tag: recTag, runID: vfRunID, named: "1", current: 2, record: stAbsentWorld,
		files: map[int64][]byte{}, unlisted: map[string]bool{"sbom.json": true}, gates: goodCapture()}
	for i, n := range wantBinaries {
		w.addAsset(n, binaryID(i), "bytes of "+n)
	}
	for i, n := range archiveNames() {
		w.addAsset(n, int64(301+i), "bytes of "+n)
	}
	w.addAsset("sbom.json", vfExtraID, "bytes of sbom")
	return w
}

func (w *vfWorld) addAsset(name string, id int64, data string) {
	w.assets = append(w.assets, assetSpec{name, id, "uploaded"})
	w.files[id] = []byte(data)
}

// hostName is the asset the host binary is.
func hostName(t *testing.T) string {
	t.Helper()
	name, err := selfmanage.AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("this host has no asset: %v", err)
	}
	return name
}

func (w *vfWorld) idOf(name string) int64 {
	for _, a := range w.assets {
		if a.Name == name {
			return a.ID
		}
	}
	w.t.Fatalf("no asset %s", name)
	return 0
}

type vfEnv struct {
	w     *vfWorld
	d     guardDeps
	api   *vfAPI
	git   *vfGit
	out   *bytes.Buffer
	errb  *bytes.Buffer
	ev    *[]string
	calls *[]runCall
	gates string
	tmp   string
	cwd   string
}

func (w *vfWorld) build() vfEnv {
	t := w.t
	t.Helper()
	ev := &[]string{}

	assets := slices.Clone(w.assets)
	files := map[int64][]byte{}
	for id, data := range w.files {
		files[id] = data
	}
	sums := ""
	if w.sums != nil {
		sums = *w.sums
	} else {
		var b strings.Builder
		for _, a := range w.assets {
			if !w.unlisted[a.Name] {
				fmt.Fprintf(&b, "%s  %s\n", sha256Of(string(w.files[a.ID])), a.Name)
			}
		}
		sums = b.String()
	}
	if !w.noSums {
		assets = append(assets, assetSpec{checksumsName, vfSumsID, "uploaded"})
		files[vfSumsID] = []byte(sums)
	}
	body := w.body
	if body == "" {
		body = stBody(vfE, w.runID+"/"+w.named)
	}

	world := w.record()
	world.resp["releases?per_page=100&page=2"] = "[]"
	world.resp[workflowEndpoint()] = fmt.Sprintf(`{"id":%d,"name":"Fork Release","path":".github/workflows/fork-release.yml"}`, vfWFID)
	id, _ := strconv.Atoi(w.runID)
	named, _ := strconv.Atoi(w.named)
	world.resp[attemptEndpoint(w.runID, w.named)] = rnRun(id, named, "completed", "success", vfE, "push", vfWFID)
	run := rnRun(id, w.current, "completed", "success", vfE, "push", vfWFID)
	world.resp[runEndpoint(w.runID)] = run
	listing := w.listing
	if listing == nil {
		listing = []string{run}
	}
	world.resp[runsEndpoint(vfE, 1)] = rnListing(len(listing), listing...)

	first := relPage(append([]string{relEntryAssets(w.tag, false, body, assets)}, w.others...)...)
	api := &vfAPI{fakeAPI: world, files: files, failDL: map[int64]bool{}, rel: []string{first}, ev: ev}

	tree := fstest.MapFS{
		"go.mod":                             file([]byte(vfGoMod)),
		"devbox.lock":                        file([]byte(vfLock)),
		".goreleaser.yaml":                   file([]byte(vfConfig)),
		".github/workflows/fork-release.yml": file([]byte(vfWorkflow(vfPin))),
		"README.md":                          file([]byte("unrelated\n")),
	}
	git := &vfGit{fakeGit: newFakeGit(tree), tags: map[string]string{w.tag: vfE}, ev: ev}
	d, out, errb := testDeps(git.fakeGit)
	d.git = git
	d.gh = api
	d.clock = vfClock
	var calls []runCall
	version := strings.TrimPrefix(w.tag, "v")
	d.runProgram = func(path string, args, env []string) ([]byte, error) {
		*ev = append(*ev, "exec")
		return recordingRunner(&calls, stampJSON(version), nil)(path, args, env)
	}

	gates := filepath.Join(t.TempDir(), "gates.txt")
	if err := os.WriteFile(gates, []byte(w.gates), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp, cwd := t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Chdir(cwd)
	return vfEnv{w: w, d: d, api: api, git: git, out: out, errb: errb, ev: ev, calls: &calls, gates: gates, tmp: tmp, cwd: cwd}
}

func (e vfEnv) args() []string { return []string{"verify", e.w.tag, e.gates} }

func (e vfEnv) run() int { return run(e.args(), e.d) }

// nothingLeft requires the temporary directory and the working directory to be
// empty again: the command wrote no file and removed what it made.
func (e vfEnv) nothingLeft(t *testing.T) {
	t.Helper()
	for _, dir := range []string{e.tmp, e.cwd} {
		if names := entriesOf(t, dir); len(names) != 0 {
			t.Fatalf("left in %s: %v", dir, names)
		}
	}
}

// pass runs the command and requires the printed record.
func (e vfEnv) pass(t *testing.T) string {
	t.Helper()
	if code := e.run(); code != 0 {
		t.Fatalf("exit %d; stdout %q stderr %q", code, e.out, e.errb)
	}
	if e.errb.Len() != 0 {
		t.Fatalf("stderr %q", e.errb)
	}
	e.nothingLeft(t)
	return e.out.String()
}

// refuse runs the command and requires one refusal line and nothing else.
func (e vfEnv) refuse(t *testing.T) string {
	t.Helper()
	if code := e.run(); code != 1 {
		t.Fatalf("exit %d; stdout %q stderr %q", code, e.out, e.errb)
	}
	line := e.errb.String()
	if !strings.HasPrefix(line, "fork-release: ") || strings.Count(line, "\n") != 1 || e.out.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", e.out, line)
	}
	for _, bad := range []string{relMarker, rnMarker, "SECRET-BINARY-OUTPUT", "SECRET-URL-MARKER", "example.invalid", "http", capCommit} {
		if strings.Contains(line, bad) {
			t.Fatalf("the refusal quotes %q: %q", bad, line)
		}
	}
	e.nothingLeft(t)
	return line
}

func TestVerifyPrintsTheRecord(t *testing.T) {
	e := newVfWorld(t).build()
	out := e.pass(t)

	rec, found := parseRecord(verDir+recTag+".md", recTag, []byte(out))
	if len(found) != 0 || rec.Status != statusVerified {
		t.Fatalf("findings %v\n%s", found, out)
	}
	tree := fstest.MapFS{verDir + "README.md": file([]byte("# README\n")), verDir + recTag + ".md": file([]byte(out))}
	if fs := validateVerificationDir(tree); len(fs) != 0 {
		t.Fatalf("findings %v", fs)
	}
	v := rec.Values
	for key, want := range map[string]string{
		"schema": "1", "tag": recTag, "status": "verified", "time": "2026-11-06T10:20:30Z", "commit": vfE,
		"exceptions": "sbom.json", "host-platform": runtime.GOOS + "/" + runtime.GOARCH, "proton-version": "5.1.0-ezop.1",
		"go-version": "1.27.0", "goreleaser-version": "2.17.1",
		"run-id": vfRunID, "run-attempt": "1", "run-conclusion": "success",
		"attested.gate-commit": capCommit, "attested.gates": "test-fast,lint,build,snapshot", "attested.lint-diff": "none",
		"attested.go-version": "1.27.0", "attested.goreleaser-version": "2.17.1",
		"attested.snapshot-version": "5.1.0-ezop.1-SNAPSHOT-0123456",
	} {
		if v[key] != want {
			t.Errorf("%s = %q, want %q", key, v[key], want)
		}
	}
	// One asset line per downloaded asset, checksums.txt and the extra included.
	var assets []string
	for _, l := range strings.Split(out, "\n") {
		if n, ok := strings.CutPrefix(l, "asset."); ok {
			name, hash, _ := strings.Cut(n, ": ")
			assets = append(assets, name)
			if want := sha256Of(string(e.api.files[e.w.idOf0(name)])); hash != want {
				t.Errorf("asset %s has hash %s, want %s", name, hash, want)
			}
		}
	}
	if len(assets) != 14 || !slices.IsSorted(assets) || !slices.Contains(assets, checksumsName) || !slices.Contains(assets, "sbom.json") {
		t.Fatalf("assets %v", assets)
	}
	// The two blobs are those of the tree of E.
	for oid, name := range e.git.names {
		switch name {
		case ".goreleaser.yaml":
			if v["goreleaser-config-blob"] != oid {
				t.Errorf("config blob %q, want %q", v["goreleaser-config-blob"], oid)
			}
		case "devbox.lock":
			if v["devbox-lock-blob"] != oid {
				t.Errorf("lock blob %q, want %q", v["devbox-lock-blob"], oid)
			}
		}
	}
}

// idOf0 is idOf for a name of the built environment's files, which also knows
// checksums.txt.
func (w *vfWorld) idOf0(name string) int64 {
	if name == checksumsName {
		return vfSumsID
	}
	return w.idOf(name)
}

func TestVerifyReadsEverythingBeforePrintingAndInOrder(t *testing.T) {
	e := newVfWorld(t).build()
	e.pass(t)
	want := []string{"record", "tag", "release", "ancestry", "tools", "download", "exec", "workflow", "attempt", "run", "release", "listing"}
	if got := kinds(*e.ev); !slices.Equal(got, want) {
		t.Fatalf("reads %v, want %v", got, want)
	}
	if n := len(e.api.downloads); n != 14 {
		t.Fatalf("%d downloads", n)
	}
	seen := map[string]bool{}
	for _, d := range e.api.downloads {
		if !assetPattern.MatchString(d) || seen[d] || strings.Contains(d, "dist") {
			t.Fatalf("download %q", d)
		}
		seen[d] = true
	}
	if n := e.api.relCalls; n != 2 {
		t.Fatalf("%d release lookups", n)
	}
	if n := countOf(e.git.calls, "fetch"); n != 1 {
		t.Fatalf("%d fetches", n)
	}
	if n := countOf(e.git.calls, "origin tags"); n != 1 {
		t.Fatalf("%d tag reads", n)
	}
	// The ancestry of E is asked of the fetched tip, after the helper's own.
	wantAnc := "ancestor " + vfE + " " + ezopTipSHA
	if !slices.Contains(e.git.calls, wantAnc) {
		t.Fatalf("calls %v", e.git.calls)
	}
	// The last two reads are the release and the listing, in that order.
	n := len(*e.ev)
	if (*e.ev)[n-1] != "listing" || (*e.ev)[n-2] != "release" {
		t.Fatalf("the last reads are %v", (*e.ev)[n-3:])
	}
}

func TestVerifyRoundTripThroughStatus(t *testing.T) {
	out := newVfWorld(t).build().pass(t)
	c := stVerified()
	c.record = stRecordWorld([]byte(out))
	env := c.build(t)
	code := run([]string{"verify", "--status", recTag}, env.d)
	if code != 0 || env.out.String() != statusLines("verified", "Published-Same") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, env.out, env.errb)
	}
}

func TestVerifyWithNoExtraAssetHasNoException(t *testing.T) {
	w := newVfWorld(t)
	w.assets = slices.DeleteFunc(w.assets, func(a assetSpec) bool { return a.Name == "sbom.json" })
	out := w.build().pass(t)
	if !strings.Contains(out, "\nexceptions:\n") || strings.Contains(out, "sbom.json") {
		t.Fatalf("record:\n%s", out)
	}
	if _, found := parseRecord(verDir+recTag+".md", recTag, []byte(out)); len(found) != 0 {
		t.Fatalf("findings %v", found)
	}
}

func TestVerifyTwoReleasesAtOneCommit(t *testing.T) {
	const tagB = "v5.1.0-ezop.2"
	mk := func(tag, runID, otherTag, otherRun string, listing ...string) vfEnv {
		w := newVfWorld(t)
		w.tag, w.runID = tag, runID
		w.others = []string{relEntryAssets(otherTag, false, stBody(vfE, otherRun+"/1"), nil)}
		w.listing = listing
		e := w.build()
		e.git.tags = map[string]string{recTag: vfE, tagB: vfE}
		return e
	}
	completed := func(id int) string { return rnRun(id, 2, "completed", "success", vfE, "push", vfWFID) }
	for tag, runs := range map[string][2]string{recTag: {"111", "222"}, tagB: {"222", "111"}} {
		e := mk(tag, runs[0], map[string]string{recTag: tagB, tagB: recTag}[tag], runs[1], completed(111), completed(222))
		out := e.pass(t)
		rec, found := parseRecord(verDir+tag+".md", tag, []byte(out))
		if len(found) != 0 || rec.Values["tag"] != tag || rec.Values["run-id"] != runs[0] || rec.Values["proton-version"] != strings.TrimPrefix(tag, "v") {
			t.Fatalf("%s: findings %v, tag %q, run %q", tag, found, rec.Values["tag"], rec.Values["run-id"])
		}
	}
	// A run of the other tag still in progress at the commit is not over for either.
	for tag, runs := range map[string][2]string{recTag: {"111", "222"}, tagB: {"222", "111"}} {
		e := mk(tag, runs[0], map[string]string{recTag: tagB, tagB: recTag}[tag], runs[1],
			completed(111), rnRun(222, 1, "in_progress", "", vfE, "push", vfWFID))
		if tag == tagB {
			e = mk(tag, runs[0], recTag, runs[1], rnRun(111, 1, "in_progress", "", vfE, "push", vfWFID), completed(222))
		}
		if line := e.refuse(t); !strings.HasPrefix(line, "fork-release: verify: ") {
			t.Fatalf("%s: %q", tag, line)
		}
	}
}

// ---- refusals, each beside a healthy control ----

func vfOther(tag string, draft bool, body string) string {
	return relEntryAssets(tag, draft, body, nil)
}

type vfCase struct {
	name  string
	world func(w *vfWorld)
	env   func(e *vfEnv)
}

func vfRefusals(t *testing.T) []vfCase {
	run := func(id, attempt int, status, concl, head, event string, wf int) string {
		return rnRun(id, attempt, status, concl, head, event, wf)
	}
	id := 123456789
	setRun := func(endpoint, body string) func(e *vfEnv) {
		return func(e *vfEnv) { e.api.resp[endpoint] = body }
	}
	attemptEP := attemptEndpoint(vfRunID, "1")
	badBody := func(body string) vfCase {
		return vfCase{name: "the notes: " + strings.ReplaceAll(body, "\n", "|"), world: func(w *vfWorld) { w.body = body }}
	}
	cases := []vfCase{
		// the record
		{name: "a record is Present", world: func(w *vfWorld) { w.record = stRecordWorld(recordFor(recTag, false)) }},
		{name: "a verified record is Present", world: func(w *vfWorld) { w.record = stRecordWorld(recordFor(recTag, true)) }},
		{name: "a directory is at the record path", world: func(w *vfWorld) {
			w.record = stEntryWorld(lkEntry{recTag + ".md", "040000", "tree", lkOtherSHA})
		}},
		{name: "the record lookup is Unknown", world: func(w *vfWorld) {
			w.record = func() *fakeAPI { a := lkWorld(nil, nil); a.fail[epRef] = true; return a }
		}},
		// the release
		{name: "no release", env: func(e *vfEnv) { e.api.rel = []string{"[]"} }},
		{name: "a draft release", env: func(e *vfEnv) { e.api.rel = []string{relPage(vfOther(recTag, true, stBody(vfE, "1/1")))} }},
		{name: "a release naming another commit", env: func(e *vfEnv) {
			e.api.rel = []string{relPage(vfOther(recTag, false, stBody(rnOther, "1/1")))}
		}},
		{name: "a release with no Commit line", env: func(e *vfEnv) {
			e.api.rel = []string{relPage(vfOther(recTag, false, "Run: 1/1\n"))}
		}},
		{name: "the releases list cannot be read", env: func(e *vfEnv) { e.api.rel = []string{vfFail} }},
		{name: "the tag is listed twice", env: func(e *vfEnv) {
			e.api.rel = []string{relPage(vfOther(recTag, false, stBody(vfE, "1/1")), vfOther(recTag, false, stBody(vfE, "1/1")))}
		}},
		// the tag and the ancestry
		{name: "the tag is absent from origin", env: func(e *vfEnv) { e.git.tags = map[string]string{} }},
		{name: "the read of the tag failed", env: func(e *vfEnv) { e.git.tags, e.git.tagsErr = nil, errors.New("ls-remote failed "+secretURL) }},
		{name: "the tag peels to another commit than the release names", env: func(e *vfEnv) { e.git.tags = map[string]string{recTag: rnOther} }},
		{name: "E is not an ancestor of origin/ezop", env: func(e *vfEnv) { e.git.eStatus = 1 }},
		{name: "the ancestry of E is undecided", env: func(e *vfEnv) { e.git.eStatus = 128 }},
		{name: "the ancestry of E cannot be asked", env: func(e *vfEnv) { e.git.eErr = errors.New("boom " + secretURL) }},
		// the capture
		{name: "an empty capture", world: func(w *vfWorld) { w.gates = "" }},
		{name: "an oversized capture", world: func(w *vfWorld) { w.gates = goodCapture() + strings.Repeat("#", 64<<10) }},
		{name: "a malformed capture", world: func(w *vfWorld) { w.gates = "gates test-fast\n" }},
		{name: "a capture with a repeated key", world: func(w *vfWorld) { w.gates = goodCapture() + "lint-diff: none\n" }},
		{name: "a capture with a missing key", world: func(w *vfWorld) { w.gates = strings.Replace(goodCapture(), "lint-diff: none\n", "", 1) }},
		{name: "a capture with an unknown key", world: func(w *vfWorld) { w.gates = goodCapture() + "extra: 1\n" }},
		{name: "a capture whose gate-commit is not E", world: func(w *vfWorld) {
			w.gates = strings.Replace(goodCapture(), "gate-commit: "+capCommit, "gate-commit: "+rnOther, 1)
		}},
		{name: "a capture with other gates", world: func(w *vfWorld) {
			w.gates = strings.Replace(goodCapture(), "test-fast,lint,build,snapshot", "lint,test-fast,build,snapshot", 1)
		}},
		{name: "a capture with a lint diff", world: func(w *vfWorld) { w.gates = strings.Replace(goodCapture(), "lint-diff: none", "lint-diff: 3 files", 1) }},
		{name: "a capture with a bad Go version", world: func(w *vfWorld) {
			w.gates = strings.Replace(goodCapture(), "go-version: 1.27.0", "go-version: go1.27", 1)
		}},
		{name: "a capture with an empty value", world: func(w *vfWorld) {
			w.gates = strings.Replace(goodCapture(), "snapshot-version: 5.1.0-ezop.1-SNAPSHOT-0123456", "snapshot-version:", 1)
		}},
		{name: "the capture file is missing", env: func(e *vfEnv) { e.gates = filepath.Join(e.cwd, "missing.txt") }},
		{name: "the capture is a directory", env: func(e *vfEnv) { e.gates = e.cwd }},
		// the tool identity
		{name: "no workflow in the tree of E", env: func(e *vfEnv) { delete(e.git.tree, ".github/workflows/fork-release.yml") }},
		{name: "a workflow without a pin", env: func(e *vfEnv) {
			e.git.tree[".github/workflows/fork-release.yml"] = file([]byte("name: x\n"))
		}},
		{name: "a pin different from the lock", env: func(e *vfEnv) {
			e.git.tree[".github/workflows/fork-release.yml"] = file([]byte(vfWorkflow("v2.17.2")))
		}},
		{name: "a pin that is repeated", env: func(e *vfEnv) {
			e.git.tree[".github/workflows/fork-release.yml"] = file([]byte("env:\n  GORELEASER_PIN: v2.17.1\n  GORELEASER_PIN: v2.17.1\n"))
		}},
		{name: "a lock that disagrees with go.mod", env: func(e *vfEnv) { e.git.tree["go.mod"] = file([]byte("module m\n\ngo 1.28.0\n")) }},
		{name: "no go.mod", env: func(e *vfEnv) { delete(e.git.tree, "go.mod") }},
		{name: "no devbox.lock", env: func(e *vfEnv) { delete(e.git.tree, "devbox.lock") }},
		{name: "no .goreleaser.yaml", env: func(e *vfEnv) { delete(e.git.tree, ".goreleaser.yaml") }},
		{name: "the tree cannot be listed", env: func(e *vfEnv) { e.git.treeErr = errors.New("boom " + secretURL) }},
		// the assets
		{name: "an asset that is not uploaded", world: func(w *vfWorld) { w.assets[len(w.assets)-1].State = "starter" }},
		{name: "a binary that is not uploaded", world: func(w *vfWorld) { w.assets[2].State = "starter" }},
		{name: "no checksums.txt", world: func(w *vfWorld) { w.noSums = true }},
		{name: "an asset list that cannot be trusted", world: func(w *vfWorld) { w.assets = append(w.assets, assetSpec{wantBinaries[0], 999, "uploaded"}) }},
		{name: "a failed download of a binary", env: func(e *vfEnv) { e.api.failDL[binaryID(2)] = true }},
		{name: "a failed download of an archive", env: func(e *vfEnv) { e.api.failDL[301] = true }},
		{name: "a failed download of checksums.txt", env: func(e *vfEnv) { e.api.failDL[vfSumsID] = true }},
		{name: "a failed download of the extra asset", env: func(e *vfEnv) { e.api.failDL[vfExtraID] = true }},
		{name: "a checksums.txt that is malformed", world: func(w *vfWorld) { s := "not a checksum list\n"; w.sums = &s }},
		{name: "a checksums.txt that is empty", world: func(w *vfWorld) { s := ""; w.sums = &s }},
		{name: "a listed name that is not an asset", world: func(w *vfWorld) {
			s := strings.Repeat("ab", 32) + "  ghost.tar.gz\n"
			w.sums = &s
		}},
		{name: "a hash that does not match", env: func(e *vfEnv) { e.api.files[binaryID(1)] = []byte("tampered") }},
		{name: "a hash that does not match for an archive", env: func(e *vfEnv) { e.api.files[302] = []byte("tampered") }},
		{name: "a binary that is not listed", world: func(w *vfWorld) { w.unlisted[wantBinaries[3]] = true }},
		{name: "every binary unlisted", world: func(w *vfWorld) {
			for _, n := range wantBinaries {
				w.unlisted[n] = true
			}
		}},
		{name: "the host asset missing", world: func(w *vfWorld) {
			host := hostName(t)
			w.assets = slices.DeleteFunc(w.assets, func(a assetSpec) bool { return a.Name == host })
		}},
		{name: "an exception the record cannot hold", world: func(w *vfWorld) {
			w.addAsset("bad name.txt", 500, "x")
			w.unlisted["bad name.txt"] = true
		}},
		{name: "an asset name that is too long for the record", world: func(w *vfWorld) {
			w.addAsset(strings.Repeat("a", 101), 501, "x")
			w.unlisted[strings.Repeat("a", 101)] = true
		}},
		// the host binary
		{name: "the host binary reports the Upstream version", env: func(e *vfEnv) {
			e.d.runProgram = recordingRunner(e.calls, stampJSON("5.1.0"), nil)
		}},
		{name: "the host binary reports a v prefix", env: func(e *vfEnv) {
			e.d.runProgram = recordingRunner(e.calls, stampJSON("v5.1.0-ezop.1"), nil)
		}},
		{name: "the host binary prints text", env: func(e *vfEnv) {
			e.d.runProgram = recordingRunner(e.calls, "proton-cli version 5.1.0-ezop.1\nSECRET-BINARY-OUTPUT", nil)
		}},
		{name: "the host binary prints nothing", env: func(e *vfEnv) { e.d.runProgram = recordingRunner(e.calls, "", nil) }},
		{name: "the host binary fails", env: func(e *vfEnv) {
			e.d.runProgram = recordingRunner(e.calls, "SECRET-BINARY-OUTPUT", errors.New("exit 3 SECRET-BINARY-OUTPUT"))
		}},
		// the Run line
		badBody("Commit: " + vfE + "\n"),
		badBody("Commit: " + vfE + "\nRun: 123456789/1\nRun: 123456789/1\n"),
		badBody("Commit: " + vfE + "\nRun: abc\n"),
		badBody("Commit: " + vfE + "\nRun: 123456789\n"),
		badBody("Commit: " + vfE + "\nRun: 123456789/1/1\n"),
		badBody("Commit: " + vfE + "\nRun: 0/1\n"),
		badBody("Commit: " + vfE + "\nRun: 123456789/01\n"),
		badBody("Commit: " + vfE + "\nRun:  123456789/1\n"),
		badBody("Commit: " + vfE + "\nRun: 123456789/1 \n"),
		// the workflow, the attempt and the run
		{name: "the workflow cannot be read", env: func(e *vfEnv) { e.api.fail[workflowEndpoint()] = true }},
		{name: "the workflow answer has no id", env: setRun(workflowEndpoint(), `{"name":"x"}`)},
		{name: "the attempt cannot be read", env: func(e *vfEnv) { e.api.fail[attemptEP] = true }},
		{name: "the attempt is in progress", env: setRun(attemptEP, run(id, 1, "in_progress", "", vfE, "push", vfWFID))},
		{name: "the attempt is queued", env: setRun(attemptEP, run(id, 1, "queued", "", vfE, "push", vfWFID))},
		{name: "the attempt failed", env: setRun(attemptEP, run(id, 1, "completed", "failure", vfE, "push", vfWFID))},
		{name: "the attempt was cancelled", env: setRun(attemptEP, run(id, 1, "completed", "cancelled", vfE, "push", vfWFID))},
		{name: "the attempt has no conclusion", env: setRun(attemptEP, run(id, 1, "completed", "", vfE, "push", vfWFID))},
		{name: "the attempt is of another commit", env: setRun(attemptEP, run(id, 1, "completed", "success", rnOther, "push", vfWFID))},
		{name: "the attempt is of another event", env: setRun(attemptEP, run(id, 1, "completed", "success", vfE, "workflow_dispatch", vfWFID))},
		{name: "the attempt is of another workflow", env: setRun(attemptEP, run(id, 1, "completed", "success", vfE, "push", vfWFID+1))},
		{name: "the attempt is of another run", env: setRun(attemptEP, run(id+1, 1, "completed", "success", vfE, "push", vfWFID))},
		{name: "the attempt answered is another attempt", env: setRun(attemptEP, run(id, 2, "completed", "success", vfE, "push", vfWFID))},
		{name: "the attempt has no workflow id", env: setRun(attemptEP, `{"id":123456789,"run_attempt":1,"status":"completed","conclusion":"success","head_sha":"`+vfE+`","event":"push"}`)},
		{name: "the run cannot be read", env: func(e *vfEnv) { e.api.fail[runEndpoint(vfRunID)] = true }},
		{name: "the run is below the named attempt", world: func(w *vfWorld) { w.named, w.current = "3", 2 }},
		{name: "the run answered is another run", env: setRun(runEndpoint(vfRunID), run(id+1, 2, "completed", "success", vfE, "push", vfWFID))},
		// the last two reads
		{name: "the release is gone on the last read", env: func(e *vfEnv) { e.api.rel = append(e.api.rel, "[]") }},
		{name: "the release became a draft on the last read", env: func(e *vfEnv) {
			e.api.rel = append(e.api.rel, relPage(vfOther(recTag, true, stBody(vfE, "123456789/1"))))
		}},
		{name: "the release names another commit on the last read", env: func(e *vfEnv) {
			e.api.rel = append(e.api.rel, relPage(vfOther(recTag, false, stBody(rnOther, "123456789/1"))))
		}},
		{name: "the Run line changed on the last read", env: func(e *vfEnv) {
			e.api.rel = append(e.api.rel, relPage(vfOther(recTag, false, stBody(vfE, "123456789/2"))))
		}},
		{name: "the Run line was lost on the last read", env: func(e *vfEnv) {
			e.api.rel = append(e.api.rel, relPage(vfOther(recTag, false, "Commit: "+vfE+"\n")))
		}},
		{name: "the last read of the release failed", env: func(e *vfEnv) { e.api.rel = append(e.api.rel, vfFail) }},
		{name: "a run for E is not over", world: func(w *vfWorld) {
			w.listing = []string{rnRun(1, 1, "completed", "success", vfE, "push", vfWFID), rnRun(2, 1, "in_progress", "", vfE, "push", vfWFID)}
		}},
		{name: "a run for E is queued", world: func(w *vfWorld) { w.listing = []string{rnRun(2, 1, "queued", "", vfE, "push", vfWFID)} }},
		{name: "a run for E has an unknown status", world: func(w *vfWorld) { w.listing = []string{rnRun(2, 1, "waiting_for_approval", "", vfE, "push", vfWFID)} }},
		{name: "the listing is short", env: setRun(runsEndpoint(vfE, 1), rnListing(3, rnRun(1, 1, "completed", "success", vfE, "push", vfWFID)))},
		{name: "the listing has more than its total", env: setRun(runsEndpoint(vfE, 1), rnListing(0, rnRun(1, 1, "completed", "success", vfE, "push", vfWFID)))},
		{name: "the listing is over the search cap", env: setRun(runsEndpoint(vfE, 1), rnListing(1001))},
		{name: "the listing cannot be read", env: func(e *vfEnv) { e.api.fail[runsEndpoint(vfE, 1)] = true }},
		{name: "the listing holds a run of another commit", env: setRun(runsEndpoint(vfE, 1), rnListing(1, rnRun(1, 1, "completed", "success", rnOther, "push", vfWFID)))},
		{name: "the listing is malformed", env: setRun(runsEndpoint(vfE, 1), `{"total_count":"1"}`)},
	}
	return cases
}

func TestVerifyRefusals(t *testing.T) {
	for _, c := range vfRefusals(t) {
		t.Run(c.name, func(t *testing.T) {
			w := newVfWorld(t)
			if c.world != nil {
				c.world(w)
			}
			e := w.build()
			if c.env != nil {
				c.env(&e)
			}
			line := e.refuse(t)
			if !strings.HasPrefix(line, "fork-release: verify: ") {
				t.Fatalf("refusal %q", line)
			}
			// The healthy control beside it.
			newVfWorld(t).build().pass(t)
		})
	}
}

// The final listing must hold the run the notes name. The run was just read
// and its attempt qualifies, so a listing filtered to the same workflow, push
// event and commit that does not hold it cannot show that no other attempt is
// in progress: an empty listing is not evidence.
func TestVerifyFinalListingMustHoldTheNamedRun(t *testing.T) {
	const id = 123456789 // vfRunID
	named := func(w *vfWorld) { w.named = "1" }
	current := func(status, concl string) func(e *vfEnv) {
		return func(e *vfEnv) {
			e.api.resp[runEndpoint(vfRunID)] = rnRun(id, 2, status, concl, vfE, "push", vfWFID)
		}
	}
	cases := []struct {
		name    string
		world   func(w *vfWorld)
		env     func(e *vfEnv)
		refuses string // a part of the refusal line; empty means the record is printed
	}{
		{
			// The case as reproduced: attempt 1 completed with success, the run
			// itself now at attempt 2 in progress, and a final listing with no run.
			name:    "the run is at a newer attempt in progress and the final listing is empty",
			world:   func(w *vfWorld) { named(w); w.listing = []string{} },
			env:     current("in_progress", ""),
			refuses: "do not include the run",
		},
		{
			name:    "the same newer attempt, honestly listed in progress, is not over",
			world:   func(w *vfWorld) { named(w); w.listing = []string{rnRun(id, 2, "in_progress", "", vfE, "push", vfWFID)} },
			env:     current("in_progress", ""),
			refuses: "is not over",
		},
		{
			name: "control: the named run is listed completed",
			world: func(w *vfWorld) {
				named(w)
				w.listing = []string{rnRun(id, 2, "completed", "success", vfE, "push", vfWFID)}
			},
		},
		{
			name:    "the final listing holds only other completed runs",
			world:   func(w *vfWorld) { w.listing = []string{rnRun(42, 1, "completed", "success", vfE, "push", vfWFID)} },
			refuses: "do not include the run",
		},
		{
			name: "control: the named run is listed beside other completed runs",
			world: func(w *vfWorld) {
				w.listing = []string{rnRun(42, 1, "completed", "success", vfE, "push", vfWFID), rnRun(id, 2, "completed", "success", vfE, "push", vfWFID)}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newVfWorld(t)
			c.world(w)
			e := w.build()
			if c.env != nil {
				c.env(&e)
			}
			if c.refuses == "" {
				if rec, found := parseRecord(verDir+recTag+".md", recTag, []byte(e.pass(t))); len(found) != 0 || rec.Values["run-id"] != vfRunID {
					t.Fatalf("findings %v", found)
				}
				return
			}
			line := e.refuse(t)
			if !strings.HasPrefix(line, "fork-release: verify: ") || !strings.Contains(line, c.refuses) {
				t.Fatalf("refusal %q, want a part %q", line, c.refuses)
			}
		})
	}
}

// The printed conclusion is that of the attempt the notes name, not of the run
// as it stands: a later attempt that failed does not change what was observed.
func TestVerifyRunConclusionComesFromTheNamedAttempt(t *testing.T) {
	const id = 123456789 // vfRunID
	w := newVfWorld(t)
	later := rnRun(id, 2, "completed", "failure", vfE, "push", vfWFID)
	w.listing = []string{later}
	e := w.build()
	e.api.resp[runEndpoint(vfRunID)] = later
	rec, found := parseRecord(verDir+recTag+".md", recTag, []byte(e.pass(t)))
	if len(found) != 0 {
		t.Fatalf("findings %v", found)
	}
	if rec.Values["run-attempt"] != "1" || rec.Values["run-conclusion"] != "success" {
		t.Fatalf("run-attempt %q, run-conclusion %q", rec.Values["run-attempt"], rec.Values["run-conclusion"])
	}
}

func TestVerifyIdentityRefusalsComeBeforeAnyRead(t *testing.T) {
	cases := map[string]struct {
		set   func(e *vfEnv)
		check string
	}{
		"no build information": {func(e *vfEnv) { e.d.build = func() (*debug.BuildInfo, bool) { return nil, false } }, "build"},
		"a modified tree": {func(e *vfEnv) {
			e.d.build = func() (*debug.BuildInfo, bool) {
				return settings("vcs.revision", helperSHA, "vcs.modified", "true"), true
			}
		}, "build"},
		"an unlanded commit":    {func(e *vfEnv) { e.git.ancestry = 1 }, "ancestry"},
		"an undecided ancestry": {func(e *vfEnv) { e.git.ancestry = 128 }, "ancestry"},
		"a failed ancestry":     {func(e *vfEnv) { e.git.ancestryErr = errors.New("boom " + secretURL) }, "ancestry"},
		"a failed fetch":        {func(e *vfEnv) { e.git.fetchErr = errors.New("boom " + secretURL) }, "fetch"},
		"a wrong fetch URL":     {func(e *vfEnv) { e.git.fetchURLs = []string{secretURL} }, "origin"},
		"a wrong push URL":      {func(e *vfEnv) { e.git.pushURLs = []string{secretURL} }, "origin"},
		"one bad push URL":      {func(e *vfEnv) { e.git.pushURLs = []string{canonicalOrigins[0], secretURL} }, "origin"},
		"an unreadable origin":  {func(e *vfEnv) { e.git.urlsErr = errors.New("No such remote " + secretURL) }, "origin"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newVfWorld(t).build()
			c.set(&e)
			line := e.refuse(t)
			if !strings.HasPrefix(line, "fork-release: "+c.check+": ") || strings.Contains(line, "SECRET-URL-MARKER") {
				t.Fatalf("stderr %q", line)
			}
			if len(*e.ev) != 0 || len(e.api.downloads) != 0 {
				t.Fatalf("reads after a refused identity: %v", *e.ev)
			}
		})
	}
}

func TestVerifyRefusesWhatIsNotAReleaseTagBeforeAnyRead(t *testing.T) {
	for _, tag := range []string{"v5.1.0", "5.1.0-ezop.1", "v5.1.0-ezop.0", "v5.1.0-ezop.01", "x", "", "V5.1.0-ezop.1", "../v5.1.0-ezop.1"} {
		e := newVfWorld(t).build()
		code := run([]string{"verify", tag, e.gates}, e.d)
		if code != 1 || !strings.HasPrefix(e.errb.String(), "fork-release: verify: ") || strings.Count(e.errb.String(), "\n") != 1 || e.out.Len() != 0 {
			t.Fatalf("%q: exit %d, stdout %q stderr %q", tag, code, e.out, e.errb)
		}
		if len(e.git.calls) != 0 || len(*e.ev) != 0 {
			t.Fatalf("%q: git %v, reads %v", tag, e.git.calls, *e.ev)
		}
	}
}

func TestVerifyNeedsAGitHubReader(t *testing.T) {
	e := newVfWorld(t).build()
	e.d.gh = nil
	e.refuse(t)
}

func TestVerifyOutputFailure(t *testing.T) {
	e := newVfWorld(t).build()
	e.d.stdout = errWriter{}
	if code := e.run(); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if want := "fork-release: output: could not write the result\n"; e.errb.String() != want {
		t.Fatalf("stderr %q", e.errb)
	}
}

func TestVerifyDispatch(t *testing.T) {
	const usage = "fork-release: usage: fork-release verify --status TAG | verify TAG GATES\n"
	t.Run("a dash-led word is an option, not a tag or a path: usage with no read", func(t *testing.T) {
		for name, args := range map[string][]string{
			"an option as the tag":  {"verify", "--confirm", "gates.txt"},
			"an option as the path": {"verify", recTag, "--status"},
			"-x as the path":        {"verify", recTag, "-x"},
			"three words":           {"verify", recTag, "gates.txt", "extra"},
		} {
			e := newVfWorld(t).build()
			if code := run(args, e.d); code != 2 || e.errb.String() != usage || e.out.Len() != 0 {
				t.Fatalf("%s: exit %d, stdout %q stderr %q", name, code, e.out, e.errb)
			}
			if len(e.git.calls) != 0 || len(*e.ev) != 0 {
				t.Fatalf("%s: a read after a usage error", name)
			}
		}
	})
	t.Run("two plain words are verify TAG GATES", func(t *testing.T) {
		e := newVfWorld(t).build()
		if code := run([]string{"verify", recTag, e.gates}, e.d); code != 0 {
			t.Fatalf("exit %d, stderr %q", code, e.errb)
		}
	})
	t.Run("verify --status is still the status command", func(t *testing.T) {
		env := stVerified().build(t)
		if code := run([]string{"verify", "--status", recTag}, env.d); code != 0 {
			t.Fatalf("exit %d", code)
		}
	})
}

// ---- the host binary ----

func TestVerifyRunsTheHostBinaryInAPrivateDirectoryAndRemovesIt(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "SECRET-TOKEN")
	t.Setenv("GH_TOKEN", "SECRET-TOKEN")
	t.Setenv("PROTON_CLI_TEST_USER", "SECRET-USER")
	w := newVfWorld(t)
	e := w.build()
	var dirMode os.FileMode
	var dirSeen string
	e.d.runProgram = func(path string, args, env []string) ([]byte, error) {
		info, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Errorf("the directory of the binary: %v", err)
		} else {
			dirMode, dirSeen = info.Mode(), filepath.Dir(path)
		}
		return recordingRunner(e.calls, stampJSON("5.1.0-ezop.1"), nil)(path, args, env)
	}
	e.pass(t)
	if len(*e.calls) != 1 {
		t.Fatalf("%d executions", len(*e.calls))
	}
	c := (*e.calls)[0]
	host := hostName(t)
	if filepath.Base(c.path) != host || filepath.Dir(c.path) != dirSeen || !strings.HasPrefix(dirSeen, e.tmp+string(filepath.Separator)) {
		t.Fatalf("ran %q (the temporary directory is %q)", c.path, e.tmp)
	}
	if string(c.data) != "bytes of "+host {
		t.Fatalf("the file holds %q", c.data)
	}
	if c.mode.Perm()&0o100 == 0 || c.mode.Perm()&0o077 != 0 {
		t.Fatalf("mode %v is not owner-only execute", c.mode)
	}
	if dirMode.Perm()&0o077 != 0 {
		t.Fatalf("directory mode %v is not private", dirMode)
	}
	if !slices.Equal(c.args, []string{"version", "-o", "json"}) {
		t.Fatalf("args %q", c.args)
	}
	keys := map[string]string{}
	for _, kv := range c.env {
		k, v, _ := strings.Cut(kv, "=")
		keys[k] = v
		if strings.Contains(kv, "SECRET") {
			t.Errorf("the environment carries %q", kv)
		}
	}
	for k := range keys {
		if !slices.Contains([]string{"PATH", "HOME", "XDG_CONFIG_HOME", "TMPDIR"}, k) {
			t.Errorf("unexpected variable %s", k)
		}
	}
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "TMPDIR"} {
		if !strings.HasPrefix(keys[k], dirSeen+string(filepath.Separator)) {
			t.Errorf("%s=%q is not inside the temporary directory", k, keys[k])
		}
	}
}

func TestVerifyRemovesItsDirectoryWhenTheBinaryFails(t *testing.T) {
	e := newVfWorld(t).build()
	e.d.runProgram = recordingRunner(e.calls, "", errors.New("exit 3"))
	e.refuse(t) // refuse requires the temporary directory to be empty again
	if len(*e.calls) != 1 {
		t.Fatalf("%d executions", len(*e.calls))
	}
}

// The one real execution: the downloaded bytes of the host asset are a shell
// script that stands in for the product binary. It answers with the version only
// when it is called as `version -o json`, with no credential in its environment
// and a home inside the temporary directory.
func TestVerifyExecutesTheDownloadedHostBinary(t *testing.T) {
	script := func(tmp, version string) []byte {
		return []byte(`#!/bin/sh
if [ -n "$GITHUB_TOKEN" ] || [ -n "$GH_TOKEN" ] || [ -n "$PROTON_CLI_TEST_USER" ]; then echo '{"version":"leaked"}'; exit 0; fi
if [ "$#" != 3 ] || [ "$1" != version ] || [ "$2" != -o ] || [ "$3" != json ]; then echo '{"version":"bad arguments"}'; exit 0; fi
case "$HOME" in "` + tmp + `"/*) ;; *) echo '{"version":"home outside"}'; exit 0 ;; esac
echo '{"version":"` + version + `","go":"go1.27.0","platform":"linux/amd64"}'
`)
	}
	real := func(t *testing.T, version string) vfEnv {
		w := newVfWorld(t)
		e := w.build()
		e.d.runProgram = nil // the real execution
		e.api.files[w.idOf(hostName(t))] = script(e.tmp, version)
		// The checksum list is recomputed over the stand-in bytes.
		var b strings.Builder
		for _, a := range w.assets {
			if !w.unlisted[a.Name] {
				fmt.Fprintf(&b, "%s  %s\n", sha256Of(string(e.api.files[a.ID])), a.Name)
			}
		}
		e.api.files[vfSumsID] = []byte(b.String())
		return e
	}
	t.Run("a binary that reports the version passes", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "SECRET-TOKEN")
		t.Setenv("GH_TOKEN", "SECRET-TOKEN")
		t.Setenv("PROTON_CLI_TEST_USER", "SECRET-USER")
		out := real(t, "5.1.0-ezop.1").pass(t)
		if _, found := parseRecord(verDir+recTag+".md", recTag, []byte(out)); len(found) != 0 {
			t.Fatalf("findings %v", found)
		}
	})
	t.Run("a binary that reports another version is refused without echoing", func(t *testing.T) {
		real(t, "5.1.0").refuse(t)
	})
	t.Run("a failing binary is refused without echoing its output", func(t *testing.T) {
		w := newVfWorld(t)
		e := w.build()
		e.d.runProgram = nil
		e.api.files[w.idOf(hostName(t))] = []byte("#!/bin/sh\necho SECRET-BINARY-OUTPUT\necho SECRET-BINARY-OUTPUT >&2\nexit 3\n")
		var b strings.Builder
		for _, a := range w.assets {
			if !w.unlisted[a.Name] {
				fmt.Fprintf(&b, "%s  %s\n", sha256Of(string(e.api.files[a.ID])), a.Name)
			}
		}
		e.api.files[vfSumsID] = []byte(b.String())
		e.refuse(t)
	})
}

func TestVerifyWritesNothing(t *testing.T) {
	e := newVfWorld(t).build()
	before, err := os.ReadFile(e.gates)
	if err != nil {
		t.Fatal(err)
	}
	e.pass(t) // pass requires an empty working directory and an empty temporary one
	after, err := os.ReadFile(e.gates)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("the capture changed: %v", err)
	}
}
