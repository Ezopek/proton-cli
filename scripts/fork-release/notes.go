package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The fork release notes are one fixed template rendered into the runner's
// temporary directory, and passed to GoReleaser as its release notes. They name
// the Upstream Base, warn against `proton update`, and carry the two lines the
// later checks read back: the commit the release was built from and the run
// that built it. Writer and parsers share one prefix constant per line.

const (
	// runLinePrefix starts the one line of a release's notes that names the run
	// that built the release, as <run_id>/<run_attempt>. commitLinePrefix, its
	// sibling, lives with the release lookup.
	runLinePrefix = "Run: "

	// notesFileName is the one file the notes command creates, always inside
	// RUNNER_TEMP. Story 1.9's workflow passes it to GoReleaser.
	notesFileName = "fork-release-notes.md"
)

// positiveInt is a decimal whole number of at least one, without a leading zero
// or sign. Digits are ASCII only.
var positiveInt = regexp.MustCompile(`^[1-9][0-9]*$`)

// lineValue finds the lines of a notes body that start with prefix. Lines end at
// a line feed and one carriage return before it is tolerated. It answers how
// many lines start with the prefix and, for the last of them, the rest of the
// line.
func lineValue(body, prefix string) (value string, count int) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSuffix(line, "\r")
		rest, ok := strings.CutPrefix(line, prefix)
		if !ok {
			continue
		}
		count++
		value = rest
	}
	return value, count
}

// parseRunLine finds the one Run line of a notes body. It answers the run id and
// the attempt only when exactly one line starts with the prefix and the rest of
// it is two positive decimal numbers without leading zeros, joined by a slash; a
// repeated or malformed line answers false.
func parseRunLine(body string) (runID, attempt string, ok bool) {
	value, count := lineValue(body, runLinePrefix)
	if count != 1 {
		return "", "", false
	}
	runID, attempt, found := strings.Cut(value, "/")
	if !found || !positiveInt.MatchString(runID) || !positiveInt.MatchString(attempt) {
		return "", "", false
	}
	return runID, attempt, true
}

// renderNotes is the fixed fork template for the Release Tag tag, built on the
// Upstream Base base from the commit e, by the run runID attempt. No line of the
// template other than the two at its end starts with either line prefix.
func renderNotes(tag, base, e, runID, attempt string) string {
	version := strings.TrimPrefix(tag, "v")
	var b strings.Builder
	fmt.Fprintf(&b, "Fork release %s of proton-cli.\n\n", tag)
	fmt.Fprintf(&b, "This is the Ezopek fork's build of the Upstream release %s plus the fork's own commits. ", base)
	fmt.Fprintf(&b, "It is not an Upstream release, and `proton version` reports %s.\n\n", version)
	b.WriteString("Do not run `proton update` (or `proton-cli update`) on a fork binary. ")
	b.WriteString("It resolves Upstream's latest release and would replace this fork build with an Upstream one. ")
	b.WriteString("Download the next fork release from this page instead.\n\n")
	b.WriteString(commitLinePrefix + e + "\n")
	b.WriteString(runLinePrefix + runID + "/" + attempt + "\n")
	return b.String()
}

// runnerTempDir reads RUNNER_TEMP and requires an absolute path of an existing
// directory.
func runnerTempDir(d guardDeps, check string) (string, error) {
	dir := getenvOf(d, "RUNNER_TEMP")
	if dir == "" || !filepath.IsAbs(dir) {
		return "", refusal{check, "RUNNER_TEMP is not set to an absolute path"}
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", refusal{check, "RUNNER_TEMP is not an existing directory"}
	}
	return dir, nil
}

// runNotes is notes --in-run. It reads GITHUB_REF_NAME, GITHUB_SHA,
// GITHUB_RUN_ID, GITHUB_RUN_ATTEMPT and RUNNER_TEMP, names the Upstream Base
// from the tags of origin for E = GITHUB_SHA exactly as the guard does, requires
// the tag's X.Y.Z to equal it, and creates the one new file fork-release-notes.md
// in RUNNER_TEMP. It never overwrites a file and makes no network write.
func runNotes(d guardDeps) int {
	tag, e := getenvOf(d, "GITHUB_REF_NAME"), getenvOf(d, "GITHUB_SHA")
	if err := checkTagPattern(tag); err != nil {
		return refuseWith(d, refusal{"notes", "GITHUB_REF_NAME is not a Release Tag"})
	}
	if !fullSHA.MatchString(e) {
		return refuseWith(d, refusal{"notes", "GITHUB_SHA is not a full lowercase commit SHA"})
	}
	runID, attempt := getenvOf(d, "GITHUB_RUN_ID"), getenvOf(d, "GITHUB_RUN_ATTEMPT")
	if !positiveInt.MatchString(runID) {
		return refuseWith(d, refusal{"notes", "GITHUB_RUN_ID is not a positive number without a leading zero"})
	}
	if !positiveInt.MatchString(attempt) {
		return refuseWith(d, refusal{"notes", "GITHUB_RUN_ATTEMPT is not a positive number without a leading zero"})
	}
	dir, err := runnerTempDir(d, "notes")
	if err != nil {
		return refuseWith(d, err)
	}

	tags, err := d.git.OriginTags()
	if err != nil {
		return refuseWith(d, refusal{"identity", "could not list the tags of origin"})
	}
	base, err := upstreamBase(d.git, e, tags)
	if err != nil {
		return refuseWith(d, err)
	}
	if err := checkBaseMatch(tag, base); err != nil {
		return refuseWith(d, err)
	}

	body := renderNotes(tag, base, e, runID, attempt)
	// The template is checked through the parsers that will read it back.
	if sha, ok := parseCommitLine(body); !ok || sha != e {
		return refuseWith(d, refusal{"notes", "the rendered notes do not carry exactly one Commit line"})
	}
	if id, att, ok := parseRunLine(body); !ok || id != runID || att != attempt {
		return refuseWith(d, refusal{"notes", "the rendered notes do not carry exactly one Run line"})
	}

	path := filepath.Join(dir, notesFileName)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return refuseWith(d, refusal{"notes", "could not create " + notesFileName + " in RUNNER_TEMP, which must not exist yet"})
	}
	_, werr := f.WriteString(body)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
		return refuseWith(d, refusal{"notes", "could not write " + notesFileName})
	}
	if _, err := fmt.Fprintf(d.stdout, "fork-release: notes: ok: wrote %s for %s at %s\n", notesFileName, tag, e); err != nil {
		return refuseWith(d, refusal{"output", "could not write the result"})
	}
	return exitPass
}
