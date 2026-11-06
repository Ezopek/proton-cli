package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"regexp"
	"strings"
)

// goreleaserPinPattern is the only shape of the workflow's GoReleaser pin: an
// exact version with a v, never a range.
var goreleaserPinPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// toolchainPaths are the only files of the tree the toolchain check opens.
var toolchainPaths = []string{"go.mod", "devbox.lock"}

// checkToolchainAt reads go.mod and devbox.lock from the tree of the commit e,
// never the working tree, and runs checkToolchain on them. A file that is
// missing, is not a regular file or cannot be read refuses.
func checkToolchainAt(g gitRepo, e, pin string) error {
	view, err := commitTreeFS(g, e, toolchainPaths)
	if err != nil {
		return refusal{"toolchain", "could not read the tree of the commit"}
	}
	goMod, err := readRegular(view, "go.mod")
	if err != nil {
		return refusal{"toolchain", "go.mod is missing or is not a readable regular file"}
	}
	lock, err := readRegular(view, "devbox.lock")
	if err != nil {
		return refusal{"toolchain", "devbox.lock is missing or is not a readable regular file"}
	}
	return checkToolchain(goMod, lock, pin)
}

// readRegular reads a file that must be a regular file.
func readRegular(view fs.FS, name string) ([]byte, error) {
	f, err := view.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	return io.ReadAll(f)
}

// checkToolchain requires the build toolchain to be one: exactly one key of
// devbox.lock's packages that starts with "goreleaser@" has the pin (without
// its v) as its version, and exactly one key that starts with "go@" has the
// go directive of go.mod as its version. Versions are compared as exact
// strings. Anything missing, repeated or unreadable refuses, and a reason never
// quotes the files.
func checkToolchain(goMod, devboxLock []byte, pin string) error {
	if !goreleaserPinPattern.MatchString(pin) {
		return refusal{"toolchain", "the GoReleaser pin is not an exact version (vX.Y.Z)"}
	}
	goVersion, err := goDirective(goMod)
	if err != nil {
		return refusal{"toolchain", err.Error()}
	}
	keys, err := scanLockPackages(devboxLock)
	if err != nil {
		return refusal{"toolchain", err.Error()}
	}
	for _, want := range []struct{ prefix, version, what string }{
		{"goreleaser@", strings.TrimPrefix(pin, "v"), "goreleaser"},
		{"go@", goVersion, "go"},
	} {
		var found []lockKey
		for _, k := range keys {
			if strings.HasPrefix(k.name, want.prefix) {
				found = append(found, k)
			}
		}
		switch {
		case len(found) == 0:
			return refusal{"toolchain", "devbox.lock has no " + want.what + " package"}
		case len(found) > 1:
			return refusal{"toolchain", "devbox.lock has more than one " + want.what + " package"}
		case !found[0].hasVersion:
			return refusal{"toolchain", "the " + want.what + " package of devbox.lock has no version"}
		case found[0].version != want.version:
			return refusal{"toolchain", "the " + want.what + " version of devbox.lock is not " + want.version}
		}
	}
	return nil
}

// goDirective returns the version of the one go line of a go.mod. A toolchain
// line is not a go line; comments and carriage returns are ignored.
func goDirective(goMod []byte) (string, error) {
	var version string
	var count int
	for _, line := range strings.Split(string(goMod), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "go" {
			continue
		}
		count++
		if len(fields) != 2 {
			return "", errors.New("the go directive of go.mod is malformed")
		}
		version = fields[1]
	}
	if count != 1 {
		return "", errors.New("go.mod does not have exactly one go directive")
	}
	return version, nil
}

// lockKey is a key of devbox.lock's packages that the check looks at.
type lockKey struct {
	name       string
	version    string
	hasVersion bool
}

var errLock = errors.New("devbox.lock cannot be read as a package lock")

// scanLockPackages reads the keys of the top-level "packages" object from the
// token stream, so a repeated key is seen instead of silently overwritten. It
// returns the "go@" and "goreleaser@" keys with their version fields. The
// document must be exactly one object, "packages" must appear once, no key of
// it may repeat, and a version, where present, must be a string.
func scanLockPackages(data []byte) ([]lockKey, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var keys []lockKey
	var seenPackages bool
	err := walkObject(dec, func(key string) error {
		if key != "packages" {
			return skipValue(dec)
		}
		if seenPackages {
			return errLock
		}
		seenPackages = true
		seen := map[string]bool{}
		return walkObject(dec, func(name string) error {
			if seen[name] {
				return errLock
			}
			seen[name] = true
			if !strings.HasPrefix(name, "go@") && !strings.HasPrefix(name, "goreleaser@") {
				return skipValue(dec)
			}
			k := lockKey{name: name}
			var seenVersion bool
			err := walkObject(dec, func(field string) error {
				if field != "version" {
					return skipValue(dec)
				}
				if seenVersion {
					return errLock
				}
				seenVersion = true
				tok, err := dec.Token()
				v, ok := tok.(string)
				if err != nil || !ok {
					return errLock
				}
				k.version, k.hasVersion = v, true
				return nil
			})
			keys = append(keys, k)
			return err
		})
	})
	if err != nil || !seenPackages {
		return nil, errLock
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errLock
	}
	return keys, nil
}

// walkObject reads one JSON object from the token stream and calls fn for each
// key, which must consume the key's value.
func walkObject(dec *json.Decoder, fn func(key string) error) error {
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return errLock
	}
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return errLock
		}
		if err := fn(key); err != nil {
			return err
		}
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return errLock
	}
	return nil
}

// skipValue consumes one JSON value of any kind.
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return errLock
	}
	d, ok := tok.(json.Delim)
	if !ok || (d != '{' && d != '[') {
		return nil
	}
	for depth := 1; depth > 0; {
		tok, err := dec.Token()
		if err != nil {
			return errLock
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}
