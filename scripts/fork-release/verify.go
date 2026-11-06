package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/roman-16/proton-cli/internal/selfmanage"
)

// verify TAG GATES observes a published Fork Release and prints its Verification
// Record. This file holds the pure pieces of it, the two-way checksum
// comparison, the tool identity read from the tree of E and the record renderer;
// the command that makes the observations is below them.

// verifyRefusal is a refusal of verify, with a reason from a closed set. It never
// quotes a response, a URL, a file or the binary's output.
func verifyRefusal(reason string) error { return refusal{"verify", reason} }

// compareChecksums checks checksums.txt against the downloaded assets in both
// directions. hashes maps every downloaded asset (checksums.txt included) to the
// sha256 of its bytes; sums is the parsed checksums.txt. Every listed name must
// be a downloaded asset with the listed hash, and every binary must be listed.
// It answers the exceptions: the downloaded assets, other than checksums.txt,
// that the list does not name, sorted bytewise.
func compareChecksums(hashes, sums map[string]string, binaries []string) ([]string, error) {
	if _, ok := hashes[checksumsName]; !ok {
		return nil, verifyRefusal(checksumsName + " was not downloaded")
	}
	for name, want := range sums {
		got, ok := hashes[name]
		switch {
		case !ok:
			return nil, verifyRefusal(checksumsName + " lists a file that is not an asset of the release")
		case got != want:
			return nil, verifyRefusal("the bytes of a listed asset do not match their hash in " + checksumsName)
		}
	}
	for _, b := range binaries {
		if _, ok := sums[b]; !ok {
			return nil, verifyRefusal(checksumsName + " does not list one of the six binaries")
		}
	}
	var exceptions []string
	for name := range hashes {
		if _, listed := sums[name]; !listed && name != checksumsName {
			exceptions = append(exceptions, name)
		}
	}
	sort.Strings(exceptions)
	return exceptions, nil
}

// toolIdentity is the tool identity of the tree of E: the versions the toolchain
// check vouches for and the git blob ids of the two files the record binds.
type toolIdentity struct {
	GoVersion         string
	GoreleaserVersion string
	ConfigBlob        string
	LockBlob          string
}

// forkReleasePath is the workflow file whose pin verify reads.
const forkReleasePath = workflowsDir + "/" + forkRelease

// workflowPinKey is the one top-level env entry of fork-release.yml that holds
// the GoReleaser pin; the in-run guard reads the same name from its environment.
const workflowPinKey = "GORELEASER_PIN"

// workflowPin reads the GoReleaser pin of fork-release.yml: the one top-level
// env entry GORELEASER_PIN, a vX.Y.Z string, in a file that parses without a
// duplicate key, a second document or a merge key.
func workflowPin(data []byte) (string, error) {
	bad := func(reason string) (string, error) {
		return "", verifyRefusal("the GoReleaser pin of " + forkRelease + " " + reason)
	}
	value, issues := parseYAML(data)
	if len(issues) > 0 || value == nil {
		return bad("cannot be read: the file does not parse cleanly")
	}
	file, err := parser.ParseBytes(data, 0)
	if err != nil {
		return bad("cannot be read: the file does not parse")
	}
	merged := false
	for _, doc := range file.Docs {
		if doc.Body != nil {
			ast.Walk(mergeFinder{&merged}, doc.Body)
		}
	}
	if merged {
		return bad("cannot be read: the file uses a merge key")
	}
	top, _ := value.(map[string]any)
	env, _ := top["env"].(map[string]any)
	pin, ok := env[workflowPinKey].(string)
	if !ok || !goreleaserPinPattern.MatchString(pin) {
		return bad("is missing or is not an exact vX.Y.Z string in the top-level env")
	}
	return pin, nil
}

// mergeFinder records whether a YAML document has any merge key.
type mergeFinder struct{ found *bool }

func (m mergeFinder) Visit(n ast.Node) ast.Visitor {
	if mv, ok := n.(*ast.MappingValueNode); ok && isMergeKey(mv) {
		*m.found = true
	}
	return m
}

// blobOf is the git blob id of the regular file at path in a tree listing.
func blobOf(entries []treeEntry, path string) (string, bool) {
	for _, e := range entries {
		if e.path == path && e.typ == "blob" && (e.mode == "100644" || e.mode == "100755") && blobHexPattern.MatchString(e.oid) {
			return e.oid, true
		}
	}
	return "", false
}

// readToolIdentity reads the tool identity from the tree of the commit e, never
// the working tree: the Go version of go.mod, the Go and GoReleaser versions of
// devbox.lock, the workflow's GoReleaser pin and the blob ids of .goreleaser.yaml
// and devbox.lock. The toolchain check must pass with that pin, so the versions
// are the ones it vouches for. Anything missing, unreadable, malformed or
// inconsistent refuses.
func readToolIdentity(g gitRepo, e string) (toolIdentity, error) {
	paths := []string{"go.mod", "devbox.lock", goreleaserPath, forkReleasePath}
	entries, err := g.Tree(e, paths)
	if err != nil {
		return toolIdentity{}, verifyRefusal("could not read the tree of the commit")
	}
	view, err := buildTreeFS(entries, paths, g.Blob)
	if err != nil {
		return toolIdentity{}, verifyRefusal("could not read the tree of the commit")
	}
	var files [4][]byte
	for i, p := range paths {
		if files[i], err = readRegular(view, p); err != nil {
			return toolIdentity{}, verifyRefusal(p + " is missing or is not a readable regular file in the tree of the commit")
		}
	}
	goMod, lock, workflow := files[0], files[1], files[3]
	configBlob, okConfig := blobOf(entries, goreleaserPath)
	lockBlob, okLock := blobOf(entries, "devbox.lock")
	if !okConfig || !okLock {
		return toolIdentity{}, verifyRefusal("could not read the blob ids of the configuration and the lock")
	}
	pin, err := workflowPin(workflow)
	if err != nil {
		return toolIdentity{}, err
	}
	if err := checkToolchain(goMod, lock, pin); err != nil {
		var r refusal
		if errors.As(err, &r) {
			return toolIdentity{}, verifyRefusal("toolchain: " + r.reason)
		}
		return toolIdentity{}, verifyRefusal("the toolchain check failed")
	}
	goVersion, err := goDirective(goMod)
	if err != nil {
		return toolIdentity{}, verifyRefusal("go.mod has no usable go directive")
	}
	return toolIdentity{
		GoVersion:         goVersion,
		GoreleaserVersion: strings.TrimPrefix(pin, "v"),
		ConfigBlob:        configBlob,
		LockBlob:          lockBlob,
	}, nil
}

// verifiedFacts is everything a verified record says, as observed. Time is the
// UTC instant the record is printed at, already formatted.
type verifiedFacts struct {
	Tag           string
	Commit        string
	Time          string
	Assets        map[string]string // name -> sha256 of the downloaded bytes, checksums.txt included
	Exceptions    []string
	HostPlatform  string
	ProtonVersion string
	Tools         toolIdentity
	RunID         string
	RunAttempt    string
	RunConclusion string
	// Attested are the six capture values, keyed by the capture names.
	Attested map[string]string
}

// renderVerifiedRecord renders the record of the Story 1.4 grammar in its field
// order: the header, the commit, one asset line per downloaded asset sorted
// bytewise, then the tail keys of the record. The order comes from the record's
// own key lists. Before it answers, the text is parsed with the Story 1.4 record
// parser, and any finding refuses, so a record that the landing validator would
// reject is never produced.
func renderVerifiedRecord(f verifiedFacts) ([]byte, error) {
	values := map[string]string{
		"schema": recordSchemaVersion, "tag": f.Tag, "status": statusVerified, "time": f.Time,
		"commit":                 f.Commit,
		"exceptions":             strings.Join(f.Exceptions, ","),
		"host-platform":          f.HostPlatform,
		"proton-version":         f.ProtonVersion,
		"goreleaser-config-blob": f.Tools.ConfigBlob,
		"devbox-lock-blob":       f.Tools.LockBlob,
		"go-version":             f.Tools.GoVersion,
		"goreleaser-version":     f.Tools.GoreleaserVersion,
		"run-id":                 f.RunID,
		"run-attempt":            f.RunAttempt,
		"run-conclusion":         f.RunConclusion,
	}
	names := captureKeys()
	if len(f.Attested) != len(names) {
		return nil, verifyRefusal("the gate capture does not hold exactly the six values")
	}
	for _, name := range names {
		v, ok := f.Attested[name]
		if !ok {
			return nil, verifyRefusal("the gate capture lacks one of the six values")
		}
		values[attestedPrefix+name] = v
	}
	for _, v := range values {
		if strings.ContainsAny(v, "\r\n") {
			return nil, verifyRefusal("a value of the record has a line end")
		}
	}

	var b strings.Builder
	line := func(key string) error {
		v, ok := values[key]
		if !ok {
			return verifyRefusal("the record lacks a value")
		}
		b.WriteString(key)
		b.WriteString(":")
		if v != "" {
			b.WriteString(" " + v)
		}
		b.WriteString("\n")
		return nil
	}
	for _, key := range slices.Concat(recordHeader, recordVerifiedHead) {
		if err := line(key); err != nil {
			return nil, err
		}
	}
	assets := make([]string, 0, len(f.Assets))
	for name, hash := range f.Assets {
		if !assetNamePattern.MatchString(name) {
			return nil, verifyRefusal("an asset name is not one the record can hold")
		}
		assets = append(assets, name)
		values[assetPrefix+name] = hash
	}
	sort.Strings(assets)
	for _, name := range assets {
		if err := line(assetPrefix + name); err != nil {
			return nil, err
		}
	}
	for _, key := range recordVerifiedTail {
		if err := line(key); err != nil {
			return nil, err
		}
	}

	data := []byte(b.String())
	if _, found := parseRecord(verificationDir+"/"+f.Tag+".md", f.Tag, data); len(found) > 0 {
		return nil, verifyRefusal("the record would not pass the record validator (" + found[0].Rule + ")")
	}
	return data, nil
}

// recordTimeLayout is the UTC instant of a record, to the second.
const recordTimeLayout = "2006-01-02T15:04:05Z"

// verifyDirPrefix starts the name of the private temporary directory the host
// binary runs in.
const verifyDirPrefix = "fork-release-verify-"

// hostVersion writes the downloaded host binary into a private temporary
// directory that only its owner can use, runs it with `version -o json` under a
// minimal environment without credentials, and answers the version it reports.
// The directory is removed before it returns, whatever the outcome. No refusal
// quotes the binary's output.
func hostVersion(d guardDeps, name string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", verifyRefusal("the downloaded host binary is empty")
	}
	dir, err := os.MkdirTemp("", verifyDirPrefix)
	if err != nil {
		return "", verifyRefusal("could not make a temporary directory for the host binary")
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return "", verifyRefusal("could not create the host binary in the temporary directory")
	}
	_, werr := f.Write(data)
	if cerr := f.Chmod(0o700); werr == nil {
		werr = cerr
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return "", verifyRefusal("could not write the host binary")
	}
	// The binary writes its run log under its configuration directory, so it gets
	// a home of its own inside the directory that is removed.
	home, err := os.MkdirTemp(dir, "home-")
	if err != nil {
		return "", verifyRefusal("could not make a home directory for the host binary")
	}
	env := []string{"PATH=/usr/bin:/bin", "HOME=" + home, "XDG_CONFIG_HOME=" + home, "TMPDIR=" + home}

	runner := d.runProgram
	if runner == nil {
		runner = execProgram
	}
	out, err := runner(path, []string{"version", "-o", "json"}, env)
	if err != nil {
		return "", verifyRefusal("the host binary did not run `version -o json` to a successful end")
	}
	version, ok := reportedVersion(out)
	if !ok {
		return "", verifyRefusal("the host binary did not print a JSON object with a version")
	}
	return version, nil
}

// landedAncestor requires the commit e to be an ancestor of the fetched tip of
// ezop; only exit status 0 passes, and status 1 and any other status refuse
// differently.
func landedAncestor(g gitRepo, e, tip string) error {
	status, err := g.IsAncestor(e, tip)
	if err != nil {
		status = -1
	}
	switch status {
	case 0:
		return nil
	case 1:
		return verifyRefusal("the commit of the tag is not an ancestor of origin/ezop")
	}
	return verifyRefusal(fmt.Sprintf("cannot decide whether the commit of the tag is an ancestor of origin/ezop (git exit status %d)", status))
}

// runVerifyTag is verify TAG GATES. It checks, in order: the tag pattern, the
// helper identity, the capture file, and then makes the seven groups of
// observations of AD-7, every one of them before it prints anything: the record
// is Absent and the release Published-Same for the commit E the tag peels to,
// with E landed on origin/ezop; the tool identity of the tree of E; every asset
// downloaded and checked against checksums.txt in both directions; the host
// binary's version; the run attempt the notes name and the run; and as the last
// two reads the release again and the listing of the runs for E. A failed read of
// any kind prints nothing and refuses, and the command is simply run again. The
// record is printed only after it has been parsed with the record parser; nothing
// is written but the temporary directory of the host binary, removed before
// return.
func runVerifyTag(tag, gatesPath string, d guardDeps) int {
	if checkTagPattern(tag) != nil {
		return refuseWith(d, verifyRefusal("TAG is not a Release Tag"))
	}
	_, tip, err := helperIdentity(d)
	if err != nil {
		return refuseWith(d, err)
	}
	if d.gh == nil {
		return refuseWith(d, verifyRefusal("there is no GitHub reader, so nothing can be read"))
	}

	// 1. The capture.
	raw, err := readCaptureFile(gatesPath)
	if err != nil {
		return refuseWith(d, err)
	}
	capture, err := parseCapture(raw)
	if err != nil {
		return refuseWith(d, err)
	}

	// 2. The record, the tag, the release and the ancestry of E.
	switch rec := lookupRecord(d.gh, tag); rec.State {
	case recordAbsent:
	case recordPresent:
		return refuseWith(d, verifyRefusal("a verification record already exists for the tag"))
	case recordNotFile:
		return refuseWith(d, verifyRefusal("something that is not a record file exists at the record path of the tag"))
	default:
		return refuseWith(d, verifyRefusal("the verification record state of the tag could not be read: "+rec.Reason))
	}
	read := readTag(d.git, tag)
	switch read.State {
	case tagPresent:
	case tagAbsent:
		return refuseWith(d, verifyRefusal("origin does not have the tag"))
	default:
		return refuseWith(d, verifyRefusal("the tag could not be read on origin"))
	}
	e := read.Peel
	if capture["gate-commit"] != e {
		return refuseWith(d, verifyRefusal("the gate-commit of the gate capture is not the commit of the tag"))
	}
	lk := lookupRelease(d.gh, tag, e)
	switch lk.State {
	case releasePublishedSame:
	case releaseUnknown:
		return refuseWith(d, verifyRefusal("the release of the tag could not be read: "+lk.Reason))
	default:
		return refuseWith(d, verifyRefusal("the release of the tag is "+lk.State.String()+", not Published-Same for the commit of the tag"))
	}
	if err := landedAncestor(d.git, e, tip); err != nil {
		return refuseWith(d, err)
	}

	// 3. The tool identity of the tree of E.
	tools, err := readToolIdentity(d.git, e)
	if err != nil {
		return refuseWith(d, err)
	}

	// 4. The assets, downloaded from the published release.
	if lk.AssetsReason != "" {
		return refuseWith(d, verifyRefusal("the asset list of the release cannot be trusted: "+lk.AssetsReason))
	}
	for _, a := range lk.Assets {
		if a.State != assetUploaded {
			return refuseWith(d, verifyRefusal("an asset of the release is not uploaded"))
		}
	}
	sumsAsset, err := requireUploaded(lk, "verify", checksumsName)
	if err != nil {
		return refuseWith(d, err)
	}
	binaries, err := binaryNames()
	if err != nil {
		return refuseWith(d, verifyRefusal(err.Error()))
	}
	host, err := selfmanage.AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return refuseWith(d, verifyRefusal("this platform has no release binary"))
	}
	hashes := map[string]string{}
	var sumsData, hostData []byte
	hostFound := false
	for _, a := range lk.Assets {
		data, err := downloadAsset(d, a)
		if err != nil {
			return refuseWith(d, verifyRefusal("could not download an asset of the release"))
		}
		sum := sha256.Sum256(data)
		hashes[a.Name] = hex.EncodeToString(sum[:])
		switch a.Name {
		case host:
			hostData, hostFound = data, true
		case sumsAsset.Name:
			sumsData = data
		}
	}
	if !hostFound {
		return refuseWith(d, verifyRefusal("the release has no binary for the host platform"))
	}
	sums, err := parseChecksums(sumsData)
	if err != nil {
		return refuseWith(d, verifyRefusal(checksumsName+" is not valid: "+err.Error()))
	}
	exceptions, err := compareChecksums(hashes, sums, binaries)
	if err != nil {
		return refuseWith(d, err)
	}

	// 5. The host binary reports the version of the tag.
	version, err := hostVersion(d, host, hostData)
	if err != nil {
		return refuseWith(d, err)
	}
	if want := strings.TrimPrefix(tag, "v"); version != want {
		return refuseWith(d, verifyRefusal("the host binary does not report the version of the tag"))
	}

	// 6. The run attempt the notes name, and the run.
	if !lk.RunValid {
		return refuseWith(d, verifyRefusal("the notes of the release do not carry exactly one valid Run line"))
	}
	runID, attemptNo, _ := strings.Cut(lk.RunValue, "/")
	workflow, cause := lookupWorkflowID(d.gh)
	if cause != "" {
		return refuseWith(d, verifyRefusal("the workflow could not be read: "+cause))
	}
	attempt, cause := lookupRunAttempt(d.gh, runID, attemptNo)
	if cause != "" {
		return refuseWith(d, verifyRefusal("the run attempt could not be read: "+cause))
	}
	if cause := checkAttempt(attempt, e, workflow); cause != "" {
		return refuseWith(d, verifyRefusal("the run attempt of the release does not qualify: "+cause))
	}
	runNow, cause := lookupRun(d.gh, runID)
	if cause != "" {
		return refuseWith(d, verifyRefusal("the run could not be read: "+cause))
	}
	if cause := runAttemptAtLeast(runNow, attemptNo); cause != "" {
		return refuseWith(d, verifyRefusal("the run does not qualify: "+cause))
	}

	// 7. The last two reads: the release again, then the runs for E.
	again := lookupRelease(d.gh, tag, e)
	switch {
	case again.State == releaseUnknown:
		return refuseWith(d, verifyRefusal("the release of the tag could not be read again: "+again.Reason))
	case again.State != releasePublishedSame:
		return refuseWith(d, verifyRefusal("the release of the tag is "+again.State.String()+" on the last read, not Published-Same"))
	case !again.RunValid || again.RunValue != lk.RunValue:
		return refuseWith(d, verifyRefusal("the Run line of the release changed during the check"))
	}
	listing := listRuns(d.gh, e)
	if listing.Reason != "" {
		return refuseWith(d, verifyRefusal("the runs of the workflow for the commit could not be listed: "+listing.Reason))
	}
	if !listing.AllTerminal() {
		return refuseWith(d, verifyRefusal("a run of the workflow for the commit is not over"))
	}
	// The run the notes name has qualified through the attempt read, with the
	// same workflow, event and commit the listing is filtered to, so a final
	// listing that does not hold it is not consistent with what was read: an
	// empty listing cannot show that no other attempt is in progress.
	if !slices.ContainsFunc(listing.Runs, func(r workflowRun) bool { return strconv.FormatInt(r.ID, 10) == runID }) {
		return refuseWith(d, verifyRefusal("the runs of the workflow for the commit do not include the run named by the notes"))
	}

	// Everything was observed: compose, validate and print.
	now := time.Now
	if d.clock != nil {
		now = d.clock
	}
	data, err := renderVerifiedRecord(verifiedFacts{
		Tag:           tag,
		Commit:        e,
		Time:          now().UTC().Format(recordTimeLayout),
		Assets:        hashes,
		Exceptions:    exceptions,
		HostPlatform:  runtime.GOOS + "/" + runtime.GOARCH,
		ProtonVersion: version,
		Tools:         tools,
		RunID:         runID,
		RunAttempt:    attemptNo,
		RunConclusion: attempt.Conclusion,
		Attested:      capture,
	})
	if err != nil {
		return refuseWith(d, err)
	}
	if _, err := d.stdout.Write(data); err != nil {
		return refuseWith(d, refusal{"output", "could not write the result"})
	}
	return exitPass
}
