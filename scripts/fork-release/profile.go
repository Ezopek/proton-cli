package main

import "io/fs"

// CheckRelease applies the release profile to a tree rooted at the repository
// root and returns every held item in a stable order. It is a pure function of
// the tree: nothing outside fsys is read.
//
// The profile is for a tree that is, or becomes, an ezop commit. It holds the
// .goreleaser.yaml rules, the .github/ file set, and the rules every enabled
// workflow shares, with the exceptions of fork-release.yml.
func CheckRelease(fsys fs.FS) []Finding {
	out := checkGoreleaser(fsys)
	out = append(out, checkWorkflows(fsys, workflowRules{allowForkRelease: true})...)
	return sortFindings(out)
}
