package main

import (
	"strconv"
	"strings"
)

// The release lookup answers one question: what does the repository's list of
// releases say about one Release Tag? It is the one function guard, assets,
// stamp, verify and verify --status share. It reads the list through the same
// pinned gh adapter as the record lookup, a page at a time, and decides nothing:
// the caller turns the state into a refusal or a status.
//
// The list has no total to reconcile against. It ends at the first empty page,
// a page ceiling guarantees that the walk ends, and every doubt is Unknown, so
// a partial list never reads as None.

// commitLinePrefix starts the one line of a release's notes that names the
// commit the release was built from. The notes writer shares this constant, so
// what the lookup reads is what the notes say.
const commitLinePrefix = "Commit: "

const (
	// releasePerPage is the page size asked for: the documented maximum.
	releasePerPage = 100
	// releasePageCeiling is the most pages read. A list still going after the
	// last of them is Unknown.
	releasePageCeiling = 50
)

// releaseState is the outcome of the lookup. The zero value is Unknown, so a
// result nobody filled in is the cautious one.
type releaseState int

const (
	// releaseUnknown is every doubt: a failed call, a missing or malformed page,
	// a malformed entry, two entries with the tag, a list that does not end.
	releaseUnknown releaseState = iota
	// releaseNone is a complete list in which no release has the tag.
	releaseNone
	// releasePublishedSame is exactly one release with the tag, not a draft,
	// whose notes hold exactly one Commit line and it equals the commit asked.
	releasePublishedSame
	// releaseDraft is exactly one release with the tag and it is a draft.
	releaseDraft
	// releaseOther is exactly one release with the tag that is neither of the
	// above, such as a published release whose notes have no, a repeated, a
	// malformed or a different Commit line.
	releaseOther
)

func (s releaseState) String() string {
	switch s {
	case releaseNone:
		return "None"
	case releasePublishedSame:
		return "Published-Same"
	case releaseDraft:
		return "Draft"
	case releaseOther:
		return "Other"
	}
	return "Unknown"
}

// releaseLookup is the answer: the state and, for Unknown, a reason built from
// a closed set of words. The reason never holds a response, a URL or a name
// from the answer.
type releaseLookup struct {
	State  releaseState
	Reason string
}

// Reasons of an Unknown release lookup, beside the shared causes.
const (
	causeNoTagName  = "no tag_name"
	causeNoDraft    = "no draft flag"
	causeCeiling    = "page ceiling"
	causeNotTag     = "not a Release Tag"
	causeNotFullSHA = "not a full commit SHA"
)

// parseCommitLine finds the one Commit line of a notes body. Lines end at a
// line feed and one carriage return before it is tolerated. It answers the
// commit only when exactly one line starts with the prefix and the rest of it is
// 40 lowercase hex characters; a repeated or malformed line answers false.
func parseCommitLine(body string) (string, bool) {
	var sha string
	found := 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSuffix(line, "\r")
		value, ok := strings.CutPrefix(line, commitLinePrefix)
		if !ok {
			continue
		}
		found++
		sha = value
	}
	if found != 1 || !fullSHA.MatchString(sha) {
		return "", false
	}
	return sha, true
}

// lookupRelease lists the releases of the repository, to the end, and reports
// what the list holds for tag. e is the commit the release must name to be
// Published-Same. Nothing is asked for a tag that is not a Release Tag or a
// commit that is not a full SHA.
func lookupRelease(g ghGetter, tag, e string) releaseLookup {
	unknown := func(reason string) releaseLookup { return releaseLookup{Reason: reason} }
	if checkTagPattern(tag) != nil {
		return unknown("tag: " + causeNotTag)
	}
	if !fullSHA.MatchString(e) {
		return unknown("commit: " + causeNotFullSHA)
	}

	var matches []map[string]any
	for page := 1; page <= releasePageCeiling; page++ {
		step := "releases page " + strconv.Itoa(page) + ": "
		var raw any
		endpoint := "releases?per_page=" + strconv.Itoa(releasePerPage) + "&page=" + strconv.Itoa(page)
		if cause := getJSON(g, endpoint, &raw); cause != "" {
			return unknown(step + cause)
		}
		entries, ok := raw.([]any)
		if !ok {
			return unknown(step + causeMalformed)
		}
		if len(entries) == 0 {
			return classifyRelease(matches, e)
		}
		for _, item := range entries {
			entry, ok := item.(map[string]any)
			if !ok {
				return unknown(step + causeMalformed)
			}
			name, ok := entry["tag_name"].(string)
			if !ok {
				return unknown(step + causeNoTagName)
			}
			if _, ok := entry["draft"].(bool); !ok {
				return unknown(step + causeNoDraft)
			}
			if name != tag {
				continue
			}
			if len(matches) > 0 {
				return unknown(step + causeRepeated)
			}
			matches = append(matches, entry)
		}
	}
	return unknown(causeCeiling)
}

// classifyRelease turns the complete list's entries with the tag into a state.
func classifyRelease(matches []map[string]any, e string) releaseLookup {
	if len(matches) == 0 {
		return releaseLookup{State: releaseNone}
	}
	entry := matches[0]
	if draft, _ := entry["draft"].(bool); draft {
		return releaseLookup{State: releaseDraft}
	}
	body, _ := entry["body"].(string)
	if sha, ok := parseCommitLine(body); ok && sha == e {
		return releaseLookup{State: releasePublishedSame}
	}
	return releaseLookup{State: releaseOther}
}
