package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// prePushArgs judges the command line of guard --pre-push TAG C from the
// command line alone, before anything is read: exactly a Release Tag and a
// 40-character lowercase hex commit.
func prePushArgs(args []string) (tag, commit string, ok bool) {
	if len(args) != 2 || !releaseTagPattern.MatchString(args[0]) || !fullSHA.MatchString(args[1]) {
		return "", "", false
	}
	return args[0], args[1], true
}

// runPrePush is guard --pre-push TAG C (args are what follows the flag). It is
// the last check before an immutable tag is pushed. It checks, in order and
// stopping at the first refusal, so that no network read is made when a local
// fact already fails: the origin remote; that the helper was built, clean, at
// C; one fresh fetch of origin ezop and that C is a commit on it; that origin
// does not hold TAG; the fetch of the Upstream release tags; the Upstream Base;
// the release profile and the toolchain on the tree of C; and the record and
// release lookups. Only then does it print C:refs/tags/TAG, so that the tag the
// Operator pushes is bound by value to the commit that was checked.
//
// It writes nothing to origin: it pushes nothing, creates and reads no local tag,
// reads no Actions state and makes no write call to GitHub. Its fetches change
// the local clone only as git does for them: the fetch of origin ezop writes
// FETCH_HEAD and may update the remote-tracking ref refs/remotes/origin/ezop, and
// the fetch of the Upstream tags writes only FETCH_HEAD. A refusal is one line
// per reason on standard error and nothing on standard output.
func runPrePush(args []string, d guardDeps) int {
	tag, c, ok := prePushArgs(args)
	if !ok {
		_, _ = fmt.Fprintln(d.stderr, guardUsage)
		return exitUsage
	}
	if err := prePushChecks(d, tag, c); err != nil {
		return refuseWith(d, err)
	}
	if _, err := fmt.Fprintf(d.stdout, "%s:refs/tags/%s\n", c, tag); err != nil {
		return refuseWith(d, refusal{"output", "could not write the result"})
	}
	return exitPass
}

// prePushChecks runs the eight checks of the command in their fixed order and
// answers the first refusal. Every failed read is a refusal.
func prePushChecks(d guardDeps, tag, c string) error {
	g := d.git

	// 1. origin, on every effective fetch and push URL.
	fetchURLs, err := g.OriginURLs(false)
	if err != nil {
		return refusal{"origin", "origin is not configured or cannot be read"}
	}
	pushURLs, err := g.OriginURLs(true)
	if err != nil {
		return refusal{"origin", "origin is not configured or cannot be read"}
	}
	if err := originURLsOK(fetchURLs, pushURLs, d.origins); err != nil {
		return err
	}

	// 2. The helper is built, clean, at C: the code that gates the push is the
	// code that will run at C. Landedness of the helper follows from check 3.
	helper, err := helperCommit(d)
	if err != nil {
		return err
	}
	if helper != c {
		return refusal{"build", "the helper was built from another commit than the candidate; build it in a clean clone checked out at the candidate"}
	}

	// 3. C is a commit on the origin/ezop fetched now.
	tip, err := g.FetchEzop()
	if err != nil || !fullSHA.MatchString(tip) {
		return refusal{"fetch", "could not fetch origin ezop"}
	}
	if resolved, err := g.ResolveCommit(c); err != nil || resolved != c {
		return refusal{"reachable", "the candidate does not name a commit by its own id"}
	}
	status, err := g.IsAncestor(c, tip)
	if err != nil {
		status = -1
	}
	if err := reachableDecision(status); err != nil {
		return err
	}

	// 4. Positive absence of TAG on origin, from one fresh listing that also
	// feeds the base check.
	tags, err := g.OriginTags()
	if err != nil {
		return refusal{"tag", "could not list the tags of origin"}
	}
	if _, exists := tags[tag]; exists {
		return refusal{"tag", "the tag already exists on origin"}
	}

	// 5. The objects of the Upstream release tags, by name, into FETCH_HEAD only.
	if err := fetchUpstreamTags(g, tags); err != nil {
		return err
	}

	// 6. The Upstream Base of C is the tag's X.Y.Z.
	base, err := upstreamBase(g, c, tags)
	if err != nil {
		return err
	}
	if err := checkBaseMatch(tag, base); err != nil {
		return err
	}

	// 7. The release profile and the toolchain, on the tree of C.
	if err := profileStep(g)(c); err != nil {
		return err
	}
	pin, err := treePin(g, c)
	if err != nil {
		return err
	}
	if err := checkToolchainAt(g, c, pin); err != nil {
		return err
	}

	// 8. No verification record and no release for TAG.
	return repeatCheck(d, tag, c)
}

// helperCommit is the commit the running helper was built from, from its
// embedded build information, which must be present and clean.
func helperCommit(d guardDeps) (string, error) {
	if d.build == nil {
		return buildIdentity(nil)
	}
	info, ok := d.build()
	if !ok {
		info = nil
	}
	return buildIdentity(info)
}

// fetchUpstreamTags fetches, by name, every tag of the listing that is an
// Upstream release tag, so that its objects are present for the ancestry test.
// With none nothing is fetched and the base check refuses. A git without the
// capability, or a failed fetch, is a refusal.
func fetchUpstreamTags(g gitRepo, tags map[string]string) error {
	var names []string
	for name := range tags {
		if upstreamTagPattern.MatchString(name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	slices.Sort(names)
	f, ok := g.(tagFetcher)
	if !ok {
		return refusal{"fetch", "this git adapter cannot fetch the Upstream release tags of origin"}
	}
	if err := f.FetchTags(names); err != nil {
		return refusal{"fetch", "could not fetch the Upstream release tags of origin"}
	}
	return nil
}

// treePin reads the GoReleaser pin from fork-release.yml in the tree of the
// commit c, never the working tree. The pin reader is the one verify uses; its
// refusal is re-worded under the check toolchain.
func treePin(g gitRepo, c string) (string, error) {
	view, err := commitTreeFS(g, c, []string{forkReleasePath})
	if err != nil {
		return "", refusal{"toolchain", "could not read the tree of the commit"}
	}
	workflow, err := readRegular(view, forkReleasePath)
	if err != nil {
		return "", refusal{"toolchain", forkRelease + " is missing or is not a readable regular file in the tree of the commit"}
	}
	pin, err := workflowPin(workflow)
	if err != nil {
		var r refusal
		if errors.As(err, &r) {
			return "", refusal{"toolchain", strings.TrimSpace(r.reason)}
		}
		return "", refusal{"toolchain", "the GoReleaser pin cannot be read"}
	}
	return pin, nil
}
