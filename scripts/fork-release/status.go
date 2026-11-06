package main

import (
	"fmt"
	"strings"
)

// verify --status TAG is the one refusal and Handover interface (AD-7). It is
// read-only and makes at most one record lookup, one release lookup, one read of
// TAG on origin and one listing of runs; it downloads and rereads no asset. It
// prints
//
//	record: <token>
//	release: <state>
//
// and gives the first matching row of the nine-row status table. The table is
// written once, in decideStatus, and every caller uses it by name.

// verifyUsage is the one-line message for a malformed verify command line.
const verifyUsage = "fork-release: usage: fork-release verify --status TAG | verify TAG GATES"

// statusToken is the first word of the answer, the stable interface for the
// Handover consumer.
type statusToken string

const (
	tokenVerified  statusToken = "verified"
	tokenNone      statusToken = "none"
	tokenUnknown   statusToken = "unknown"
	tokenAbandoned statusToken = "abandoned"
	tokenInvalid   statusToken = "invalid"
)

// Exit codes of the status tokens. Zero is only for verified; 1 and 2 stay the
// refusal and the usage codes (Go's defaults for a crash and a usage error).
const (
	exitStatusNone      = 3
	exitStatusUnknown   = 4
	exitStatusAbandoned = 5
	exitStatusInvalid   = 6
)

// exitCode is the exit code of a token. A token nobody knows exits as unknown,
// never as a pass.
func (t statusToken) exitCode() int {
	switch t {
	case tokenVerified:
		return exitPass
	case tokenNone:
		return exitStatusNone
	case tokenAbandoned:
		return exitStatusAbandoned
	case tokenInvalid:
		return exitStatusInvalid
	}
	return exitStatusUnknown
}

// releaseNotRead is printed beside a token that the record alone decided.
const releaseNotRead = "not read"

// Outcomes of the read of TAG on origin.
const (
	// tagFailed is a read that did not complete, or an answer that cannot be used.
	// It is the zero value, so a read nobody made is the cautious one.
	tagFailed = iota
	// tagAbsent is a completed read in which origin has no such tag.
	tagAbsent
	// tagPresent is a completed read with the commit the tag peels to.
	tagPresent
)

// tagRead is the one read of TAG on origin.
type tagRead struct {
	State int
	Peel  string
}

// readTag reads the tags of origin once and answers the tag's peeled commit,
// its absence, or a failure. Success without the tag is absence; a failed read
// is never absence.
func readTag(g gitRepo, tag string) tagRead {
	tags, err := g.OriginTags()
	if err != nil {
		return tagRead{}
	}
	peel, ok := tags[tag]
	switch {
	case !ok:
		return tagRead{State: tagAbsent}
	case !fullSHA.MatchString(peel):
		return tagRead{}
	}
	return tagRead{State: tagPresent, Peel: peel}
}

// statusInputs is everything the table looks at, already read and judged.
type statusInputs struct {
	// Record is the state of the record lookup.
	Record recordState
	// Valid is true for a Present record that the Story 1.4 parser accepts for
	// the tag; Verified tells a verified one from an abandoned one.
	Valid    bool
	Verified bool
	// RecordCommit and RecordRun are the commit and the <run_id>/<run_attempt> of
	// a valid verified record.
	RecordCommit string
	RecordRun    string
	// Release is the release lookup, Tag the read of TAG and Listing the run
	// listing, which counts only when ListingRead says it was made.
	Release     releaseLookup
	Tag         tagRead
	Listing     runListing
	ListingRead bool
}

// listingSettled is a listing that was made, read to its end and has no run that
// is not over.
func (in statusInputs) listingSettled() bool {
	return in.ListingRead && in.Listing.Reason == "" && in.Listing.AllTerminal()
}

// decideStatus is the status table: the token is the first row that matches, and
// every combination of reads matches exactly one row. A listing that was needed
// and not made is a listing that failed.
func decideStatus(in statusInputs) statusToken {
	switch in.Record {
	case recordPresent:
	case recordNotFile:
		return tokenInvalid // row 2
	case recordAbsent:
		// Rows 3 and 4. With no commit to list, the listing is not needed: a tag
		// that origin positively does not have reads none, a failed read unknown.
		switch {
		case in.Tag.State == tagFailed:
			return tokenUnknown
		case in.Tag.State == tagPresent && !in.listingSettled():
			return tokenUnknown
		}
		return tokenNone
	default:
		return tokenUnknown // row 1
	}

	switch {
	case !in.Valid:
		return tokenInvalid // row 5
	case !in.Verified:
		return tokenAbandoned // row 6, from the record alone
	}

	// Row 7: a read that did not complete, or a run that is not over.
	if in.Release.State == releaseUnknown || in.Tag.State == tagFailed || !in.listingSettled() {
		return tokenUnknown
	}
	// Row 8: every read completed and one of them disagrees with the record.
	if in.Release.State != releasePublishedSame || !in.Release.RunValid || in.Release.RunValue != in.RecordRun ||
		in.Tag.State != tagPresent || in.Tag.Peel != in.RecordCommit {
		return tokenInvalid
	}
	return tokenVerified // row 9
}

// preRerunDecision is the Operator's rule before a re-run of a tag's workflow:
// the token must be none together with release None. none beside a Published-Same
// release or a Draft means abandon, not re-run, and the pair none with an Unknown
// release is refused too. Nothing calls it yet; the status command only reports.
func preRerunDecision(token statusToken, release releaseState) error {
	switch {
	case token != tokenNone:
		return refusal{"pre-rerun", "the status token of the tag is not none"}
	case release != releaseNone:
		return refusal{"pre-rerun", "the release of the tag is not None"}
	}
	return nil
}

// zeroSHA stands in for the commit when the tag has no peel: a published release
// can then only read Other, never Same.
var zeroSHA = strings.Repeat("0", 40)

// judgeRecord judges the bytes of a Present record for the tag with the Story 1.4
// parser. It answers whether the record is valid, whether it is verified, and its
// commit and run for a valid verified one.
func judgeRecord(tag string, data []byte) (valid, verified bool, commit, run string) {
	if len(data) > maxRecordSize {
		return false, false, "", ""
	}
	rec, findings := parseRecord(verificationDir+"/"+tag+".md", tag, data)
	if len(findings) > 0 {
		return false, false, "", ""
	}
	if rec.Status != statusVerified {
		return true, false, "", ""
	}
	return true, true, rec.Values["commit"], rec.Values["run-id"] + "/" + rec.Values["run-attempt"]
}

// runVerify dispatches verify. Exactly `verify --status TAG` and `verify TAG
// GATES` are accepted; anything else is one usage line, with no read of anything.
// A word that starts with a dash is an option, never a tag or a path, so a
// mistyped option is usage and not a refusal.
func runVerify(args []string, d guardDeps) int {
	switch {
	case len(args) == 2 && args[0] == "--status":
		return runStatus(args[1], d)
	case len(args) == 2 && !strings.HasPrefix(args[0], "-") && !strings.HasPrefix(args[1], "-"):
		return runVerifyTag(args[0], args[1], d)
	}
	_, _ = fmt.Fprintln(d.stderr, verifyUsage)
	return exitUsage
}

// runStatus is verify --status TAG. It checks, in order: the tag pattern, the
// helper identity, then the reads. The record is read first and, when it alone
// decides (rows 1, 2, 5 and 6), nothing else is. Otherwise TAG is read on origin
// (its peel is the commit of an Absent record), the release is looked up for that
// commit and the runs of the workflow are listed for it. A verified record names
// its own commit.
func runStatus(tag string, d guardDeps) int {
	if checkTagPattern(tag) != nil {
		return refuseWith(d, refusal{"status", "TAG is not a Release Tag"})
	}
	if _, _, err := helperIdentity(d); err != nil {
		return refuseWith(d, err)
	}
	if d.gh == nil {
		return refuseWith(d, refusal{"status", "there is no GitHub reader, so nothing can be read"})
	}

	rec := lookupRecord(d.gh, tag)
	in := statusInputs{Record: rec.State}
	release := releaseNotRead
	needReads := rec.State == recordAbsent
	if rec.State == recordPresent {
		in.Valid, in.Verified, in.RecordCommit, in.RecordRun = judgeRecord(tag, rec.Data)
		needReads = in.Valid && in.Verified
	}
	if needReads {
		in.Tag = readTag(d.git, tag)
		e := zeroSHA
		switch {
		case in.Verified:
			e = in.RecordCommit
		case in.Tag.State == tagPresent:
			e = in.Tag.Peel
		}
		in.Release = lookupRelease(d.gh, tag, e)
		release = in.Release.State.String()
		if in.Verified || in.Tag.State == tagPresent {
			in.Listing, in.ListingRead = listRuns(d.gh, e), true
		}
	}

	token := decideStatus(in)
	if _, err := fmt.Fprintf(d.stdout, "record: %s\nrelease: %s\n", token, release); err != nil {
		return refuseWith(d, refusal{"output", "could not write the result"})
	}
	return token.exitCode()
}
