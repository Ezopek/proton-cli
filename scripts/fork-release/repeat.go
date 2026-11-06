package main

import (
	"fmt"
	"strings"
)

// repeatDecision is the refusal of a repeat run (AD-7, AMD-FR18-1): a pure
// decision over the record state and the release state. It proceeds only for a
// record that is Absent and a release list that holds no release for the tag:
// a first run, or a retry of a tag whose earlier attempt failed before any
// release object existed. Everything else refuses, a record of any content
// (presence only), something at the record path that is not a file, a release
// of any kind and any state that could not be read. Once a release object
// exists the version is abandoned and the next N is used.
//
// The reason comes from a closed vocabulary. It never quotes an answer, and
// the record is named before the release when both are the cause.
func repeatDecision(rec recordState, rel releaseState) error {
	switch rec {
	case recordAbsent:
	case recordPresent:
		return refusal{"repeat", "a verification record exists for the tag"}
	case recordNotFile:
		return refusal{"repeat", "something that is not a record file exists at the record path of the tag"}
	default:
		return refusal{"repeat", "the verification record state of the tag could not be read"}
	}
	switch rel {
	case releaseNone:
		return nil
	case releasePublishedSame:
		return refusal{"repeat", "a published release for the tag already exists"}
	case releaseDraft:
		return refusal{"repeat", "a draft release for the tag already exists"}
	case releaseOther:
		return refusal{"repeat", "a release for the tag already exists and does not name this commit"}
	}
	return refusal{"repeat", "the release state of the tag could not be read"}
}

// findingRefusals is the refusal of the release profile: one line
// "fork-release: profile: <finding>" per held item, in the profile's own order.
type findingRefusals []string

func (r findingRefusals) Error() string { return strings.Join(r, "\n") }

// profileStep is the release profile on the tree of E, the step the in-run
// guard puts between the base check and the toolchain check.
func profileStep(g gitRepo) func(e string) error {
	return func(e string) error {
		view, err := commitTreeFS(g, e, profilePaths("release"))
		if err != nil {
			return refusal{"profile", "could not read the tree of the commit"}
		}
		findings := CheckRelease(view)
		if len(findings) == 0 {
			return nil
		}
		lines := make(findingRefusals, 0, len(findings))
		for _, f := range findings {
			// One line per finding, whatever the finding's text holds.
			lines = append(lines, refusal{"profile", strings.Join(strings.Fields(f.String()), " ")}.Error())
		}
		return lines
	}
}

// repeatCheck reads the record state and the release state of the tag and
// applies the refusal decision. It is the last in-run guard and, read again, the
// check immediately before GoReleaser. Without a GitHub reader nothing could be
// read, and both states are Unknown.
func repeatCheck(d guardDeps, tag, e string) error {
	if d.gh == nil {
		return repeatDecision(recordUnknown, releaseUnknown)
	}
	rec := lookupRecord(d.gh, tag)
	rel := lookupRelease(d.gh, tag, e)
	return repeatDecision(rec.State, rel.State)
}

// getenvOf is the environment reader of the run, empty when there is none.
func getenvOf(d guardDeps, name string) string {
	if d.getenv == nil {
		return ""
	}
	return d.getenv(name)
}

// guardOK prints the one line of a passing in-run mode.
func guardOK(d guardDeps, what, tag, e string) int {
	if _, err := fmt.Fprintf(d.stdout, "fork-release: guard: ok: %s %s at %s\n", what, tag, e); err != nil {
		return refuseWith(d, refusal{"output", "could not write the result"})
	}
	return exitPass
}

// refuseWith prints a refusal on standard error and answers the held code.
func refuseWith(d guardDeps, err error) int {
	_, _ = fmt.Fprintln(d.stderr, err)
	return exitHeld
}

// runInRun is guard --in-run, the first thing the release job runs. It reads
// GITHUB_REF_NAME (the tag), GITHUB_SHA and GORELEASER_PIN, and checks in the
// fixed order of AD-6, stopping at the first refusal and reading nothing after
// it: the tag pattern; the identity of HEAD, GITHUB_SHA and the tag on origin;
// E reachable from the origin/ezop it fetches once; the Upstream Base; the
// release profile on the tree of E; the toolchain; the refusal of a repeat. It
// makes no write call, creates and pushes no tag, and does not repeat the
// Operator-side origin and build-identity checks.
func runInRun(d guardDeps) int {
	tag := getenvOf(d, "GITHUB_REF_NAME")
	e, err := checkTagBindingWith(d.git, tag, getenvOf(d, "GITHUB_SHA"), getenvOf(d, "GORELEASER_PIN"), profileStep(d.git))
	if err != nil {
		return refuseWith(d, err)
	}
	if err := repeatCheck(d, tag, e); err != nil {
		return refuseWith(d, err)
	}
	return guardOK(d, "in-run", tag, e)
}

// runRecheck is guard --recheck, run immediately before GoReleaser. It reads
// GITHUB_REF_NAME, takes E from HEAD (the first run proved the identity), reads
// the record state and the release state again and applies the same decision.
// It reads no tree and no tag.
func runRecheck(d guardDeps) int {
	tag := getenvOf(d, "GITHUB_REF_NAME")
	if err := checkTagPattern(tag); err != nil {
		return refuseWith(d, err)
	}
	e, err := d.git.ResolveCommit("HEAD")
	if err != nil || !fullSHA.MatchString(e) {
		return refuseWith(d, refusal{"identity", "HEAD does not name a commit"})
	}
	if err := repeatCheck(d, tag, e); err != nil {
		return refuseWith(d, err)
	}
	return guardOK(d, "recheck", tag, e)
}
