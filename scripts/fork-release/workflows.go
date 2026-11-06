package main

import (
	"errors"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Rule names for the .github/ checks.
const (
	ruleWorkflowSet         = "workflow-set"
	ruleGithubFile          = "github-file"
	ruleGithubNonRegular    = "github-non-regular"
	ruleWorkflowShape       = "workflow-shape"
	ruleWorkflowPermissions = "workflow-permissions"
	ruleWorkflowAction      = "workflow-action"
	ruleWorkflowTrigger     = "workflow-trigger"
	ruleWorkflowSecrets     = "workflow-secrets"
	ruleWorkflowExpression  = "workflow-expression"
	ruleWorkflowJob         = "workflow-job"
	ruleWorkflowCommand     = "workflow-command"
	ruleWorkflowUnreadable  = "workflow-unreadable"
)

const (
	githubDir    = ".github"
	workflowsDir = ".github/workflows"
	forkRelease  = "fork-release.yml"
	// forkReleaseTag is the one tag pattern that may start the release workflow.
	forkReleaseTag = "v*-ezop.*"
)

// publisherJobs are job names no workflow may carry.
var publisherJobs = []string{"apt-repo", "apt-deploy", "npm-publish"}

// workflowRules selects the exceptions granted to fork-release.yml. The mirror
// profile of a later story turns them off, and fork-release.yml is then not a
// permitted workflow at all.
type workflowRules struct {
	allowForkRelease bool
}

// permitted lists the workflow files that may exist.
func (r workflowRules) permitted() []string {
	if r.allowForkRelease {
		return []string{"ci.yml", forkRelease}
	}
	return []string{"ci.yml"}
}

// checkWorkflows applies the .github/ rules: the file set, and the shared rules
// for every workflow that may run.
func checkWorkflows(fsys fs.FS, rules workflowRules) []Finding {
	info, err := fs.Stat(fsys, githubDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return []Finding{{githubDir, ruleGithubFile, "cannot be read: " + err.Error()}}
	}
	if !info.IsDir() {
		return []Finding{{githubDir, ruleGithubFile, "exists and is not a directory"}}
	}

	var out []Finding
	var present []string
	walkErr := fs.WalkDir(fsys, githubDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			out = append(out, Finding{p, ruleGithubFile, "cannot be read: " + err.Error()})
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			out = append(out, Finding{p, ruleGithubNonRegular, "not a regular file (" + d.Type().String() + ")"})
			return nil
		}
		rest := strings.TrimPrefix(p, githubDir+"/")
		switch {
		case strings.HasPrefix(rest, "ISSUE_TEMPLATE/"):
			// Inert: GitHub reads these as templates and runs nothing.
		case strings.HasPrefix(rest, "workflows/"):
			name := strings.TrimPrefix(rest, "workflows/")
			if !strings.Contains(name, "/") && slices.Contains(rules.permitted(), name) {
				present = append(present, name)
			} else {
				out = append(out, Finding{p, ruleWorkflowSet, "not in the permitted list (" + strings.Join(rules.permitted(), ", ") + ")"})
			}
		default:
			out = append(out, Finding{p, ruleGithubFile, "only workflows and ISSUE_TEMPLATE files may exist under .github"})
		}
		return nil
	})
	if walkErr != nil {
		out = append(out, Finding{githubDir, ruleGithubFile, "cannot be walked: " + walkErr.Error()})
	}

	slices.Sort(present)
	for _, name := range present {
		out = append(out, checkWorkflowFile(fsys, name, rules)...)
	}
	return out
}

func checkWorkflowFile(fsys fs.FS, name string, rules workflowRules) []Finding {
	p := workflowsDir + "/" + name
	data, err := fs.ReadFile(fsys, p)
	if err != nil {
		return []Finding{{p, ruleWorkflowUnreadable, err.Error()}}
	}
	var out []Finding
	value, issues := parseYAML(data)
	for _, i := range issues {
		out = append(out, Finding{p, i.rule, i.detail})
	}
	if value == nil {
		// A parse failure already has its finding. A document that decodes to
		// nothing (null, ~) has nothing to check, so it is held too.
		if len(out) == 0 {
			out = append(out, Finding{p, ruleYAMLParse, "the document is null"})
		}
		return out
	}
	hold := func(rule, detail string) { out = append(out, Finding{p, rule, detail}) }
	checkWorkflow(value, rules.allowForkRelease && name == forkRelease, hold)
	return out
}

// checkWorkflow applies the shared enabled-workflow rules to one parsed
// workflow. isFork grants the exceptions of fork-release.yml.
func checkWorkflow(value any, isFork bool, hold func(rule, detail string)) {
	root, ok := value.(map[string]any)
	if !ok {
		hold(ruleWorkflowShape, "the workflow is not a mapping")
		return
	}

	if perms, ok := root["permissions"]; !ok {
		hold(ruleWorkflowPermissions, "no top-level permissions block")
	} else {
		checkPermissions("top level", perms, false, hold)
	}

	checkTrigger(root, isFork, hold)

	jobs, ok := root["jobs"].(map[string]any)
	if !ok {
		hold(ruleWorkflowShape, "jobs is absent or not a mapping")
	}
	for _, id := range sortedKeys(jobs) {
		checkJob(id, jobs[id], isFork, hold)
	}

	unknown := walkTree(root, nil,
		func([]string, string) {},
		func(path []string, s string) {
			if bad := badSecretRefs(s); bad {
				hold(ruleWorkflowSecrets, "reference to secrets other than secrets.GITHUB_TOKEN at "+dotted(path))
			}
		})
	if unknown != "" {
		hold(ruleWorkflowShape, "value of an unexpected type at "+unknown)
	}
}

// checkPermissions holds any write scope. allowContents lets contents: write
// stand, for the one job of fork-release.yml that publishes.
func checkPermissions(scope string, v any, allowContents bool, hold func(rule, detail string)) {
	switch p := v.(type) {
	case string:
		if p != "read-all" {
			hold(ruleWorkflowPermissions, scope+" permissions: "+strconv.Quote(p)+" is not read-all")
		}
	case map[string]any:
		for _, k := range sortedKeys(p) {
			switch p[k] {
			case "read", "none":
			case "write":
				if !allowContents || k != "contents" {
					hold(ruleWorkflowPermissions, scope+" holds "+k+": write")
				}
			default:
				hold(ruleWorkflowPermissions, scope+" permission "+k+" is "+show(p[k]))
			}
		}
	default:
		hold(ruleWorkflowPermissions, scope+" permissions has an unexpected shape")
	}
}

// checkTrigger holds a tag-reachable trigger. A push trigger reaches tags when
// it filters on tags, or when it has neither a branches nor a branches-ignore
// filter, since path filters are not evaluated for a tag push. The create event
// is tag-reachable too. fork-release.yml is the exception, and only with
// exactly the pinned trigger.
func checkTrigger(root map[string]any, isFork bool, hold func(rule, detail string)) {
	on, ok := root["on"]
	if !ok {
		hold(ruleWorkflowTrigger, "no on: trigger")
		return
	}
	if isFork {
		if !isForkTrigger(on) {
			hold(ruleWorkflowTrigger, "fork-release.yml must have exactly on: push: tags: ['"+forkReleaseTag+"']")
		}
		return
	}

	var events []string
	var pushValue any
	switch o := on.(type) {
	case string:
		events = []string{o}
	case []any:
		for _, e := range o {
			s, ok := e.(string)
			if !ok {
				hold(ruleWorkflowTrigger, "on lists an event that is not a string")
				return
			}
			events = append(events, s)
		}
	case map[string]any:
		events = sortedKeys(o)
		pushValue = o["push"]
	default:
		hold(ruleWorkflowTrigger, "on has an unexpected shape")
		return
	}

	for _, e := range events {
		switch e {
		case "create":
			hold(ruleWorkflowTrigger, "tag-reachable: the create event")
		case "push":
			switch pv := pushValue.(type) {
			case nil:
				hold(ruleWorkflowTrigger, "tag-reachable: push without a branches or branches-ignore filter")
			case map[string]any:
				_, tags := pv["tags"]
				_, tagsIgnore := pv["tags-ignore"]
				_, branches := pv["branches"]
				_, branchesIgnore := pv["branches-ignore"]
				switch {
				case tags || tagsIgnore:
					hold(ruleWorkflowTrigger, "tag-reachable: push with a tags or tags-ignore filter")
				case !branches && !branchesIgnore:
					hold(ruleWorkflowTrigger, "tag-reachable: push without a branches or branches-ignore filter")
				}
			default:
				hold(ruleWorkflowTrigger, "push has an unexpected shape")
			}
		}
	}
}

// isForkTrigger reports whether on is exactly push: tags: ['v*-ezop.*'].
func isForkTrigger(on any) bool {
	m, ok := on.(map[string]any)
	if !ok || len(m) != 1 {
		return false
	}
	push, ok := m["push"].(map[string]any)
	if !ok || len(push) != 1 {
		return false
	}
	tags, ok := push["tags"].([]any)
	return ok && len(tags) == 1 && tags[0] == forkReleaseTag
}

func checkJob(id string, v any, isFork bool, hold func(rule, detail string)) {
	if slices.Contains(publisherJobs, id) {
		hold(ruleWorkflowJob, "job "+id+" is a publisher job")
	}
	job, ok := v.(map[string]any)
	if !ok {
		hold(ruleWorkflowShape, "job "+id+" is not a mapping")
		return
	}
	if perms, ok := job["permissions"]; ok {
		checkPermissions("job "+id, perms, isFork && id == "release", hold)
	}
	if u, ok := job["uses"]; ok {
		checkUses("job "+id, u, isFork, hold)
	}
	stepsValue, ok := job["steps"]
	if !ok {
		return
	}
	steps, ok := stepsValue.([]any)
	if !ok {
		hold(ruleWorkflowShape, "job "+id+" steps is not a list")
		return
	}
	for i, sv := range steps {
		where := "job " + id + " step " + strconv.Itoa(i+1)
		step, ok := sv.(map[string]any)
		if !ok {
			hold(ruleWorkflowShape, where+" is not a mapping")
			continue
		}
		if u, ok := step["uses"]; ok {
			checkUses(where, u, isFork, hold)
		}
		if r, ok := step["run"]; ok {
			body, ok := r.(string)
			if !ok {
				hold(ruleWorkflowShape, where+" run is not a string")
				continue
			}
			if strings.Contains(body, "${{") {
				hold(ruleWorkflowExpression, where+" has a ${{ }} expression in run; pass it through env")
			}
			for _, cmd := range forbiddenCommands(body) {
				hold(ruleWorkflowCommand, where+" runs "+cmd)
			}
		}
	}
}

// checkUses holds an action that is not an exact entry of the action list, and
// the GoReleaser action anywhere but fork-release.yml.
func checkUses(where string, v any, isFork bool, hold func(rule, detail string)) {
	u, ok := v.(string)
	if !ok {
		hold(ruleWorkflowShape, where+" uses is not a string")
		return
	}
	name, _, _ := strings.Cut(u, "@")
	switch {
	case name == goreleaserAction && !isFork:
		hold(ruleWorkflowAction, where+" uses "+u+": the GoReleaser action is permitted only in "+forkRelease)
	case !allowedActions[u]:
		hold(ruleWorkflowAction, where+" uses "+u+": not an entry of the action list (a full 40-hex SHA is required)")
	}
}

var secretsWord = regexp.MustCompile(`(?i)\bsecrets\b`)

// badSecretRefs reports whether s refers to secrets in any way other than
// secrets.GITHUB_TOKEN. It is deliberately blunt: the word alone counts.
func badSecretRefs(s string) bool {
	for _, loc := range secretsWord.FindAllStringIndex(s, -1) {
		after := s[loc[1]:]
		if !strings.HasPrefix(after, ".GITHUB_TOKEN") {
			return true
		}
		if rest := after[len(".GITHUB_TOKEN"):]; rest != "" && isNameChar(rest[0]) {
			return true
		}
		if s[loc[0]:loc[1]] != "secrets" {
			return true
		}
	}
	return false
}

func isNameChar(c byte) bool {
	return c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// forbiddenCommands returns every command a shell body invokes that the release
// rules enumerate. It is a conservative match of those patterns, not an analysis
// of arbitrary shell: the body is split on newlines and shell separators, quotes
// are stripped, and each segment is read as words, so text that is only echoed
// is held too, and a command the patterns do not name is not seen.
func forbiddenCommands(body string) []string {
	var found []string
	add := func(s string) {
		if !slices.Contains(found, s) {
			found = append(found, s)
		}
	}
	body = strings.ReplaceAll(body, "\\\n", " ")
	var lines []string
	for _, l := range strings.Split(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			lines = append(lines, l)
		}
	}
	body = strings.Join(lines, "\n")
	body = strings.NewReplacer(";", "\n", "&", "\n", "|", "\n", "(", "\n", ")", "\n", "`", "\n", "{", "\n", "}", "\n",
		"'", "", `"`, "", "\\", "").Replace(body)

	for _, seg := range strings.Split(body, "\n") {
		words := strings.Fields(seg)
		for i, w := range words {
			rest := words[i+1:]
			switch path.Base(w) {
			case "git":
				if slices.Contains(rest, "push") {
					add("git push")
				}
				if slices.Contains(rest, "tag") {
					add("git tag")
				}
				if ci := slices.Index(rest, "config"); ci >= 0 {
					for _, r := range rest[ci+1:] {
						if strings.HasPrefix(r, "user.") {
							add("git config user.*")
						}
					}
				}
				// An identity can also be set inline: git -c user.name=... or
				// git -c user.email=....
				for k := 0; k+1 < len(rest); k++ {
					if rest[k] == "-c" && (strings.HasPrefix(rest[k+1], "user.name=") || strings.HasPrefix(rest[k+1], "user.email=")) {
						add("git -c user.*")
					}
				}
			case "gh":
				for j := 0; j+1 < len(rest); j++ {
					if rest[j] == "release" && slices.Contains([]string{"create", "edit", "upload", "delete", "delete-asset"}, rest[j+1]) {
						add("gh release " + rest[j+1])
					}
				}
				if ai := slices.Index(rest, "api"); ai >= 0 {
					if !onlyGet(rest[ai+1:]) {
						add("gh api without exactly one method flag, GET")
					}
				}
			case "curl", "wget":
				add(path.Base(w))
			case "goreleaser":
				if slices.Contains(rest, "release") {
					add("goreleaser release")
				}
			}
		}
	}
	return found
}

// onlyGet reports whether the arguments of a gh api call carry exactly one
// method flag and it is GET. gh api switches to POST when parameters are added,
// so the flag must be explicit.
func onlyGet(args []string) bool {
	var methods []string
	for i, a := range args {
		switch {
		case a == "--method" || a == "-X":
			if i+1 < len(args) {
				methods = append(methods, args[i+1])
			} else {
				methods = append(methods, "")
			}
		case strings.HasPrefix(a, "--method="):
			methods = append(methods, strings.TrimPrefix(a, "--method="))
		case strings.HasPrefix(a, "-X") && len(a) > 2:
			methods = append(methods, strings.TrimPrefix(a[2:], "="))
		}
	}
	return len(methods) == 1 && methods[0] == "GET"
}
