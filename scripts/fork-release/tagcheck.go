package main

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// releaseTagPattern is the only shape a Release Tag may have, matched against
// the whole string. Go's $ matches only at the end of the text, so a trailing
// newline does not match.
var releaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-ezop\.[1-9][0-9]*$`)

// checkTagPattern accepts only a Release Tag name.
func checkTagPattern(tag string) error {
	if !releaseTagPattern.MatchString(tag) {
		return refusal{"tag", "the tag name is not a Release Tag (vX.Y.Z-ezop.N with N a positive number without a leading zero)"}
	}
	return nil
}

// checkIdentity decides whether HEAD, GITHUB_SHA (already peeled to a commit)
// and the tag as listed by origin (peeled) are one and the same commit, and
// returns it as E. An empty input is a missing one. The reason names the input
// at fault and never a URL.
func checkIdentity(head, githubSHA, originTag string) (string, error) {
	for _, in := range []struct{ name, sha string }{
		{"HEAD", head},
		{"GITHUB_SHA", githubSHA},
		{"the tag on origin", originTag},
	} {
		if in.sha == "" {
			return "", refusal{"identity", in.name + " is missing"}
		}
		if !fullSHA.MatchString(in.sha) {
			return "", refusal{"identity", in.name + " is not a full lowercase commit SHA"}
		}
	}
	if githubSHA != head {
		return "", refusal{"identity", "GITHUB_SHA differs from HEAD"}
	}
	if originTag != head {
		return "", refusal{"identity", "the tag on origin differs from HEAD"}
	}
	return head, nil
}

// reachableDecision maps the exit status of git merge-base --is-ancestor
// (E, fetched origin/ezop) to a decision. Only status 0 passes. Status 1 is a
// definite no; any other status means git could not decide, which is told apart
// because the advice differs.
func reachableDecision(status int) error {
	switch status {
	case 0:
		return nil
	case 1:
		return refusal{"reachable", "the commit is not reachable from origin/ezop: it is neither the tip nor an ancestor of it"}
	}
	return refusal{"reachable", "cannot decide whether the commit is reachable from origin/ezop (git exit status " + strconv.Itoa(status) + ")"}
}

// checkReachable fetches origin/ezop once and requires E to be equal to its
// tip or an ancestor of it. A failed fetch is a refusal of its own.
func checkReachable(g gitRepo, e string) error {
	tip, err := g.FetchEzop()
	if err != nil || !fullSHA.MatchString(tip) {
		return refusal{"reachable", "could not fetch origin ezop, so reachability is undecided"}
	}
	status, err := g.IsAncestor(e, tip)
	if err != nil {
		status = -1
	}
	return reachableDecision(status)
}

// upstreamTagPattern is the shape of an Upstream release tag, the only kind of
// name that can be an Upstream Base. Fork tags and pre-releases do not match.
var upstreamTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// triple is the numeric (X, Y, Z) of an Upstream release tag name, kept as
// digit strings so that no number can overflow.
func triple(name string) [3]string {
	var t [3]string
	copy(t[:], strings.Split(strings.TrimPrefix(name, "v"), "."))
	return t
}

// compareTriples orders two triples numerically: leading zeros are dropped,
// then a longer digit string is greater, then the text decides.
func compareTriples(a, b [3]string) int {
	for i := range a {
		x, y := strings.TrimLeft(a[i], "0"), strings.TrimLeft(b[i], "0")
		if len(x) != len(y) {
			return cmp.Compare(len(x), len(y))
		}
		if c := strings.Compare(x, y); c != 0 {
			return c
		}
	}
	return 0
}

// upstreamBase computes the Upstream Base of the commit e from the tags of
// origin: the tag with the highest numeric triple among those whose name is an
// Upstream release and whose commit is e or an ancestor of it. Order never
// depends on dates, topology or names. No candidate, two names with one triple,
// or a candidate whose ancestry cannot be decided is a refusal.
func upstreamBase(g gitRepo, e string, tags map[string]string) (string, error) {
	if !fullSHA.MatchString(e) {
		return "", refusal{"base", "the commit is not a full lowercase commit SHA"}
	}
	names := make([]string, 0, len(tags))
	for name := range tags {
		if upstreamTagPattern.MatchString(name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var candidates []string
	for _, name := range names {
		status, err := g.IsAncestor(tags[name], e)
		if err != nil {
			status = -1
		}
		switch status {
		case 0:
			candidates = append(candidates, name)
		case 1: // not merged into E: ignored
		default:
			return "", refusal{"base", "cannot decide whether the tag " + name + " of origin is an ancestor of the commit (git exit status " + strconv.Itoa(status) + ")"}
		}
	}
	if len(candidates) == 0 {
		return "", refusal{"base", "no Upstream release tag of origin is the commit or an ancestor of it"}
	}
	slices.SortStableFunc(candidates, func(x, y string) int { return compareTriples(triple(x), triple(y)) })
	for i := 1; i < len(candidates); i++ {
		if compareTriples(triple(candidates[i-1]), triple(candidates[i])) == 0 {
			return "", refusal{"base", "the tags " + candidates[i-1] + " and " + candidates[i] + " have the same numeric version"}
		}
	}
	best := candidates[len(candidates)-1]
	return best, nil
}

// checkBaseMatch requires the X.Y.Z of the Release Tag to equal the base tag
// name without its v, as an exact string.
func checkBaseMatch(tag, base string) error {
	if !releaseTagPattern.MatchString(tag) {
		return refusal{"base", "the tag name is not a Release Tag"}
	}
	if !upstreamTagPattern.MatchString(base) {
		return refusal{"base", "the Upstream Base is not an Upstream release tag"}
	}
	version, _, _ := strings.Cut(strings.TrimPrefix(tag, "v"), "-ezop.")
	if version != strings.TrimPrefix(base, "v") {
		return refusal{"base", "the tag version " + version + " is not the Upstream Base " + base}
	}
	return nil
}

// checkTagBinding decides whether a Release Tag, the commit it names, the
// Upstream release it is based on and the build toolchain agree, and returns
// the commit E. The checks run in order and the first refusal ends the run:
// the tag pattern; the identity of HEAD, GITHUB_SHA and the tag as listed by
// origin; reachability from the origin/ezop fetched once; the Upstream Base;
// the toolchain pins in the tree of E. It creates, moves and pushes no tag and
// writes nothing to origin. Its one fetch of origin's ezop branch (FetchEzop)
// does update local bookkeeping: the remote-tracking ref refs/remotes/origin/ezop
// and FETCH_HEAD. That fetch is required by the reachability check. The caller
// has checked that origin is the canonical remote and has fetched the objects of
// origin's tags.
func checkTagBinding(g gitRepo, tag, githubSHA, pin string) (string, error) {
	if err := checkTagPattern(tag); err != nil {
		return "", err
	}
	head, err := g.ResolveCommit("HEAD")
	if err != nil {
		head = ""
	}
	if fullSHA.MatchString(githubSHA) {
		if githubSHA, err = g.ResolveCommit(githubSHA); err != nil {
			githubSHA = ""
		}
	}
	tags, err := g.OriginTags()
	if err != nil {
		return "", refusal{"identity", "could not list the tags of origin"}
	}
	e, err := checkIdentity(head, githubSHA, tags[tag])
	if err != nil {
		return "", err
	}
	if err := checkReachable(g, e); err != nil {
		return "", err
	}
	base, err := upstreamBase(g, e, tags)
	if err != nil {
		return "", err
	}
	if err := checkBaseMatch(tag, base); err != nil {
		return "", err
	}
	if err := checkToolchainAt(g, e, pin); err != nil {
		return "", err
	}
	return e, nil
}
