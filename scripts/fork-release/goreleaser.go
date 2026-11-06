package main

import (
	"io/fs"
	"slices"
	"strconv"
	"strings"
)

const goreleaserPath = ".goreleaser.yaml"

// Rule names for the .goreleaser.yaml checks.
const (
	ruleGoUnreadable = "goreleaser-unreadable"
	ruleGoTopLevel   = "goreleaser-top-level-key"
	ruleGoHooks      = "goreleaser-hooks"
	ruleGoReleaseKey = "goreleaser-release-key"
	ruleGoReleasePin = "goreleaser-release-pin"
	ruleGoTemplate   = "goreleaser-template"
)

var (
	// goTopLevel is every key the cleaned configuration may have.
	goTopLevel = []string{"version", "before", "builds", "archives", "checksum", "release"}
	// goReleaseKeys is every key its release block may have.
	goReleaseKeys = []string{"draft", "prerelease", "mode", "replace_existing_artifacts"}
	// goHook is the one command that may run before the build.
	goHook = "bash scripts/gen-completions.sh"
	// goTemplateActions is the closed set of template actions. GoReleaser's
	// .Env, envOrDefault and isEnvSet all read the environment, and the cleaned
	// configuration uses none of them.
	goTemplateActions = []string{".Version", ".ProjectName", ".Os", ".Arch"}
)

// checkGoreleaser applies the .goreleaser.yaml rules of the release profile.
func checkGoreleaser(fsys fs.FS) []Finding {
	data, err := fs.ReadFile(fsys, goreleaserPath)
	if err != nil {
		return []Finding{{goreleaserPath, ruleGoUnreadable, err.Error()}}
	}
	var out []Finding
	value, issues := parseYAML(data)
	for _, i := range issues {
		out = append(out, Finding{goreleaserPath, i.rule, i.detail})
	}
	if value == nil {
		// A parse failure already has its finding. A document that decodes to
		// nothing (null, ~) has no pins to read, so it is held too.
		if len(out) == 0 {
			out = append(out, Finding{goreleaserPath, ruleYAMLParse, "the document is null"})
		}
		return out
	}
	hold := func(rule, detail string) { out = append(out, Finding{goreleaserPath, rule, detail}) }

	root, ok := value.(map[string]any)
	if !ok {
		hold(ruleGoTopLevel, "the file is not a mapping")
		return out
	}
	for _, k := range sortedKeys(root) {
		if !slices.Contains(goTopLevel, k) {
			hold(ruleGoTopLevel, "key "+k+" is not in the permitted list")
		}
	}
	checkBefore(root, hold)
	checkRelease(root, hold)

	unknown := walkTree(root, nil,
		func(path []string, key string) {
			if key == "hooks" && (len(path) != 1 || path[0] != "before") {
				hold(ruleGoHooks, "hooks key at "+dotted(append(path[:len(path):len(path)], key)))
			}
		},
		func(path []string, s string) {
			for _, bad := range badActions(s) {
				hold(ruleGoTemplate, "template action "+bad+" at "+dotted(path))
			}
		})
	if unknown != "" {
		hold(ruleGoTopLevel, "value of an unexpected type at "+unknown)
	}
	return out
}

// checkBefore pins before.hooks to the single completions command.
func checkBefore(root map[string]any, hold func(rule, detail string)) {
	before, ok := root["before"].(map[string]any)
	if !ok {
		hold(ruleGoHooks, "before is absent or not a mapping")
		return
	}
	for _, k := range sortedKeys(before) {
		if k != "hooks" {
			hold(ruleGoHooks, "before."+k+" is not permitted")
		}
	}
	hooks, ok := before["hooks"].([]any)
	switch {
	case !ok:
		hold(ruleGoHooks, "before.hooks is absent or not a list")
	case len(hooks) != 1:
		hold(ruleGoHooks, "before.hooks has "+strconv.Itoa(len(hooks))+" entries, want exactly one")
	case hooks[0] != goHook:
		hold(ruleGoHooks, "before.hooks[0] is "+show(hooks[0])+", want "+show(goHook))
	}
}

// checkRelease pins the release keys and the two non-replacing values. The pin
// does not rely on GoReleaser's defaults, so an absent key is held. The values
// are compared by type: the string keep-existing and the boolean false.
func checkRelease(root map[string]any, hold func(rule, detail string)) {
	rel, ok := root["release"].(map[string]any)
	if !ok {
		hold(ruleGoReleasePin, "release is absent or not a mapping")
		return
	}
	for _, k := range sortedKeys(rel) {
		if !slices.Contains(goReleaseKeys, k) {
			hold(ruleGoReleaseKey, "release."+k+" is not in the permitted list")
		}
	}
	if v, ok := rel["mode"]; !ok {
		hold(ruleGoReleasePin, "release.mode is absent, want \"keep-existing\"")
	} else if v != "keep-existing" {
		hold(ruleGoReleasePin, "release.mode is "+show(v)+", want \"keep-existing\" (string)")
	}
	if v, ok := rel["replace_existing_artifacts"]; !ok {
		hold(ruleGoReleasePin, "release.replace_existing_artifacts is absent, want false")
	} else if v != false {
		hold(ruleGoReleasePin, "release.replace_existing_artifacts is "+show(v)+", want false (bool)")
	}
}

// badActions returns every template action in s outside the closed set, and an
// unterminated opening as such.
func badActions(s string) []string {
	var bad []string
	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			return bad
		}
		j := strings.Index(s[i+2:], "}}")
		if j < 0 {
			return append(bad, "{{ (unterminated)")
		}
		inner := s[i+2 : i+2+j]
		if !slices.Contains(goTemplateActions, strings.TrimSpace(inner)) {
			bad = append(bad, "{{"+inner+"}}")
		}
		s = s[i+2+j+2:]
	}
}
