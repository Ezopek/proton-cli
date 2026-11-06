package main

import (
	"strings"
	"testing"
)

const (
	goodMod  = "module example.invalid/m\n\ngo 1.27.0\n\nrequire example.invalid/dep v1.0.0\n"
	goodLock = `{"lockfile_version":"1","packages":{` +
		`"go@latest":{"last_modified":"x","version":"1.27.0"},` +
		`"golangci-lint@latest":{"version":"2.13.1"},` +
		`"goreleaser@latest":{"version":"2.17.1","systems":{"x86_64-linux":{"outputs":[]}}}}}`
	goodPin = "v2.17.1"
)

func lockWith(packages string) string {
	return `{"lockfile_version":"1","packages":{` + packages + `}}`
}

func TestCheckToolchain(t *testing.T) {
	t.Run("one matching key of each kind passes", func(t *testing.T) {
		if err := checkToolchain([]byte(goodMod), []byte(goodLock), goodPin); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("keys are matched by prefix and versions by their field", func(t *testing.T) {
		lock := lockWith(`"go@1.27.0":{"version":"1.27.0"},"goreleaser@2.17.1":{"version":"2.17.1"}`)
		if err := checkToolchain([]byte(goodMod), []byte(lock), goodPin); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("other keys, other places and other fields are not counted", func(t *testing.T) {
		lock := `{"go@top":{"version":"9"},"lockfile_version":"1","packages":{` +
			`"go@latest":{"version":"1.27.0","nested":{"version":"0","go@x":1}},` +
			`"gotools@latest":{"version":"0.1"},"goreleaser@latest":{"version":"2.17.1"}}}`
		if err := checkToolchain([]byte(goodMod), []byte(lock), goodPin); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a toolchain line, comments and carriage returns in go.mod are fine", func(t *testing.T) {
		mod := "module m\r\n// go 9.9.9\r\ngo 1.27.0 // the language\r\ntoolchain go1.27.1\r\n"
		if err := checkToolchain([]byte(mod), []byte(goodLock), goodPin); err != nil {
			t.Fatal(err)
		}
	})

	mods := map[string]string{
		"go.mod with no go directive":   "module m\n",
		"go.mod with two go directives": "module m\ngo 1.27.0\ngo 1.27.0\n",
		"go.mod with a different go":    "module m\ngo 1.27\n",
		"go.mod with a newer go":        "module m\ngo 1.28.0\n",
		"go.mod with an empty go":       "module m\ngo\n",
		"go.mod with an extra token":    "module m\ngo 1.27.0 extra\n",
		"an empty go.mod":               "",
		"a go line only in a comment":   "module m\n// go 1.27.0\n",
		"a toolchain line is not a go":  "module m\ntoolchain go1.27.0\n",
	}
	for name, mod := range mods {
		t.Run("refuses "+name, func(t *testing.T) {
			wantRefusal(t, checkToolchain([]byte(mod), []byte(goodLock), goodPin), "toolchain")
		})
	}

	locks := map[string]string{
		"no goreleaser key":               lockWith(`"go@latest":{"version":"1.27.0"}`),
		"no go key":                       lockWith(`"goreleaser@latest":{"version":"2.17.1"}`),
		"two goreleaser keys":             lockWith(`"go@latest":{"version":"1.27.0"},"goreleaser@latest":{"version":"2.17.1"},"goreleaser@2.17.1":{"version":"2.17.1"}`),
		"two go keys":                     lockWith(`"go@latest":{"version":"1.27.0"},"go@1.27.0":{"version":"1.27.0"},"goreleaser@latest":{"version":"2.17.1"}`),
		"a repeated go key":               lockWith(`"go@latest":{"version":"1.27.0"},"go@latest":{"version":"1.27.0"},"goreleaser@latest":{"version":"2.17.1"}`),
		"a repeated goreleaser key":       lockWith(`"go@latest":{"version":"1.27.0"},"goreleaser@latest":{"version":"2.17.1"},"goreleaser@latest":{"version":"2.17.1"}`),
		"a repeated unrelated key":        lockWith(`"go@latest":{"version":"1.27.0"},"goreleaser@latest":{"version":"2.17.1"},"jq@latest":{},"jq@latest":{}`),
		"goreleaser at another version":   lockWith(`"go@latest":{"version":"1.27.0"},"goreleaser@latest":{"version":"2.17.0"}`),
		"go at another version":           lockWith(`"go@latest":{"version":"1.26.9"},"goreleaser@latest":{"version":"2.17.1"}`),
		"go with a prefix of the version": lockWith(`"go@latest":{"version":"1.27"},"goreleaser@latest":{"version":"2.17.1"}`),
		"goreleaser with a v":             lockWith(`"go@latest":{"version":"1.27.0"},"goreleaser@latest":{"version":"v2.17.1"}`),
		"goreleaser without version":      lockWith(`"go@latest":{"version":"1.27.0"},"goreleaser@latest":{"resolved":"x"}`),
		"go without version":              lockWith(`"go@latest":{},"goreleaser@latest":{"version":"2.17.1"}`),
		"a numeric version":               lockWith(`"go@latest":{"version":1.27},"goreleaser@latest":{"version":"2.17.1"}`),
		"a null version":                  lockWith(`"go@latest":{"version":null},"goreleaser@latest":{"version":"2.17.1"}`),
		"a list version":                  lockWith(`"go@latest":{"version":["1.27.0"]},"goreleaser@latest":{"version":"2.17.1"}`),
		"a repeated version field":        lockWith(`"go@latest":{"version":"1.27.0","version":"1.27.0"},"goreleaser@latest":{"version":"2.17.1"}`),
		"a key that is not an object":     lockWith(`"go@latest":"1.27.0","goreleaser@latest":{"version":"2.17.1"}`),
		"no packages":                     `{"lockfile_version":"1"}`,
		"packages that is a list":         `{"packages":[]}`,
		"a repeated packages key":         `{"packages":` + `{"go@latest":{"version":"1.27.0"},"goreleaser@latest":{"version":"2.17.1"}}` + `,"packages":{}}`,
		"trailing data":                   goodLock + `{}`,
		"a second document":               goodLock + "\n" + goodLock,
		"top level list":                  `[]`,
		"not JSON":                        `go@latest`,
		"truncated":                       goodLock[:len(goodLock)-3],
		"empty":                           ``,
		"null":                            `null`,
	}
	for name, lock := range locks {
		t.Run("refuses "+name, func(t *testing.T) {
			wantRefusal(t, checkToolchain([]byte(goodMod), []byte(lock), goodPin), "toolchain")
		})
	}

	pins := map[string]string{
		"a pin of another version": "v2.17.0",
		"a pin without v":          "2.17.1",
		"a floating pin":           "~> v2",
		"a two-part pin":           "v2.17",
		"a padded pin":             " v2.17.1",
		"a pin with a newline":     "v2.17.1\n",
		"a pre-release pin":        "v2.17.1-rc.1",
		"an empty pin":             "",
		"a latest pin":             "latest",
	}
	for name, pin := range pins {
		t.Run("refuses "+name, func(t *testing.T) {
			wantRefusal(t, checkToolchain([]byte(goodMod), []byte(goodLock), pin), "toolchain")
		})
	}

	t.Run("a refusal never quotes the files", func(t *testing.T) {
		lock := lockWith(`"go@latest":{"version":"SECRET-MARKER"},"goreleaser@latest":{"version":"2.17.1"}`)
		err := checkToolchain([]byte(goodMod), []byte(lock), goodPin)
		wantRefusal(t, err, "toolchain")
		if strings.Contains(err.Error(), "SECRET-MARKER") {
			t.Fatalf("quotes the lock: %v", err)
		}
	})
}

func TestCheckToolchainAt(t *testing.T) {
	// commitWith commits a go.mod and a devbox.lock made by the setup and
	// returns the checked commit.
	commitWith := func(t *testing.T, setup func(s *scratch)) (execGit, string) {
		t.Helper()
		s := newScratch(t)
		setup(s)
		sha := s.commit("tree")
		return s.adapter(), sha
	}
	both := func(s *scratch) {
		s.write("go.mod", goodMod)
		s.write("devbox.lock", goodLock)
	}
	t.Run("reads both files from the tree of the commit", func(t *testing.T) {
		g, sha := commitWith(t, both)
		if err := checkToolchainAt(g, sha, goodPin); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("the working tree is not read", func(t *testing.T) {
		g, sha := commitWith(t, both)
		s := &scratch{t: t, dir: g.dir, env: g.env}
		s.write("devbox.lock", lockWith(`"go@latest":{"version":"0"}`))
		s.write("go.mod", "module m\ngo 0.0.1\n")
		if err := checkToolchainAt(g, sha, goodPin); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a pin that disagrees with the committed lock refuses", func(t *testing.T) {
		g, sha := commitWith(t, both)
		wantRefusal(t, checkToolchainAt(g, sha, "v2.18.0"), "toolchain")
	})
	t.Run("a missing go.mod refuses", func(t *testing.T) {
		g, sha := commitWith(t, func(s *scratch) {
			s.write("devbox.lock", goodLock)
			s.write("README.md", "x")
		})
		wantRefusal(t, checkToolchainAt(g, sha, goodPin), "toolchain")
	})
	t.Run("a missing devbox.lock refuses", func(t *testing.T) {
		g, sha := commitWith(t, func(s *scratch) {
			s.write("go.mod", goodMod)
			s.write("README.md", "x")
		})
		wantRefusal(t, checkToolchainAt(g, sha, goodPin), "toolchain")
	})
	// The symlink entry is made in Git's index (mode 120000 over a valid
	// regular target), so no operating-system symlink is needed. A regular
	// commit of the same files is the positive control, then the link replaces
	// one file and is refused although its target is a valid file.
	for _, link := range []string{"go.mod", "devbox.lock"} {
		t.Run("a symlink in place of "+link+" refuses", func(t *testing.T) {
			s := newScratch(t)
			both(s)
			target := goodMod
			if link == "devbox.lock" {
				target = goodLock
			}
			s.write("real", target)
			regular := s.commit("regular files")
			if err := checkToolchainAt(s.adapter(), regular, goodPin); err != nil {
				t.Fatalf("control: the regular tree is refused: %v", err)
			}
			blob := s.gitStdin("real", "hash-object", "-w", "--stdin")
			s.git("update-index", "--cacheinfo", "120000,"+blob+","+link)
			s.git("commit", "-q", "-m", "a symlink in place of "+link)
			linked := s.git("rev-parse", "HEAD")
			if mode := s.git("ls-tree", linked, link); !strings.HasPrefix(mode, "120000 ") {
				t.Fatalf("control: %s is not a symlink entry: %q", link, mode)
			}
			// The reason names the file kind, so a refusal of the link text as
			// garbage content would not pass.
			wantRefusal(t, checkToolchainAt(s.adapter(), linked, goodPin), "toolchain", link, "regular file")
		})
	}
	t.Run("a directory in place of a file refuses", func(t *testing.T) {
		g, sha := commitWith(t, func(s *scratch) {
			s.write("go.mod/inner", goodMod)
			s.write("devbox.lock", goodLock)
		})
		wantRefusal(t, checkToolchainAt(g, sha, goodPin), "toolchain")
	})
	t.Run("a commit git does not know refuses", func(t *testing.T) {
		g, _ := commitWith(t, both)
		wantRefusal(t, checkToolchainAt(g, strings.Repeat("ab", 20), goodPin), "toolchain")
	})
	t.Run("an executable file is still a regular file", func(t *testing.T) {
		s := newScratch(t)
		both(s)
		s.git("add", "-A")
		s.git("update-index", "--chmod=+x", "go.mod")
		s.git("commit", "-q", "-m", "an executable go.mod")
		sha := s.git("rev-parse", "HEAD")
		if mode := s.git("ls-tree", sha, "go.mod"); !strings.HasPrefix(mode, "100755 ") {
			t.Fatalf("control: go.mod is not an executable entry: %q", mode)
		}
		if err := checkToolchainAt(s.adapter(), sha, goodPin); err != nil {
			t.Fatal(err)
		}
	})
}
