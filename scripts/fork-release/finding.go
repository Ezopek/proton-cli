package main

import (
	"fmt"
	"slices"
	"strings"
)

// Finding is one held item: a path, the rule that holds it and the detail.
type Finding struct {
	Path   string
	Rule   string
	Detail string
}

// String renders a finding on one line.
func (f Finding) String() string {
	return f.Path + ": " + f.Rule + ": " + f.Detail
}

// sortFindings returns the findings in a stable order, without exact
// duplicates. It leaves its input alone.
func sortFindings(fs []Finding) []Finding {
	out := slices.Clone(fs)
	slices.SortFunc(out, func(a, b Finding) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if c := strings.Compare(a.Rule, b.Rule); c != 0 {
			return c
		}
		return strings.Compare(a.Detail, b.Detail)
	})
	return slices.Compact(out)
}

// verdict renders the outcome of a profile and names every held item.
func verdict(profile string, fs []Finding) string {
	if len(fs) == 0 {
		return profile + ": pass\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: held, %d item(s)\n", profile, len(fs))
	for _, f := range fs {
		b.WriteString("  " + f.String() + "\n")
	}
	return b.String()
}
