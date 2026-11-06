package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/roman-16/proton-cli/internal/selfmanage"
)

// The in-run asset check and the stamping check read the release the workflow
// has just published. assets is presence-level: it looks at the asset list and
// at checksums.txt, and downloads nothing else. stamp downloads one binary and
// runs it.

const (
	// checksumsName is the name of the checksum list GoReleaser uploads.
	checksumsName = "checksums.txt"
	// assetUploaded is the one asset state that means the upload finished.
	assetUploaded = "uploaded"
)

// platforms are the six operating system and architecture pairs a fork release
// ships a raw binary for: Linux, macOS and Windows on amd64 and arm64.
var platforms = [][2]string{
	{"linux", "amd64"}, {"linux", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"},
}

// binaryNames are the six binary names of platforms. They come from
// selfmanage.AssetName and nowhere else: this file spells none of them.
func binaryNames() ([]string, error) { return binaryNamesFor(platforms) }

// binaryNamesFor is binaryNames for a given list of pairs. A pair that
// selfmanage.AssetName refuses fails the whole list.
func binaryNamesFor(pairs [][2]string) ([]string, error) {
	names := make([]string, 0, len(pairs))
	for _, p := range pairs {
		name, err := selfmanage.AssetName(p[0], p[1])
		if err != nil {
			return nil, errors.New("no binary name for the platform " + p[0] + "/" + p[1])
		}
		names = append(names, name)
	}
	return names, nil
}

// sha256Hex is a SHA-256 digest as checksums.txt writes it.
var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// parseChecksums is the one strict reader of checksums.txt, shared by the
// in-run asset check and the later verify. Every line is `<64 lowercase hex>`,
// two spaces and a file name. The text may end with one line feed, which closes
// its last line. No other empty line, no carriage return or other control
// character, no name with a leading or trailing space, and no name listed twice
// is accepted, and an empty file is not a list. It answers the hash of each name.
// An error says what kind of fault was found and never quotes the file.
func parseChecksums(data []byte) (map[string]string, error) {
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil, errors.New("the checksum list is empty")
	}
	sums := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		hash, name, ok := strings.Cut(line, "  ")
		if !ok || !sha256Hex.MatchString(hash) {
			return nil, errors.New("the checksum list has a line that is not `<hash>  <name>`")
		}
		if name == "" || name != strings.TrimSpace(name) || strings.ContainsFunc(name, unicode.IsControl) {
			return nil, errors.New("the checksum list has a line with an unusable file name")
		}
		if _, dup := sums[name]; dup {
			return nil, errors.New("the checksum list names a file twice")
		}
		sums[name] = hash
	}
	return sums, nil
}

// publishedSame reads GITHUB_REF_NAME and GITHUB_SHA and the one release lookup,
// and requires the release of the tag to be Published-Same for that commit. The
// reason of a refusal comes from a closed vocabulary.
func publishedSame(d guardDeps, check string) (tag, e string, lk releaseLookup, err error) {
	tag, e = getenvOf(d, "GITHUB_REF_NAME"), getenvOf(d, "GITHUB_SHA")
	if checkTagPattern(tag) != nil {
		return "", "", lk, refusal{check, "GITHUB_REF_NAME is not a Release Tag"}
	}
	if !fullSHA.MatchString(e) {
		return "", "", lk, refusal{check, "GITHUB_SHA is not a full lowercase commit SHA"}
	}
	if d.gh == nil {
		return "", "", lk, refusal{check, "there is no GitHub reader, so the release cannot be read"}
	}
	lk = lookupRelease(d.gh, tag, e)
	switch lk.State {
	case releasePublishedSame:
		return tag, e, lk, nil
	case releaseUnknown:
		return "", "", lk, refusal{check, "the release of the tag could not be read: " + lk.Reason}
	}
	return "", "", lk, refusal{check, "the release of the tag is " + lk.State.String() + ", not Published-Same for this commit"}
}

// requireUploaded finds a named asset in the release's list and requires it to
// be uploaded.
func requireUploaded(lk releaseLookup, check, name string) (releaseAsset, error) {
	if lk.AssetsReason != "" {
		return releaseAsset{}, refusal{check, "the asset list of the release cannot be trusted: " + lk.AssetsReason}
	}
	for _, a := range lk.Assets {
		if a.Name != name {
			continue
		}
		if a.State != assetUploaded {
			return releaseAsset{}, refusal{check, "the asset " + name + " is not uploaded"}
		}
		return a, nil
	}
	return releaseAsset{}, refusal{check, "the release has no asset " + name}
}

// downloadAsset downloads one asset of the release by its id through the reader
// of the run, when it can download.
func downloadAsset(d guardDeps, a releaseAsset) ([]byte, error) {
	dl, ok := d.gh.(ghDownloader)
	if !ok {
		return nil, errors.New("the GitHub reader cannot download")
	}
	return dl.Download("releases/assets/" + strconv.FormatInt(a.ID, 10))
}

// runAssets is assets --in-run. It reads GITHUB_REF_NAME and GITHUB_SHA,
// requires the release of the tag to be Published-Same for that commit, every
// one of the six binaries and checksums.txt to be uploaded, and checksums.txt to
// list each binary. It downloads checksums.txt and nothing else; extra assets
// are ignored.
func runAssets(d guardDeps) int {
	tag, _, lk, err := publishedSame(d, "assets")
	if err != nil {
		return refuseWith(d, err)
	}
	names, err := binaryNames()
	if err != nil {
		return refuseWith(d, refusal{"assets", err.Error()})
	}
	var sumsAsset releaseAsset
	for _, name := range append(names, checksumsName) {
		a, err := requireUploaded(lk, "assets", name)
		if err != nil {
			return refuseWith(d, err)
		}
		if name == checksumsName {
			sumsAsset = a
		}
	}
	data, err := downloadAsset(d, sumsAsset)
	if err != nil {
		return refuseWith(d, refusal{"assets", "could not download " + checksumsName})
	}
	sums, err := parseChecksums(data)
	if err != nil {
		return refuseWith(d, refusal{"assets", checksumsName + " is not valid: " + err.Error()})
	}
	for _, name := range names {
		if _, ok := sums[name]; !ok {
			return refuseWith(d, refusal{"assets", checksumsName + " does not list " + name})
		}
	}
	if _, err := fmt.Fprintf(d.stdout, "fork-release: assets: ok: %s holds %d binaries and %s listing each\n", tag, len(names), checksumsName); err != nil {
		return refuseWith(d, refusal{"output", "could not write the result"})
	}
	return exitPass
}

const (
	// stampTimeout bounds the one run of the downloaded binary.
	stampTimeout = 30 * time.Second
	// maxProgramOutput bounds what a run of the binary may print; `version -o
	// json` is a few hundred bytes.
	maxProgramOutput = 64 << 10
	// stampFilePrefix starts the name of the file stamp downloads the binary to,
	// inside RUNNER_TEMP.
	stampFilePrefix = "stamp-"
)

// execProgram is the real program runner: it runs path with args and exactly the
// given environment, from the directory of the file, with no input, a time limit
// and a bound on standard output. Standard error is dropped. No error it returns
// carries the program's output.
func execProgram(path string, args, env []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), stampTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.Dir = filepath.Dir(path)
	cmd.WaitDelay = time.Second
	out := &limitWriter{max: maxProgramOutput}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		return nil, errors.New("the program did not run to a successful end")
	}
	return out.buf.Bytes(), nil
}

// reportedVersion reads the one `version` field of the JSON object that
// `proton version -o json` prints. The key must be spelled exactly, the value
// must be a string, and nothing may follow the object.
func reportedVersion(out []byte) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(out))
	var object map[string]any
	if err := dec.Decode(&object); err != nil {
		return "", false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", false
	}
	version, ok := object["version"].(string)
	return version, ok
}

// runStamp is stamp --in-run. It reads GITHUB_REF_NAME, GITHUB_SHA and
// RUNNER_TEMP, requires a Published-Same release for that commit, downloads the
// uploaded Linux amd64 binary into a new file in RUNNER_TEMP that only its owner
// can execute, runs it with `version -o json` under a minimal environment
// without credentials, and requires the reported version to equal the tag
// without its leading v. Every refusal is one line that never quotes the
// binary's output.
func runStamp(d guardDeps) int {
	tag, _, lk, err := publishedSame(d, "stamp")
	if err != nil {
		return refuseWith(d, err)
	}
	names, err := binaryNamesFor([][2]string{{"linux", "amd64"}})
	if err != nil {
		return refuseWith(d, refusal{"stamp", err.Error()})
	}
	asset, err := requireUploaded(lk, "stamp", names[0])
	if err != nil {
		return refuseWith(d, err)
	}
	dir, err := runnerTempDir(d, "stamp")
	if err != nil {
		return refuseWith(d, err)
	}
	data, err := downloadAsset(d, asset)
	if err != nil {
		return refuseWith(d, refusal{"stamp", "could not download " + names[0]})
	}
	if len(data) == 0 {
		return refuseWith(d, refusal{"stamp", "the downloaded " + names[0] + " is empty"})
	}

	path := filepath.Join(dir, stampFilePrefix+names[0])
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return refuseWith(d, refusal{"stamp", "could not create " + stampFilePrefix + names[0] + " in RUNNER_TEMP, which must not exist yet"})
	}
	_, werr := f.Write(data)
	if cerr := f.Chmod(0o700); werr == nil {
		werr = cerr
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
		return refuseWith(d, refusal{"stamp", "could not write " + stampFilePrefix + names[0]})
	}

	// The binary writes its run log under its configuration directory, so it
	// gets a home of its own, removed afterwards.
	home, err := os.MkdirTemp(dir, "stamp-home-")
	if err != nil {
		return refuseWith(d, refusal{"stamp", "could not make a home directory for the binary in RUNNER_TEMP"})
	}
	defer func() { _ = os.RemoveAll(home) }()
	env := []string{"PATH=/usr/bin:/bin", "HOME=" + home, "XDG_CONFIG_HOME=" + home, "TMPDIR=" + home}

	runner := d.runProgram
	if runner == nil {
		runner = execProgram
	}
	out, err := runner(path, []string{"version", "-o", "json"}, env)
	if err != nil {
		return refuseWith(d, refusal{"stamp", "the downloaded " + names[0] + " did not run `version -o json` to a successful end"})
	}
	version, ok := reportedVersion(out)
	if !ok {
		return refuseWith(d, refusal{"stamp", "the downloaded " + names[0] + " did not print a JSON object with a version"})
	}
	if want := strings.TrimPrefix(tag, "v"); version != want {
		return refuseWith(d, refusal{"stamp", "the downloaded " + names[0] + " does not report the version of the tag, " + want})
	}
	if _, err := fmt.Fprintf(d.stdout, "fork-release: stamp: ok: %s reports version %s\n", names[0], version); err != nil {
		return refuseWith(d, refusal{"output", "could not write the result"})
	}
	return exitPass
}
