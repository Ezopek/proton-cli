package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
)

// guardUsage is the one-line message for a malformed guard command line.
const guardUsage = "fork-release: usage: fork-release guard --profile release|mirror COMMIT"

// Exit codes: 0 pass, 1 held or refused, 2 usage.
const (
	exitPass  = 0
	exitHeld  = 1
	exitUsage = 2
)

// guardDeps is everything the commands touch outside the process: git, the
// build information, the canonical origin list and the two output streams.
// Tests replace it. The production value is productionDeps, and nothing on the
// command line or in the environment can change it.
type guardDeps struct {
	git     gitRepo
	build   func() (*debug.BuildInfo, bool)
	origins []string
	stdout  io.Writer
	stderr  io.Writer
}

// productionDeps is the real git adapter over the working directory, the
// binary's own build information and the canonical origin forms.
func productionDeps() guardDeps {
	return guardDeps{
		git:     newExecGit(),
		build:   debug.ReadBuildInfo,
		origins: canonicalOrigins,
		stdout:  os.Stdout,
		stderr:  os.Stderr,
	}
}

// run dispatches a command line (without the program name) and returns the
// exit code. Only guard exists so far.
func run(args []string, d guardDeps) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(d.stderr, "fork-release: no command")
		return exitUsage
	}
	if args[0] == "guard" {
		return runGuard(args[1:], d)
	}
	_, _ = fmt.Fprintln(d.stderr, "fork-release: unknown command")
	return exitUsage
}

// profilePaths are the only paths of a commit's tree a profile may open.
// mirror never names .goreleaser.yaml.
func profilePaths(profile string) []string {
	if profile == "release" {
		return []string{goreleaserPath, githubDir}
	}
	return []string{githubDir}
}

// runGuard is guard --profile release|mirror COMMIT. It checks, in order: the
// command line, the origin remote, the helper's own build identity, that the
// helper's commit is landed on the origin/ezop it fetches once, and then
// classifies the tree of exactly COMMIT. A refusal is one line on standard
// error and nothing on standard output; a verdict starts with the full SHA
// that was checked.
func runGuard(args []string, d guardDeps) int {
	if len(args) != 3 || args[0] != "--profile" || (args[1] != "release" && args[1] != "mirror") {
		_, _ = fmt.Fprintln(d.stderr, guardUsage)
		return exitUsage
	}
	profile, rev := args[1], args[2]

	refuse := func(err error) int {
		_, _ = fmt.Fprintln(d.stderr, err)
		return exitHeld
	}

	fetchURLs, err := d.git.OriginURLs(false)
	if err != nil {
		return refuse(refusal{"origin", "origin is not configured or cannot be read"})
	}
	pushURLs, err := d.git.OriginURLs(true)
	if err != nil {
		return refuse(refusal{"origin", "origin is not configured or cannot be read"})
	}
	if err := originURLsOK(fetchURLs, pushURLs, d.origins); err != nil {
		return refuse(err)
	}

	info, ok := d.build()
	if !ok {
		info = nil
	}
	helper, err := buildIdentity(info)
	if err != nil {
		return refuse(err)
	}

	tip, err := d.git.FetchEzop()
	if err != nil || !fullSHA.MatchString(tip) {
		return refuse(refusal{"fetch", "could not fetch origin ezop"})
	}
	status, err := d.git.IsAncestor(helper, tip)
	if err != nil {
		status = -1
	}
	if err := ancestryDecision(status); err != nil {
		return refuse(err)
	}

	if rev == "" || strings.HasPrefix(rev, "-") {
		return refuse(refusal{"commit", "COMMIT must be a revision that names a commit, and must not start with a dash"})
	}
	commit, err := d.git.ResolveCommit(rev)
	if err != nil || !fullSHA.MatchString(commit) {
		return refuse(refusal{"commit", "COMMIT does not name a commit"})
	}

	view, err := commitTreeFS(d.git, commit, profilePaths(profile))
	if err != nil {
		return refuse(refusal{"tree", "could not read the tree of the commit"})
	}
	var findings []Finding
	if profile == "release" {
		findings = CheckRelease(view)
	} else {
		findings = CheckMirror(view)
	}
	if _, err := fmt.Fprintf(d.stdout, "checked %s\n%s", commit, verdict(profile, findings)); err != nil {
		return refuse(refusal{"output", "could not write the result"})
	}
	if len(findings) > 0 {
		return exitHeld
	}
	return exitPass
}
