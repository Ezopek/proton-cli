package main

import (
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
)

// buildRecipe is the documented way to build this helper, run from the root of
// a clean clone of the fork. The output goes outside the clone: a file left
// inside it is an untracked file, which makes the stamp read "modified". It
// has to be go build, because go run and go test are not relied on to stamp the
// build information that the Operator-side commands read.
const buildRecipe = "go build -o <outside the clone>/fork-release ./scripts/fork-release"

// buildRecipeArgs is the argument list of buildRecipe, with out as the output.
func buildRecipeArgs(out string) []string {
	return []string{"build", "-o", out, "./scripts/fork-release"}
}

// canonicalOrigins are the only URL forms the origin remote may have, on every
// effective fetch and push URL. They are case-sensitive.
var canonicalOrigins = []string{
	"git@github.com:Ezopek/proton-cli.git",
	"https://github.com/Ezopek/proton-cli.git",
	"ssh://git@github.com/Ezopek/proton-cli.git",
}

// refusal is why the helper declined to answer. It reads as one line,
// "fork-release: <check>: <reason>", and its reason never carries a URL, git
// output or any other raw diagnostic.
type refusal struct {
	check  string
	reason string
}

func (r refusal) Error() string {
	return "fork-release: " + r.check + ": " + r.reason
}

// originURLsOK decides whether every effective fetch and push URL of origin is
// one of the allowed forms. Both lists must be non-empty, so a missing origin
// is refused, and one bad URL among good ones refuses. The reason names the
// direction and never the URL.
func originURLsOK(fetch, push, allowed []string) error {
	for _, side := range []struct {
		name string
		urls []string
	}{{"fetch", fetch}, {"push", push}} {
		if len(side.urls) == 0 {
			return refusal{"origin", "origin has no " + side.name + " URL"}
		}
		for _, u := range side.urls {
			if !slices.Contains(allowed, u) {
				return refusal{"origin", "a " + side.name + " URL of origin is not the canonical Ezopek/proton-cli remote"}
			}
		}
	}
	return nil
}

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// buildIdentity reads the commit a helper was built from out of its embedded
// build information. It refuses a binary with no build information, without a
// revision or a modified flag, whose revision is not a full lowercase hex
// SHA-1, or that was built from a modified tree.
func buildIdentity(info *debug.BuildInfo) (string, error) {
	if info == nil {
		return "", refusal{"build", "the binary has no embedded build information; build it with the documented recipe"}
	}
	var revision, modified string
	var haveRevision, haveModified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision, haveRevision = s.Value, true
		case "vcs.modified":
			modified, haveModified = s.Value, true
		}
	}
	switch {
	case !haveRevision:
		return "", refusal{"build", "the binary has no vcs.revision; build it with the documented recipe in a clean clone"}
	case !haveModified:
		return "", refusal{"build", "the binary has no vcs.modified; build it with the documented recipe in a clean clone"}
	case !fullSHA.MatchString(revision):
		return "", refusal{"build", "vcs.revision is not a full 40-character lowercase hex commit"}
	case modified == "true":
		return "", refusal{"build", "the binary was built from a modified tree; build it in a clean clone"}
	case modified != "false":
		return "", refusal{"build", "vcs.modified is neither true nor false"}
	}
	return revision, nil
}

// ancestryDecision maps the exit status of git merge-base --is-ancestor
// (helper commit, fetched origin/ezop) to a decision. Only status 0 is landed.
// Status 1 is a definite no. Any other status means git could not decide, which
// is told apart from "not landed" because the advice differs.
func ancestryDecision(status int) error {
	switch status {
	case 0:
		return nil
	case 1:
		return refusal{"ancestry", "this helper's commit is not landed: it is neither origin/ezop nor an ancestor of it"}
	}
	return refusal{"ancestry", "cannot decide whether this helper's commit is landed (git exit status " + strconv.Itoa(status) + ")"}
}
