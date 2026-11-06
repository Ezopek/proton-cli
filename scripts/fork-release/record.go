package main

import (
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The Verification Record is one plain-text file per Release Tag. This file
// owns its grammar: the closed key set below is the only list of keys there is,
// and fork/verification/README.md is checked against it by a test. The gate
// capture file that the Operator hands to verify uses the same keys without the
// attested. prefix.

// Rules a finding can carry.
const (
	ruleRecordEmpty      = "record-empty"
	ruleRecordChars      = "record-characters"
	ruleRecordLineEnds   = "record-line-ends"
	ruleRecordFinalLF    = "record-final-lf"
	ruleRecordBlankLine  = "record-blank-line"
	ruleRecordLine       = "record-line"
	ruleRecordKeyUnknown = "record-key-unknown"
	ruleRecordKeyRepeat  = "record-key-repeated"
	ruleRecordKeyOrder   = "record-key-order"
	ruleRecordKeyMissing = "record-key-missing"
	ruleRecordValue      = "record-value"
	ruleRecordInvariant  = "record-invariant"
	ruleRecordSurface    = "record-surface"
	ruleRecordKind       = "record-kind"
	ruleRecordName       = "record-name"
	ruleRecordSize       = "record-size"
)

// verificationDir is the directory that holds the records, relative to the
// repository root.
const verificationDir = "fork/verification"

// maxRecordSize bounds a record file; a real one is well under 4 KiB.
const maxRecordSize = 64 << 10

// recordHeader are the first four keys of every record, in this order.
var recordHeader = []string{"schema", "tag", "status", "time"}

// A verified record adds, after the header and in this order: recordVerifiedHead,
// one asset.<name> line per downloaded asset, then recordVerifiedTail.
var recordVerifiedHead = []string{"commit"}

var recordVerifiedTail = []string{
	"exceptions",
	"host-platform",
	"proton-version",
	"goreleaser-config-blob",
	"devbox-lock-blob",
	"go-version",
	"goreleaser-version",
	"run-id",
	"run-attempt",
	"run-conclusion",
	"attested.gate-commit",
	"attested.gates",
	"attested.lint-diff",
	"attested.go-version",
	"attested.goreleaser-version",
	"attested.snapshot-version",
}

// recordAbandonedKeys are the keys an abandoned record adds after the header.
var recordAbandonedKeys = []string{"reason"}

// recordReasons is the closed list of reasons an abandoned record can give.
var recordReasons = []string{"refused", "failed", "draft", "window-passed", "rebased", "operator-decision"}

// Pattern keys. asset.<name> is one line per downloaded asset; attested.<key>
// stands for the attested.* fields listed in recordVerifiedTail.
const (
	recordAssetPattern    = "asset.<name>"
	recordAttestedPattern = "attested.<key>"
	assetPrefix           = "asset."
	attestedPrefix        = "attested."
)

// recordFields is every concrete key of the closed set.
func recordFields() []string {
	var out []string
	out = append(out, recordHeader...)
	out = append(out, recordAbandonedKeys...)
	out = append(out, recordVerifiedHead...)
	out = append(out, recordVerifiedTail...)
	return out
}

// recordPatterns is every pattern key of the closed set.
func recordPatterns() []string {
	return []string{recordAssetPattern, recordAttestedPattern}
}

var (
	assetNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	sha256Pattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	blobHexPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	timePattern      = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)
	dottedPattern    = regexp.MustCompile(`^[0-9]+(\.[0-9]+)+$`)
	countPattern     = regexp.MustCompile(`^[1-9][0-9]*$`)
)

const (
	attestedGates       = "test-fast,lint,build,snapshot"
	attestedLintDiff    = "none"
	runConclusionOK     = "success"
	statusVerified      = "verified"
	statusAbandoned     = "abandoned"
	recordSchemaVersion = "1"
)

// record is a parsed Verification Record.
type record struct {
	// Status is the value of the status key, empty when it could not be read.
	Status string
	// Values maps every key of the record to its value.
	Values map[string]string
}

// recordLine is one line of a record: the 1-based line number, the key and the
// value.
type recordLine struct {
	n   int
	key string
	val string
}

// isRecordKey reports whether key belongs to the closed key set, the pattern
// keys included.
func isRecordKey(key string) bool {
	if slices.Contains(recordFields(), key) {
		return true
	}
	name, ok := strings.CutPrefix(key, assetPrefix)
	return ok && assetNamePattern.MatchString(name)
}

// parseRecord checks the bytes of the record file for the Release Tag wantTag.
// Every finding names path, the rule and a key or a line number, and never
// holds a byte of the file. It returns the parsed record along with the
// findings; the record is only meaningful when there are none.
func parseRecord(path, wantTag string, data []byte) (record, []Finding) {
	rec := record{Values: map[string]string{}}
	var found []Finding
	add := func(rule, detail string) { found = append(found, Finding{Path: path, Rule: rule, Detail: detail}) }

	if len(data) == 0 {
		add(ruleRecordEmpty, "the file is empty")
		return rec, found
	}
	// Bytes first: the file is printable ASCII with LF line ends.
	line := 1
	reportedCR, reportedChars := false, false
	for _, c := range data {
		switch {
		case c == '\n':
			line++
		case c == '\r':
			if !reportedCR {
				add(ruleRecordLineEnds, "line "+strconv.Itoa(line)+": a carriage return; line ends are LF")
				reportedCR = true
			}
		case c < 0x20 || c > 0x7e:
			if !reportedChars {
				add(ruleRecordChars, "line "+strconv.Itoa(line)+": a byte outside printable ASCII")
				reportedChars = true
			}
		}
	}
	if data[len(data)-1] != '\n' {
		add(ruleRecordFinalLF, "the file does not end with a line feed")
	}
	if len(found) > 0 {
		return rec, found
	}

	// Lines: one key: value each.
	var lines []recordLine
	for i, text := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		n := i + 1
		if text == "" {
			add(ruleRecordBlankLine, "line "+strconv.Itoa(n)+" is blank")
			continue
		}
		key, rest, ok := strings.Cut(text, ":")
		val := ""
		switch {
		case !ok || key == "" || text[0] == ' ':
			add(ruleRecordLine, "line "+strconv.Itoa(n)+" is not key: value")
			continue
		case rest == "":
		case rest[0] != ' ' || len(rest) == 1 || rest[1] == ' ' || rest[len(rest)-1] == ' ':
			add(ruleRecordLine, "line "+strconv.Itoa(n)+": the value must follow one space and carry no leading or trailing space")
			continue
		default:
			val = rest[1:]
		}
		lines = append(lines, recordLine{n: n, key: key, val: val})
	}
	seen := map[string]bool{}
	for _, l := range lines {
		switch {
		case !isRecordKey(l.key):
			add(ruleRecordKeyUnknown, "line "+strconv.Itoa(l.n)+": the key is not in the closed set")
		case seen[l.key]:
			add(ruleRecordKeyRepeat, "line "+strconv.Itoa(l.n)+": "+l.key+" is repeated")
		}
		seen[l.key] = true
	}
	if len(found) > 0 {
		return rec, found
	}

	// Order: the header, then the keys of the status.
	next := 0
	expect := func(key string) bool {
		if next >= len(lines) {
			add(ruleRecordKeyMissing, key+" is missing")
			return false
		}
		if lines[next].key != key {
			add(ruleRecordKeyOrder, "line "+strconv.Itoa(lines[next].n)+": expected "+key)
			return false
		}
		next++
		return true
	}
	for _, key := range recordHeader {
		if !expect(key) {
			return rec, found
		}
	}
	for _, l := range lines {
		rec.Values[l.key] = l.val
	}
	status := lines[2].val
	var plan []string
	switch status {
	case statusAbandoned:
		plan = recordAbandonedKeys
	case statusVerified:
	default:
		add(ruleRecordValue, "status must be "+statusVerified+" or "+statusAbandoned)
		return rec, found
	}
	rec.Status = status
	var assets []string
	if status == statusVerified {
		for _, key := range recordVerifiedHead {
			if !expect(key) {
				return rec, found
			}
		}
		for next < len(lines) && strings.HasPrefix(lines[next].key, assetPrefix) {
			name := strings.TrimPrefix(lines[next].key, assetPrefix)
			if len(assets) > 0 && name <= assets[len(assets)-1] {
				add(ruleRecordKeyOrder, "line "+strconv.Itoa(lines[next].n)+": asset names are not sorted")
				return rec, found
			}
			assets = append(assets, name)
			next++
		}
		plan = recordVerifiedTail
	}
	for _, key := range plan {
		if !expect(key) {
			return rec, found
		}
	}
	if next < len(lines) {
		add(ruleRecordKeyOrder, "line "+strconv.Itoa(lines[next].n)+": "+lines[next].key+" is not expected for status "+status)
		return rec, found
	}

	// Values and invariants.
	for _, l := range lines {
		if problem := recordValueProblem(l.key, l.val); problem != "" {
			add(ruleRecordValue, recordKeyLabel(l)+": "+problem)
		}
	}
	if rec.Values["tag"] != wantTag {
		add(ruleRecordInvariant, "tag: the record names another tag than its file name")
	}
	if status == statusVerified {
		found = append(found, verifiedInvariants(path, rec.Values, assets)...)
	}
	return rec, found
}

// recordKeyLabel names a line by its key, or by its line number when the key
// holds a file-chosen asset name.
func recordKeyLabel(l recordLine) string {
	if strings.HasPrefix(l.key, assetPrefix) {
		return "line " + strconv.Itoa(l.n)
	}
	return l.key
}

// recordValueProblem judges the shape of one value; the empty string is none.
func recordValueProblem(key, val string) string {
	switch {
	case key == "schema":
		return want(val == recordSchemaVersion, "must be "+recordSchemaVersion)
	case key == "tag":
		return want(checkTagPattern(val) == nil, "must be a Release Tag")
	case key == "status":
		return want(val == statusVerified || val == statusAbandoned, "must be verified or abandoned")
	case key == "time":
		return want(isUTCInstant(val), "must be a UTC instant, YYYY-MM-DDThh:mm:ssZ")
	case key == "reason":
		return want(slices.Contains(recordReasons, val), "must be one of the listed reasons")
	case key == "commit", key == "goreleaser-config-blob", key == "devbox-lock-blob", key == "attested.gate-commit":
		return want(blobHexPattern.MatchString(val), "must be 40 lowercase hex characters")
	case strings.HasPrefix(key, assetPrefix):
		return want(sha256Pattern.MatchString(val), "must be 64 lowercase hex characters")
	case key == "exceptions":
		return exceptionsShape(val)
	case key == "host-platform", key == "attested.snapshot-version", key == "attested.gates", key == "attested.lint-diff", key == "run-conclusion", key == "proton-version":
		return want(val != "", "must not be empty")
	case key == "go-version", key == "goreleaser-version", key == "attested.go-version", key == "attested.goreleaser-version":
		return want(dottedPattern.MatchString(val), "must be a dotted number")
	case key == "run-id", key == "run-attempt":
		return want(countPattern.MatchString(val), "must be a positive integer")
	}
	return "unexpected key"
}

func want(ok bool, problem string) string {
	if ok {
		return ""
	}
	return problem
}

// isUTCInstant accepts exactly YYYY-MM-DDThh:mm:ssZ and a real instant.
func isUTCInstant(s string) bool {
	if !timePattern.MatchString(s) {
		return false
	}
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

// exceptionsShape judges the list without the assets: names separated by
// commas, none empty, strictly sorted.
func exceptionsShape(val string) string {
	if val == "" {
		return ""
	}
	names := strings.Split(val, ",")
	for i, n := range names {
		if !assetNamePattern.MatchString(n) {
			return "must be a comma-separated list of asset names"
		}
		if i > 0 && n <= names[i-1] {
			return "must be sorted and without repeats"
		}
	}
	return ""
}

// verifiedInvariants are the stated invariants of a verified record that tie
// several keys together.
func verifiedInvariants(path string, v map[string]string, assets []string) []Finding {
	var fs []Finding
	add := func(detail string) { fs = append(fs, Finding{Path: path, Rule: ruleRecordInvariant, Detail: detail}) }
	if v["attested.gate-commit"] != v["commit"] {
		add("attested.gate-commit: must equal commit")
	}
	if v["run-conclusion"] != runConclusionOK {
		add("run-conclusion: must be " + runConclusionOK)
	}
	if v["attested.gates"] != attestedGates {
		add("attested.gates: must be exactly " + attestedGates)
	}
	if v["attested.lint-diff"] != attestedLintDiff {
		add("attested.lint-diff: must be exactly " + attestedLintDiff)
	}
	if v["proton-version"] != strings.TrimPrefix(v["tag"], "v") {
		add("proton-version: must equal tag without its v")
	}
	if v["exceptions"] != "" {
		for _, n := range strings.Split(v["exceptions"], ",") {
			if !slices.Contains(assets, n) {
				add("exceptions: names an asset that is not listed")
				break
			}
		}
	}
	return fs
}

// validateVerificationDir checks the Verification Record surface in a file
// system rooted at the repository root: the directory and its README.md exist
// as a directory and a regular file, and every other entry is a regular file
// named by the Release Tag pattern plus .md that holds one valid record naming
// that tag. A symlink, a directory or a submodule is never followed. The
// findings are sorted and name a path, a rule and a key or line, never content.
func validateVerificationDir(fsys fs.FS) []Finding {
	var out []Finding
	add := func(path, rule, detail string) { out = append(out, Finding{Path: path, Rule: rule, Detail: detail}) }

	for _, dir := range []string{"fork", verificationDir} {
		info, err := fs.Lstat(fsys, dir)
		if err != nil || !info.IsDir() {
			add(verificationDir, ruleRecordSurface, "the directory does not exist as a directory")
			return sortFindings(out)
		}
	}
	entries, err := fs.ReadDir(fsys, verificationDir)
	if err != nil {
		add(verificationDir, ruleRecordSurface, "the directory cannot be listed")
		return sortFindings(out)
	}
	sawReadme := false
	for _, e := range entries {
		name := e.Name()
		path := verificationDir + "/" + name
		regular := e.Type().IsRegular()
		if name == "README.md" {
			sawReadme = true
			if !regular {
				add(path, ruleRecordSurface, "README.md is not a regular file")
			}
			continue
		}
		if !regular {
			add(path, ruleRecordKind, "the entry is not a regular file")
			continue
		}
		tag, ok := strings.CutSuffix(name, ".md")
		if !ok || checkTagPattern(tag) != nil {
			add(path, ruleRecordName, "the name is not a Release Tag plus .md")
			continue
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			add(path, ruleRecordKind, "the file cannot be read")
			continue
		}
		if len(data) > maxRecordSize {
			add(path, ruleRecordSize, "the file is larger than a record can be")
			continue
		}
		_, found := parseRecord(path, tag, data)
		out = append(out, found...)
	}
	if !sawReadme {
		add(verificationDir+"/README.md", ruleRecordSurface, "README.md is missing")
	}
	return sortFindings(out)
}
