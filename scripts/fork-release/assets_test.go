package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/selfmanage"
)

// wantBinaries are the six names the release must hold, in the order of the
// platform pairs. They are spelled here, in a test, to pin what
// selfmanage.AssetName answers; no production file spells any of them.
var wantBinaries = []string{
	"proton-cli_linux_amd64",
	"proton-cli_linux_arm64",
	"proton-cli_darwin_amd64",
	"proton-cli_darwin_arm64",
	"proton-cli_windows_amd64.exe",
	"proton-cli_windows_arm64.exe",
}

func TestBinaryNamesComeFromAssetName(t *testing.T) {
	got, err := binaryNames()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, wantBinaries) {
		t.Fatalf("names %q, want %q", got, wantBinaries)
	}
	var viaAssetName []string
	for _, pair := range [][2]string{
		{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"},
	} {
		name, err := selfmanage.AssetName(pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
		viaAssetName = append(viaAssetName, name)
	}
	if !slices.Equal(got, viaAssetName) {
		t.Fatalf("names %q differ from selfmanage.AssetName %q", got, viaAssetName)
	}
	t.Run("an unsupported pair fails the check", func(t *testing.T) {
		for _, pair := range [][2]string{{"plan9", "amd64"}, {"linux", "386"}, {"", ""}, {"windows", "riscv64"}} {
			if names, err := binaryNamesFor([][2]string{{"linux", "amd64"}, pair}); err == nil || names != nil {
				t.Fatalf("%v: names %q, err %v", pair, names, err)
			}
		}
	})
	t.Run("the six platform pairs are the web of Linux, macOS and Windows on amd64 and arm64", func(t *testing.T) {
		if len(platforms) != 6 {
			t.Fatalf("%d platform pairs", len(platforms))
		}
	})
	t.Run("no production file of the helper spells a binary name", func(t *testing.T) {
		files, err := filepath.Glob("*.go")
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "proton-cli_") {
				t.Errorf("%s spells a binary name", f)
			}
		}
	})
}

// goreleaserChecksums is the shape of a GoReleaser checksums.txt: one line per
// file, `<64 lowercase hex>  <name>`, a single line feed at the end. The hashes
// are made up.
func goreleaserChecksums(names ...string) string {
	var b strings.Builder
	for i, n := range names {
		fmt.Fprintf(&b, "%064x  %s\n", i+1, n)
	}
	return b.String()
}

// archiveNames are the twelve-line snapshot's other half: the six archives.
func archiveNames() []string {
	var out []string
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			ext := ".tar.gz"
			if goos == "windows" {
				ext = ".zip"
			}
			out = append(out, "proton-cli_5.1.0-ezop.1_"+goos+"_"+arch+ext)
		}
	}
	return out
}

func TestParseChecksums(t *testing.T) {
	good := strings.Repeat("ab", 32)
	t.Run("a GoReleaser file of twelve lines", func(t *testing.T) {
		names := append(slices.Clone(wantBinaries), archiveNames()...)
		got, err := parseChecksums([]byte(goreleaserChecksums(names...)))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 12 {
			t.Fatalf("%d entries", len(got))
		}
		for i, n := range names {
			if got[n] != fmt.Sprintf("%064x", i+1) {
				t.Errorf("%s has hash %q", n, got[n])
			}
		}
	})
	t.Run("one line, with or without the closing line feed", func(t *testing.T) {
		for _, text := range []string{good + "  a\n", good + "  a"} {
			got, err := parseChecksums([]byte(text))
			if err != nil || len(got) != 1 || got["a"] != good {
				t.Fatalf("%q: %v, %v", text, got, err)
			}
		}
	})
	t.Run("two names may share a hash", func(t *testing.T) {
		got, err := parseChecksums([]byte(good + "  a\n" + good + "  b\n"))
		if err != nil || len(got) != 2 {
			t.Fatalf("%v, %v", got, err)
		}
	})
	t.Run("names with spaces inside and dots are kept", func(t *testing.T) {
		got, err := parseChecksums([]byte(good + "  a b.tar.gz\n"))
		if err != nil || got["a b.tar.gz"] != good {
			t.Fatalf("%v, %v", got, err)
		}
	})
	for name, text := range map[string]string{
		"empty":                        "",
		"only a line feed":             "\n",
		"a blank line first":           "\n" + good + "  a\n",
		"a blank line between lines":   good + "  a\n\n" + good + "  b\n",
		"two blank lines at the end":   good + "  a\n\n\n",
		"a blank line at the end":      good + "  a\n\n",
		"CRLF":                         good + "  a\r\n",
		"one space":                    good + " a\n",
		"three spaces":                 good + "   a\n",
		"a tab":                        good + "\ta\n",
		"star for binary mode":         good + " *a\n",
		"upper case hex":               strings.ToUpper(good) + "  a\n",
		"mixed case hex":               "A" + good[1:] + "  a\n",
		"a short hash":                 good[:63] + "  a\n",
		"a long hash":                  good + "a  a\n",
		"a non hex hash":               strings.Repeat("zz", 32) + "  a\n",
		"no name":                      good + "  \n",
		"no separator":                 good + "\n",
		"a name with a leading space":  good + "   a\n",
		"a name with a trailing space": good + "  a \n",
		"a control character":          good + "  a\x01\n",
		"a line before the hash":       "x " + good + "  a\n",
		"a leading space":              " " + good + "  a\n",
		"a repeated name":              good + "  a\n" + strings.Repeat("cd", 32) + "  a\n",
		"a repeated name and hash":     good + "  a\n" + good + "  a\n",
		"a good line and a bad one":    good + "  a\nnot a checksum line\n",
		"a NUL byte":                   good + "  a\x00\n",
		"text without hashes":          "hello world\n",
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			got, err := parseChecksums([]byte(text))
			if err == nil || got != nil {
				t.Fatalf("accepted: %v", got)
			}
			if strings.Contains(err.Error(), good) {
				t.Fatalf("the error quotes the file: %v", err)
			}
		})
	}
}

// ---- assets over fakes ----

type assetSpec struct {
	Name  string
	ID    int64
	State string
}

// assetAPI serves the releases list from fakePages and the asset bytes by id,
// and records every download it is asked for. It is a ghGetter and a
// ghDownloader.
type assetAPI struct {
	*fakePages
	files     map[int64][]byte
	failOn    map[int64]bool
	downloads []string
}

func (a *assetAPI) Download(endpoint string) ([]byte, error) {
	a.downloads = append(a.downloads, endpoint)
	id, err := strconv.ParseInt(strings.TrimPrefix(endpoint, "releases/assets/"), 10, 64)
	if err != nil || a.failOn[id] {
		return nil, errors.New("gh failed " + relMarker)
	}
	data, ok := a.files[id]
	if !ok {
		return nil, errors.New("no such asset " + relMarker)
	}
	return data, nil
}

// relEntryAssets is a release entry as the API renders it, with assets.
func relEntryAssets(tag string, draft bool, body string, assets []assetSpec) string {
	list := make([]any, 0, len(assets))
	for _, a := range assets {
		list = append(list, map[string]any{"name": a.Name, "id": a.ID, "state": a.State, "size": 1234, "url": "https://example.invalid/" + relMarker})
	}
	b, _ := json.Marshal(map[string]any{
		"id": 1, "tag_name": tag, "draft": draft, "prerelease": true,
		"name": relMarker, "body": body, "assets": list,
	})
	return string(b)
}

const checksumsID = 200

// assetsWorld is one run of the assets or stamp command over fakes: the release
// entry, the bytes of its assets, and the two environment variables.
type assetsWorld struct {
	t       *testing.T
	assets  []assetSpec
	files   map[int64][]byte
	body    string
	draft   bool
	tagName string
	env     map[string]string
	reads   []string
	api     *assetAPI
}

// binaryID is the asset id of the i-th binary.
func binaryID(i int) int64 { return int64(101 + i) }

func newAssetsWorld(t *testing.T) *assetsWorld {
	t.Helper()
	w := &assetsWorld{
		t:       t,
		files:   map[int64][]byte{},
		body:    relBody(targetSHA),
		tagName: inRunTag,
		env:     map[string]string{"GITHUB_REF_NAME": inRunTag, "GITHUB_SHA": targetSHA},
	}
	names := slices.Clone(wantBinaries)
	for i, n := range names {
		w.assets = append(w.assets, assetSpec{n, binaryID(i), "uploaded"})
		w.files[binaryID(i)] = []byte("bytes of " + n)
	}
	for i, n := range archiveNames() {
		id := int64(301 + i)
		w.assets = append(w.assets, assetSpec{n, id, "uploaded"})
		w.files[id] = []byte("bytes of " + n)
		names = append(names, n)
	}
	w.assets = append(w.assets, assetSpec{"checksums.txt", checksumsID, "uploaded"})
	w.files[checksumsID] = []byte(goreleaserChecksums(names...))
	return w
}

func (w *assetsWorld) deps() (guardDeps, *bytes.Buffer, *bytes.Buffer) {
	w.api = &assetAPI{
		fakePages: pagesOf(relPage(relEntryAssets(w.tagName, w.draft, w.body, w.assets))),
		files:     w.files,
		failOn:    map[int64]bool{},
	}
	var out, errb bytes.Buffer
	return guardDeps{
		git:    newFakeGit(cleanTree(w.t)),
		build:  cleanBuild,
		stdout: &out,
		stderr: &errb,
		getenv: func(name string) string {
			w.reads = append(w.reads, name)
			return w.env[name]
		},
		gh: w.api,
	}, &out, &errb
}

func (w *assetsWorld) asset(name string) *assetSpec {
	for i := range w.assets {
		if w.assets[i].Name == name {
			return &w.assets[i]
		}
	}
	w.t.Fatalf("no asset %s", name)
	return nil
}

func (w *assetsWorld) drop(name string) {
	w.assets = slices.DeleteFunc(w.assets, func(a assetSpec) bool { return a.Name == name })
}

func (w *assetsWorld) runAssets(mutate func(d *guardDeps)) (code int, stdout, stderr string) {
	d, out, errb := w.deps()
	if mutate != nil {
		mutate(&d)
	}
	code = run([]string{"assets", "--in-run"}, d)
	return code, out.String(), errb.String()
}

func TestRunAssetsPass(t *testing.T) {
	t.Run("six binaries and checksums.txt, with archives besides, pass", func(t *testing.T) {
		w := newAssetsWorld(t)
		code, out, errb := w.runAssets(nil)
		if code != 0 || errb != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		if !strings.HasPrefix(out, "fork-release: assets: ok") || strings.Count(out, "\n") != 1 {
			t.Fatalf("stdout %q", out)
		}
	})
	t.Run("only checksums.txt is downloaded, never a binary, and only the two variables are read", func(t *testing.T) {
		w := newAssetsWorld(t)
		if code, out, errb := w.runAssets(nil); code != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		if want := []string{"releases/assets/200"}; !slices.Equal(w.api.downloads, want) {
			t.Fatalf("downloads %v", w.api.downloads)
		}
		for _, name := range w.reads {
			if name != "GITHUB_REF_NAME" && name != "GITHUB_SHA" {
				t.Errorf("read %q", name)
			}
		}
	})
	t.Run("no deb, rpm or apk is needed, and one that is present is ignored", func(t *testing.T) {
		w := newAssetsWorld(t)
		for _, n := range archiveNames() {
			w.drop(n)
		}
		if code, out, errb := w.runAssets(nil); code != 0 {
			t.Fatalf("without extras: exit %d, stdout %q, stderr %q", code, out, errb)
		}
		w = newAssetsWorld(t)
		w.assets = append(w.assets, assetSpec{"proton-cli_5.1.0.deb", 400, "uploaded"}, assetSpec{"x.rpm", 401, "starter"}, assetSpec{"y.apk", 402, "open"})
		if code, out, errb := w.runAssets(nil); code != 0 {
			t.Fatalf("with extras: exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("a checksums.txt that lists only the six binaries passes", func(t *testing.T) {
		w := newAssetsWorld(t)
		w.files[checksumsID] = []byte(goreleaserChecksums(wantBinaries...))
		if code, out, errb := w.runAssets(nil); code != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("the release is read once through the one lookup, page by page", func(t *testing.T) {
		w := newAssetsWorld(t)
		w.runAssets(nil)
		want := []string{"releases?per_page=100&page=1", "releases?per_page=100&page=2"}
		if !slices.Equal(w.api.calls, want) {
			t.Fatalf("calls %v", w.api.calls)
		}
	})
}

func (w *assetsWorld) wantAssetsRefusal(mutate func(d *guardDeps), needles ...string) string {
	w.t.Helper()
	code, out, errb := w.runAssets(mutate)
	oneLineRefusal(w.t, code, out, errb, "assets")
	for _, n := range needles {
		if !strings.Contains(errb, n) {
			w.t.Fatalf("stderr %q lacks %q", errb, n)
		}
	}
	for _, bad := range []string{relMarker, "http", "example.invalid", strings.Repeat("0", 20)} {
		if strings.Contains(errb, bad) {
			w.t.Fatalf("the refusal quotes %q: %q", bad, errb)
		}
	}
	return errb
}

func TestRunAssetsFailures(t *testing.T) {
	required := append(slices.Clone(wantBinaries), "checksums.txt")
	for _, name := range required {
		t.Run("missing "+name, func(t *testing.T) {
			w := newAssetsWorld(t)
			w.drop(name)
			w.wantAssetsRefusal(nil, name)
		})
		t.Run("not uploaded: "+name, func(t *testing.T) {
			for _, state := range []string{"starter", "open", "", "Uploaded", "uploaded "} {
				w := newAssetsWorld(t)
				w.asset(name).State = state
				w.wantAssetsRefusal(nil, name)
			}
		})
	}
	t.Run("a name that differs only in case is not the asset", func(t *testing.T) {
		w := newAssetsWorld(t)
		w.asset("checksums.txt").Name = "Checksums.txt"
		w.wantAssetsRefusal(nil, "checksums.txt")
	})
	for i, name := range wantBinaries {
		t.Run("not listed in checksums.txt: "+name, func(t *testing.T) {
			w := newAssetsWorld(t)
			var rest []string
			for j, n := range wantBinaries {
				if j != i {
					rest = append(rest, n)
				}
			}
			w.files[checksumsID] = []byte(goreleaserChecksums(append(rest, archiveNames()...)...))
			w.wantAssetsRefusal(nil, name)
		})
	}
	good := strings.Repeat("ab", 32)
	for name, text := range map[string]string{
		"a malformed line":      goreleaserChecksums(wantBinaries...) + "garbage\n",
		"a repeated line":       goreleaserChecksums(wantBinaries...) + goreleaserChecksums(wantBinaries[0]),
		"CRLF":                  strings.ReplaceAll(goreleaserChecksums(wantBinaries...), "\n", "\r\n"),
		"empty":                 "",
		"a blank line":          goreleaserChecksums(wantBinaries[:3]...) + "\n" + goreleaserChecksums(wantBinaries[3:]...),
		"upper case hashes":     strings.ToUpper(good) + "  " + wantBinaries[0] + "\n",
		"one space":             strings.ReplaceAll(goreleaserChecksums(wantBinaries...), "  ", " "),
		"a JSON error document": `{"message":"Not Found","documentation_url":"https://example.invalid/` + relMarker + `"}`,
	} {
		t.Run("checksums.txt is invalid: "+name, func(t *testing.T) {
			w := newAssetsWorld(t)
			w.files[checksumsID] = []byte(text)
			w.wantAssetsRefusal(nil, "checksums.txt")
		})
	}
	t.Run("the checksums.txt download fails", func(t *testing.T) {
		w := newAssetsWorld(t)
		w.wantAssetsRefusal(func(d *guardDeps) { d.gh.(*assetAPI).failOn[checksumsID] = true }, "checksums.txt")
	})
	t.Run("the bytes of checksums.txt are not in the files", func(t *testing.T) {
		w := newAssetsWorld(t)
		delete(w.files, checksumsID)
		w.wantAssetsRefusal(nil, "checksums.txt")
	})
	t.Run("a GitHub reader that cannot download refuses", func(t *testing.T) {
		w := newAssetsWorld(t)
		w.wantAssetsRefusal(func(d *guardDeps) { d.gh = d.gh.(*assetAPI).fakePages })
	})
	t.Run("no GitHub reader refuses", func(t *testing.T) {
		w := newAssetsWorld(t)
		w.wantAssetsRefusal(func(d *guardDeps) { d.gh = nil })
	})
	t.Run("no environment refuses", func(t *testing.T) {
		w := newAssetsWorld(t)
		w.wantAssetsRefusal(func(d *guardDeps) { d.getenv = nil })
	})
	t.Run("the environment is checked", func(t *testing.T) {
		for name, edit := range map[string]func(w *assetsWorld){
			"no tag":              func(w *assetsWorld) { delete(w.env, "GITHUB_REF_NAME") },
			"a tag of no pattern": func(w *assetsWorld) { w.env["GITHUB_REF_NAME"] = "v5.1.0" },
			"no commit":           func(w *assetsWorld) { delete(w.env, "GITHUB_SHA") },
			"a short commit":      func(w *assetsWorld) { w.env["GITHUB_SHA"] = targetSHA[:39] },
		} {
			w := newAssetsWorld(t)
			edit(w)
			w.wantAssetsRefusal(nil)
			if len(w.api.calls) != 0 {
				t.Fatalf("%s: GitHub was asked: %v", name, w.api.calls)
			}
		}
	})
}

func TestRunAssetsReleaseStates(t *testing.T) {
	for name, edit := range map[string]func(w *assetsWorld){
		"Draft": func(w *assetsWorld) { w.draft = true },
		"Other, naming another commit": func(w *assetsWorld) {
			w.body = relBody(relOther)
		},
		"Other, with no Commit line": func(w *assetsWorld) { w.body = "Notes only\n" },
		"None": func(w *assetsWorld) {
			w.tagName = "v5.1.0-ezop.2"
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newAssetsWorld(t)
			edit(w)
			w.wantAssetsRefusal(nil)
			if len(w.api.downloads) != 0 {
				t.Fatalf("downloads %v", w.api.downloads)
			}
		})
	}
	t.Run("Unknown: the list cannot be read", func(t *testing.T) {
		w := newAssetsWorld(t)
		w.wantAssetsRefusal(func(d *guardDeps) { d.gh.(*assetAPI).fail[1] = true })
	})
	t.Run("Unknown: two releases carry the tag", func(t *testing.T) {
		w := newAssetsWorld(t)
		w.wantAssetsRefusal(func(d *guardDeps) {
			e := relEntryAssets(w.tagName, false, w.body, w.assets)
			d.gh.(*assetAPI).fakePages = pagesOf(relPage(e, e))
		})
	})
	t.Run("the state is named in the reason", func(t *testing.T) {
		w := newAssetsWorld(t)
		w.draft = true
		w.wantAssetsRefusal(nil, "Draft")
	})
}

func TestLookupReleaseCarriesAssets(t *testing.T) {
	const tag = inRunTag
	body := relBody(relE)
	t.Run("Published-Same carries name, id and state in order", func(t *testing.T) {
		list := []assetSpec{{"b", 7, "uploaded"}, {"a", 9007199254740991, "starter"}, {"c", 1, "uploaded"}}
		got := lookupRelease(pagesOf(relPage(relEntryAssets(tag, false, body, list))), tag, relE)
		wantRelease(t, got, releasePublishedSame)
		if got.AssetsReason != "" {
			t.Fatalf("assets reason %q", got.AssetsReason)
		}
		want := []releaseAsset{{"b", 7, "uploaded"}, {"a", 9007199254740991, "starter"}, {"c", 1, "uploaded"}}
		if !slices.Equal(got.Assets, want) {
			t.Fatalf("assets %+v", got.Assets)
		}
	})
	t.Run("an empty list is a list", func(t *testing.T) {
		got := lookupRelease(pagesOf(relPage(relEntryAssets(tag, false, body, nil))), tag, relE)
		wantRelease(t, got, releasePublishedSame)
		if got.AssetsReason != "" || len(got.Assets) != 0 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("the other states carry no assets", func(t *testing.T) {
		list := []assetSpec{{"a", 1, "uploaded"}}
		for state, entry := range map[releaseState]string{
			releaseDraft: relEntryAssets(tag, true, body, list),
			releaseOther: relEntryAssets(tag, false, relBody(relOther), list),
		} {
			got := lookupRelease(pagesOf(relPage(entry)), tag, relE)
			wantRelease(t, got, state)
			if got.Assets != nil || got.AssetsReason != "" {
				t.Fatalf("%v: %+v", state, got)
			}
		}
		got := lookupRelease(pagesOf("[]"), tag, relE)
		if got.Assets != nil {
			t.Fatalf("%+v", got)
		}
	})
	asset := func(fields string) string {
		return `[{"tag_name":"` + tag + `","draft":false,"body":` + strconv.Quote(body) + `,"assets":` + fields + `}]`
	}
	for name, assets := range map[string]string{
		"the key is missing":        "",
		"null":                      "null",
		"an object":                 `{"name":"a"}`,
		"a string":                  `"a"`,
		"an entry that is null":     `[null]`,
		"an entry that is a string": `["a"]`,
		"no name":                   `[{"id":1,"state":"uploaded"}]`,
		"a name that is a number":   `[{"name":5,"id":1,"state":"uploaded"}]`,
		"an empty name":             `[{"name":"","id":1,"state":"uploaded"}]`,
		"no id":                     `[{"name":"a","state":"uploaded"}]`,
		"an id that is a string":    `[{"name":"a","id":"1","state":"uploaded"}]`,
		"an id that is a fraction":  `[{"name":"a","id":1.5,"state":"uploaded"}]`,
		"an id of zero":             `[{"name":"a","id":0,"state":"uploaded"}]`,
		"a negative id":             `[{"name":"a","id":-4,"state":"uploaded"}]`,
		"an id too large to trust":  `[{"name":"a","id":9007199254740993,"state":"uploaded"}]`,
		"no state":                  `[{"name":"a","id":1}]`,
		"a state that is a number":  `[{"name":"a","id":1,"state":1}]`,
		"a repeated name":           `[{"name":"a","id":1,"state":"uploaded"},{"name":"a","id":2,"state":"uploaded"}]`,
		"a repeated id":             `[{"name":"a","id":1,"state":"uploaded"},{"name":"b","id":1,"state":"uploaded"}]`,
		"a good entry and a bad":    `[{"name":"a","id":1,"state":"uploaded"},{"name":"b"}]`,
	} {
		t.Run("a malformed asset list fails only the reader of it: "+name, func(t *testing.T) {
			page := asset(assets)
			if assets == "" {
				page = `[{"tag_name":"` + tag + `","draft":false,"body":` + strconv.Quote(body) + `}]`
			}
			got := lookupRelease(pagesOf(page), tag, relE)
			wantRelease(t, got, releasePublishedSame)
			if got.AssetsReason == "" || got.Assets != nil {
				t.Fatalf("%+v", got)
			}
			if strings.Contains(got.AssetsReason, relMarker) || strings.Contains(got.AssetsReason, "uploaded") {
				t.Fatalf("the reason quotes an answer: %q", got.AssetsReason)
			}
		})
	}
	t.Run("a malformed asset list on another tag's release does not matter", func(t *testing.T) {
		other := `{"tag_name":"v5.1.0-ezop.2","draft":false,"body":"","assets":null}`
		entry := relEntryAssets(tag, false, body, []assetSpec{{"a", 1, "uploaded"}})
		got := lookupRelease(pagesOf(relPage(other, entry)), tag, relE)
		wantRelease(t, got, releasePublishedSame)
		if got.AssetsReason != "" || len(got.Assets) != 1 {
			t.Fatalf("%+v", got)
		}
	})
}

// ---- the download path of the adapter ----

func TestGHDownloadArgs(t *testing.T) {
	for _, id := range []string{"1", "9", "10", "123456789", "1234567890123456"} {
		got, err := ghDownloadArgs("releases/assets/" + id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		want := []string{
			"api", "--method", "GET", "-H", "X-GitHub-Api-Version: 2026-03-10", "-H", "Accept: application/octet-stream",
			"repos/Ezopek/proton-cli/releases/assets/" + id,
		}
		if !slices.Equal(got, want) {
			t.Fatalf("argv = %q, want %q", got, want)
		}
	}
}

func TestGHDownloadArgsRefuseOtherPaths(t *testing.T) {
	for _, endpoint := range []string{
		"", "releases/assets/", "releases/assets", "releases/assets/0", "releases/assets/01", "releases/assets/-1",
		"releases/assets/+1", "releases/assets/1.5", "releases/assets/a", "releases/assets/12345678901234567",
		"releases/assets/1/", "releases/assets/1/x", "releases/assets/1?x=1", "releases/assets/1#x", "releases/assets/1\n",
		"releases/assets/1 ", " releases/assets/1", "/releases/assets/1", "releases/assets/../1", "releases/assets/%31",
		"RELEASES/assets/1", "releases/1", "releases/1/assets", "releases/latest", "releases/tags/v5.1.0-ezop.1",
		"releases?per_page=100&page=1", "git/ref/heads/ezop", "repos/Other/repo/releases/assets/1", "releases/assets/1/../2",
	} {
		if _, err := ghDownloadArgs(endpoint); err == nil {
			t.Fatalf("%q was accepted", endpoint)
		}
	}
}

func TestGetPathsDoNotReachAssets(t *testing.T) {
	for _, endpoint := range []string{"releases/assets/1", "releases/assets/123"} {
		if _, err := ghArgs(endpoint); err == nil {
			t.Fatalf("the JSON reader accepted %q", endpoint)
		}
	}
}

func TestExecGHDownloadRunsTheExactArgv(t *testing.T) {
	g, log := fakeGHAdapter(t, "binary bytes", "GH_REPO=Other/repo", "GH_HOST=example.invalid")
	got, err := g.Download("releases/assets/4242")
	if err != nil || string(got) != "binary bytes" {
		t.Fatalf("got %q, %v", got, err)
	}
	calls := readCalls(t, log)
	want := []string{
		"api", "--method", "GET", "-H", "X-GitHub-Api-Version: 2026-03-10", "-H", "Accept: application/octet-stream",
		"repos/Ezopek/proton-cli/releases/assets/4242",
	}
	if len(calls) != 1 || !slices.Equal(calls[0].Argv, want) {
		t.Fatalf("calls %+v", calls)
	}
	if calls[0].GHRepo != "" || calls[0].GHHost != "" {
		t.Fatalf("GH_REPO or GH_HOST reached gh: %+v", calls[0])
	}
}

func TestExecGHDownloadRefusesBeforeRunningGH(t *testing.T) {
	g, log := fakeGHAdapter(t, "x")
	for _, endpoint := range []string{"releases/assets/0", "releases/latest", "releases?per_page=100&page=1", "releases/assets/1?x"} {
		if _, err := g.Download(endpoint); err == nil {
			t.Fatalf("%q was run", endpoint)
		}
	}
	if _, err := g.Get("releases/assets/5"); err == nil {
		t.Fatal("Get ran an asset path")
	}
	if calls := readCalls(t, log); len(calls) != 0 {
		t.Fatalf("gh ran: %+v", calls)
	}
}

func TestExecGHDownloadLimit(t *testing.T) {
	if maxGHDownload <= maxGHOutput {
		t.Fatalf("the download limit %d is not larger than the JSON limit %d", maxGHDownload, maxGHOutput)
	}
	t.Run("an answer larger than the JSON limit downloads, and does not read as JSON", func(t *testing.T) {
		g, _ := fakeGHAdapter(t, "", "FORK_RELEASE_FAKE_BYTES="+strconv.Itoa(maxGHOutput+1024))
		got, err := g.Download("releases/assets/7")
		if err != nil || len(got) != maxGHOutput+1024 {
			t.Fatalf("%d bytes, %v", len(got), err)
		}
		if _, err := g.Get("releases?per_page=100&page=1"); err == nil {
			t.Fatal("the JSON limit did not apply to Get")
		}
	})
	t.Run("an answer over the download limit fails", func(t *testing.T) {
		g, _ := fakeGHAdapter(t, "", "FORK_RELEASE_FAKE_BYTES=2000")
		g.downloadLimit = 1000
		if got, err := g.Download("releases/assets/7"); err == nil || got != nil {
			t.Fatalf("%d bytes, %v", len(got), err)
		}
	})
	t.Run("an answer at the limit passes", func(t *testing.T) {
		g, _ := fakeGHAdapter(t, "", "FORK_RELEASE_FAKE_BYTES=1000")
		g.downloadLimit = 1000
		if got, err := g.Download("releases/assets/7"); err != nil || len(got) != 1000 {
			t.Fatalf("%d bytes, %v", len(got), err)
		}
	})
	t.Run("a failing gh is an error without its output", func(t *testing.T) {
		g, _ := fakeGHAdapter(t, "x", "FORK_RELEASE_FAKE_EXIT=1")
		_, err := g.Download("releases/assets/7")
		if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "http") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestProductionGHCanDownload(t *testing.T) {
	g := productionDeps().gh
	if _, ok := g.(ghDownloader); !ok {
		t.Fatalf("production gh %T cannot download", g)
	}
}

// ---- stamp ----

// stampFile is the file stamp creates in RUNNER_TEMP for the Linux amd64 binary.
var stampFile = "stamp-" + wantBinaries[0]

// runCall is one execution the fake runner saw, with the file as it was at that
// moment.
type runCall struct {
	path string
	args []string
	env  []string
	mode os.FileMode
	data []byte
}

// recordingRunner is a program runner that records the call, reads the file it
// was asked to run, and answers out and err.
func recordingRunner(calls *[]runCall, out string, err error) func(string, []string, []string) ([]byte, error) {
	return func(path string, args, env []string) ([]byte, error) {
		c := runCall{path: path, args: args, env: env}
		if info, statErr := os.Stat(path); statErr == nil {
			c.mode = info.Mode()
		}
		c.data, _ = os.ReadFile(path)
		*calls = append(*calls, c)
		return []byte(out), err
	}
}

const stampVersion = "5.1.0-ezop.1"

func stampJSON(version string) string {
	return `{"version":"` + version + `","go":"go1.27.0","platform":"linux/amd64","revision":"0123456789ab"}` + "\n"
}

// newStampWorld is an assets world with a RUNNER_TEMP, whose Linux amd64 binary
// is the stand-in bytes.
func newStampWorld(t *testing.T) *assetsWorld {
	t.Helper()
	w := newAssetsWorld(t)
	w.env["RUNNER_TEMP"] = t.TempDir()
	return w
}

func (w *assetsWorld) runStamp(mutate func(d *guardDeps)) (code int, stdout, stderr string) {
	d, out, errb := w.deps()
	if mutate != nil {
		mutate(&d)
	}
	code = run([]string{"stamp", "--in-run"}, d)
	return code, out.String(), errb.String()
}

func (w *assetsWorld) wantStampRefusal(mutate func(d *guardDeps), needles ...string) string {
	w.t.Helper()
	code, out, errb := w.runStamp(mutate)
	oneLineRefusal(w.t, code, out, errb, "stamp")
	for _, n := range needles {
		if !strings.Contains(errb, n) {
			w.t.Fatalf("stderr %q lacks %q", errb, n)
		}
	}
	for _, bad := range []string{relMarker, "SECRET-BINARY-OUTPUT", "http", "example.invalid"} {
		if strings.Contains(errb, bad) {
			w.t.Fatalf("the refusal quotes %q: %q", bad, errb)
		}
	}
	return errb
}

func TestRunStampPass(t *testing.T) {
	t.Run("the Linux amd64 binary is downloaded, written executable and run with version -o json", func(t *testing.T) {
		w := newStampWorld(t)
		var calls []runCall
		code, out, errb := w.runStamp(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, stampJSON(stampVersion), nil) })
		if code != 0 || errb != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		if !strings.HasPrefix(out, "fork-release: stamp: ok") || strings.Count(out, "\n") != 1 || strings.Contains(out, "go1.27") {
			t.Fatalf("stdout %q", out)
		}
		if want := []string{"releases/assets/101"}; !slices.Equal(w.api.downloads, want) {
			t.Fatalf("downloads %v", w.api.downloads)
		}
		if len(calls) != 1 {
			t.Fatalf("%d executions", len(calls))
		}
		c := calls[0]
		dir := w.env["RUNNER_TEMP"]
		if c.path != filepath.Join(dir, stampFile) {
			t.Fatalf("ran %q", c.path)
		}
		if string(c.data) != "bytes of "+wantBinaries[0] {
			t.Fatalf("the file holds %q", c.data)
		}
		if c.mode.Perm()&0o100 == 0 || c.mode.Perm()&0o077 != 0 {
			t.Fatalf("mode %v is not owner-only execute", c.mode)
		}
		if !slices.Equal(c.args, []string{"version", "-o", "json"}) {
			t.Fatalf("args %q", c.args)
		}
	})
	t.Run("the environment of the binary is minimal and holds no credential", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "SECRET-TOKEN")
		t.Setenv("GH_TOKEN", "SECRET-TOKEN")
		t.Setenv("PROTON_CLI_TEST_USER", "SECRET-USER")
		w := newStampWorld(t)
		var calls []runCall
		if code, out, errb := w.runStamp(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, stampJSON(stampVersion), nil) }); code != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		keys := map[string]string{}
		for _, kv := range calls[0].env {
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
		for _, k := range []string{"HOME", "XDG_CONFIG_HOME"} {
			if !strings.HasPrefix(keys[k], w.env["RUNNER_TEMP"]+string(filepath.Separator)) {
				t.Errorf("%s=%q is not inside RUNNER_TEMP", k, keys[k])
			}
		}
	})
	t.Run("only the three variables are read, and nothing else is left in RUNNER_TEMP but the binary", func(t *testing.T) {
		w := newStampWorld(t)
		var calls []runCall
		if code, out, errb := w.runStamp(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, stampJSON(stampVersion), nil) }); code != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		for _, name := range w.reads {
			if !slices.Contains([]string{"GITHUB_REF_NAME", "GITHUB_SHA", "RUNNER_TEMP"}, name) {
				t.Errorf("read %q", name)
			}
		}
		if names := entriesOf(t, w.env["RUNNER_TEMP"]); !slices.Equal(names, []string{stampFile}) {
			t.Fatalf("files %v", names)
		}
	})
	t.Run("other assets and states are ignored: only the Linux amd64 binary matters", func(t *testing.T) {
		w := newStampWorld(t)
		w.asset(wantBinaries[1]).State = "starter"
		w.drop("checksums.txt")
		var calls []runCall
		if code, out, errb := w.runStamp(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, stampJSON(stampVersion), nil) }); code != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
	t.Run("the tag decides the version: another tag, another version", func(t *testing.T) {
		w := newStampWorld(t)
		w.tagName = "v5.1.0-ezop.12"
		w.env["GITHUB_REF_NAME"] = "v5.1.0-ezop.12"
		var calls []runCall
		if code, out, errb := w.runStamp(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, stampJSON("5.1.0-ezop.12"), nil) }); code != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
}

func TestRunStampFailures(t *testing.T) {
	ok := stampJSON(stampVersion)
	t.Run("the version is wrong, or the answer is not the JSON object", func(t *testing.T) {
		for name, out := range map[string]string{
			"upstream version":     stampJSON("5.1.0"),
			"a v prefix":           stampJSON("v" + stampVersion),
			"another fork number":  stampJSON("5.1.0-ezop.2"),
			"another base":         stampJSON("5.2.0-ezop.1"),
			"a trailing space":     stampJSON(stampVersion + " "),
			"dev":                  stampJSON("dev"),
			"an empty version":     stampJSON(""),
			"no version":           `{"go":"go1.27.0"}`,
			"a null version":       `{"version":null}`,
			"a numeric version":    `{"version":5.1}`,
			"an empty output":      "",
			"only a line feed":     "\n",
			"text":                 "proton-cli version " + stampVersion + "\n",
			"a JSON array":         "[" + ok + "]",
			"a JSON string":        `"` + stampVersion + `"`,
			"a cut object":         `{"version":"` + stampVersion + `"`,
			"two objects":          ok + ok,
			"trailing text":        ok + "SECRET-BINARY-OUTPUT",
			"a case difference":    `{"Version":"` + stampVersion + `"}`,
			"the version in text":  `{"version":"x","note":"` + stampVersion + `"}`,
			"binary output marker": "SECRET-BINARY-OUTPUT",
		} {
			w := newStampWorld(t)
			var calls []runCall
			w.wantStampRefusal(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, out, nil) })
			if len(calls) != 1 {
				t.Fatalf("%s: %d executions", name, len(calls))
			}
		}
	})
	t.Run("a failing binary", func(t *testing.T) {
		w := newStampWorld(t)
		var calls []runCall
		w.wantStampRefusal(func(d *guardDeps) {
			d.runProgram = recordingRunner(&calls, "SECRET-BINARY-OUTPUT "+ok, errors.New("exit status 3 SECRET-BINARY-OUTPUT"))
		})
	})
	t.Run("the asset is missing", func(t *testing.T) {
		w := newStampWorld(t)
		w.drop(wantBinaries[0])
		var calls []runCall
		w.wantStampRefusal(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, ok, nil) }, wantBinaries[0])
		if len(calls) != 0 || len(w.api.downloads) != 0 {
			t.Fatalf("ran %d, downloaded %v", len(calls), w.api.downloads)
		}
	})
	t.Run("the asset is not uploaded", func(t *testing.T) {
		w := newStampWorld(t)
		w.asset(wantBinaries[0]).State = "starter"
		var calls []runCall
		w.wantStampRefusal(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, ok, nil) }, wantBinaries[0])
		if len(calls) != 0 || len(w.api.downloads) != 0 {
			t.Fatalf("ran %d, downloaded %v", len(calls), w.api.downloads)
		}
	})
	t.Run("the asset list cannot be trusted", func(t *testing.T) {
		w := newStampWorld(t)
		w.assets = append(w.assets, assetSpec{wantBinaries[0], 999, "uploaded"})
		var calls []runCall
		w.wantStampRefusal(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, ok, nil) })
		if len(calls) != 0 {
			t.Fatal("the binary ran")
		}
	})
	for name, edit := range map[string]func(w *assetsWorld){
		"Draft":                 func(w *assetsWorld) { w.draft = true },
		"Other, another commit": func(w *assetsWorld) { w.body = relBody(relOther) },
		"None":                  func(w *assetsWorld) { w.tagName = "v5.1.0-ezop.2" },
	} {
		t.Run("the release is "+name, func(t *testing.T) {
			w := newStampWorld(t)
			edit(w)
			var calls []runCall
			w.wantStampRefusal(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, ok, nil) })
			if len(calls) != 0 || len(w.api.downloads) != 0 {
				t.Fatalf("ran %d, downloaded %v", len(calls), w.api.downloads)
			}
		})
	}
	t.Run("the release is Unknown", func(t *testing.T) {
		w := newStampWorld(t)
		var calls []runCall
		w.wantStampRefusal(func(d *guardDeps) {
			d.gh.(*assetAPI).fail[1] = true
			d.runProgram = recordingRunner(&calls, ok, nil)
		})
		if len(calls) != 0 {
			t.Fatal("the binary ran")
		}
	})
	t.Run("the download fails", func(t *testing.T) {
		w := newStampWorld(t)
		var calls []runCall
		w.wantStampRefusal(func(d *guardDeps) {
			d.gh.(*assetAPI).failOn[101] = true
			d.runProgram = recordingRunner(&calls, ok, nil)
		})
		if len(calls) != 0 {
			t.Fatal("the binary ran")
		}
		if names := entriesOf(t, w.env["RUNNER_TEMP"]); len(names) != 0 {
			t.Fatalf("files %v", names)
		}
	})
	t.Run("the download is empty", func(t *testing.T) {
		w := newStampWorld(t)
		w.files[101] = nil
		var calls []runCall
		w.wantStampRefusal(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, ok, nil) })
		if len(calls) != 0 {
			t.Fatal("the binary ran")
		}
	})
	t.Run("the reader cannot download", func(t *testing.T) {
		w := newStampWorld(t)
		var calls []runCall
		w.wantStampRefusal(func(d *guardDeps) {
			d.gh = d.gh.(*assetAPI).fakePages
			d.runProgram = recordingRunner(&calls, ok, nil)
		})
		if len(calls) != 0 {
			t.Fatal("the binary ran")
		}
	})
	t.Run("no GitHub reader and no environment", func(t *testing.T) {
		for _, mutate := range []func(d *guardDeps){func(d *guardDeps) { d.gh = nil }, func(d *guardDeps) { d.getenv = nil }} {
			w := newStampWorld(t)
			w.wantStampRefusal(mutate)
		}
	})
	t.Run("RUNNER_TEMP is not usable", func(t *testing.T) {
		for name, edit := range map[string]func(w *assetsWorld){
			"missing":      func(w *assetsWorld) { delete(w.env, "RUNNER_TEMP") },
			"relative":     func(w *assetsWorld) { w.env["RUNNER_TEMP"] = "tmp" },
			"not existing": func(w *assetsWorld) { w.env["RUNNER_TEMP"] = filepath.Join(w.env["RUNNER_TEMP"], "absent") },
		} {
			w := newStampWorld(t)
			edit(w)
			var calls []runCall
			w.wantStampRefusal(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, ok, nil) })
			if len(calls) != 0 {
				t.Fatalf("%s: the binary ran", name)
			}
		}
	})
	t.Run("an existing file is never overwritten and the binary does not run", func(t *testing.T) {
		w := newStampWorld(t)
		path := filepath.Join(w.env["RUNNER_TEMP"], stampFile)
		if err := os.WriteFile(path, []byte("earlier"), 0o600); err != nil {
			t.Fatal(err)
		}
		var calls []runCall
		w.wantStampRefusal(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, ok, nil) })
		if got, _ := os.ReadFile(path); string(got) != "earlier" || len(calls) != 0 {
			t.Fatalf("file %q, executions %d", got, len(calls))
		}
	})
	t.Run("a symlink at the path is not followed", func(t *testing.T) {
		w := newStampWorld(t)
		target := filepath.Join(t.TempDir(), "target")
		if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(w.env["RUNNER_TEMP"], stampFile)); err != nil {
			t.Fatal(err)
		}
		var calls []runCall
		w.wantStampRefusal(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, ok, nil) })
		if got, _ := os.ReadFile(target); string(got) != "keep" || len(calls) != 0 {
			t.Fatalf("target %q, executions %d", got, len(calls))
		}
	})
	t.Run("control: the same world passes", func(t *testing.T) {
		w := newStampWorld(t)
		var calls []runCall
		if code, out, errb := w.runStamp(func(d *guardDeps) { d.runProgram = recordingRunner(&calls, ok, nil) }); code != 0 {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
	})
}

// The one real execution: the downloaded bytes are a shell script that stands in
// for the product binary. It answers with the version only when it is called as
// `version -o json`, with no credential in its environment and a home inside
// RUNNER_TEMP.
func TestRunStampExecutesTheDownloadedFile(t *testing.T) {
	script := func(dir, version string) []byte {
		return []byte(`#!/bin/sh
if [ -n "$GITHUB_TOKEN" ] || [ -n "$GH_TOKEN" ] || [ -n "$PROTON_CLI_TEST_USER" ]; then echo '{"version":"leaked"}'; exit 0; fi
if [ "$#" != 3 ] || [ "$1" != version ] || [ "$2" != -o ] || [ "$3" != json ]; then echo '{"version":"bad arguments"}'; exit 0; fi
case "$HOME" in "` + dir + `"/*) ;; *) echo '{"version":"home outside"}'; exit 0 ;; esac
echo '{"version":"` + version + `","go":"go1.27.0","platform":"linux/amd64"}'
`)
	}
	t.Run("a binary that reports the version passes", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "SECRET-TOKEN")
		t.Setenv("GH_TOKEN", "SECRET-TOKEN")
		t.Setenv("PROTON_CLI_TEST_USER", "SECRET-USER")
		w := newStampWorld(t)
		w.files[101] = script(w.env["RUNNER_TEMP"], stampVersion)
		code, out, errb := w.runStamp(nil)
		if code != 0 || errb != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
		}
		info, err := os.Stat(filepath.Join(w.env["RUNNER_TEMP"], stampFile))
		if err != nil || info.Mode().Perm()&0o077 != 0 || info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("%v, %v", info, err)
		}
	})
	t.Run("a binary that reports another version fails without echoing", func(t *testing.T) {
		w := newStampWorld(t)
		w.files[101] = script(w.env["RUNNER_TEMP"], "5.1.0")
		w.wantStampRefusal(nil)
	})
	t.Run("a failing binary fails without echoing its output", func(t *testing.T) {
		w := newStampWorld(t)
		w.files[101] = []byte("#!/bin/sh\necho SECRET-BINARY-OUTPUT\necho SECRET-BINARY-OUTPUT >&2\nexit 3\n")
		w.wantStampRefusal(nil)
	})
	t.Run("a file the system cannot execute fails", func(t *testing.T) {
		w := newStampWorld(t)
		w.files[101] = []byte("SECRET-BINARY-OUTPUT is not a program")
		w.wantStampRefusal(nil)
	})
	t.Run("an answer larger than the limit fails", func(t *testing.T) {
		w := newStampWorld(t)
		w.files[101] = []byte("#!/bin/sh\nyes SECRET-BINARY-OUTPUT | head -c 300000\n")
		w.wantStampRefusal(nil)
	})
}

// ---- dispatch of the three commands ----

func TestRunDispatchInRunCommands(t *testing.T) {
	for _, name := range []string{"notes", "assets", "stamp"} {
		for label, args := range map[string][]string{
			"bare":                   {name},
			"an extra argument":      {name, "--in-run", "extra"},
			"an argument first":      {name, "x", "--in-run"},
			"another flag":           {name, "--bogus"},
			"the flag twice":         {name, "--in-run", "--in-run"},
			"the guard spelling":     {name, "--recheck"},
			"an empty argument":      {name, ""},
			"the flag with a value":  {name, "--in-run=1"},
			"a similar flag":         {name, "-in-run"},
			"the flag in upper case": {name, "--IN-RUN"},
		} {
			t.Run(name+" "+label+" is usage", func(t *testing.T) {
				g := newFakeGit(cleanTree(t))
				d, out, errb := testDeps(g)
				api := &assetAPI{fakePages: pagesOf("[]")}
				d.gh = api
				d.getenv = func(string) string { t.Fatal("the environment was read"); return "" }
				if code := run(args, d); code != 2 {
					t.Fatalf("exit %d", code)
				}
				want := "fork-release: usage: fork-release " + name + " --in-run\n"
				if out.Len() != 0 || errb.String() != want || len(g.calls) != 0 || len(api.calls) != 0 {
					t.Fatalf("stdout %q stderr %q git %v gh %v", out, errb, g.calls, api.calls)
				}
			})
		}
		t.Run(name+" --in-run is dispatched to its command", func(t *testing.T) {
			d, out, errb := testDeps(newFakeGit(cleanTree(t)))
			// With no environment each command refuses under its own name.
			if code := run([]string{name, "--in-run"}, d); code != 1 || out.Len() != 0 {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errb)
			}
			if !strings.HasPrefix(errb.String(), "fork-release: "+name+": ") {
				t.Fatalf("stderr %q", errb)
			}
		})
	}
}
