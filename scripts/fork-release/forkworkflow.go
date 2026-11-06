package main

import (
	"fmt"
	"io/fs"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// This file pins the shape of .github/workflows/fork-release.yml, the one
// workflow that publishes. The expected structure is data below; the check
// compares the parsed workflow with it and reports one finding per violated
// predicate under ruleWorkflowShape, naming the predicate and never quoting the
// file. The shape changes only in the same reviewed topic as the workflow.

const (
	forkWorkflowName  = "Fork Release"
	forkRunner        = "ubuntu-24.04"
	forkRepository    = "github.repository == 'Ezopek/proton-cli'"
	forkConcurrency   = "${{ github.workflow }}-${{ github.ref }}"
	forkPinReference  = "${{ env.GORELEASER_PIN }}"
	forkTokenValue    = "${{ secrets.GITHUB_TOKEN }}"
	forkCurrentTag    = "${{ github.ref_name }}"
	forkNotesArgument = "--release-notes"
	forkHelperBuild   = `go build -o "$RUNNER_TEMP/fork-release" ./scripts/fork-release`
)

// stepKeys are the only keys a step may carry. Any other, such as if,
// continue-on-error, shell, working-directory or id, could turn off a guard.
var stepKeys = []string{"name", "uses", "run", "with", "env"}

// wantStep is one step of a job: an action (uses) or a script (run), with its
// exact with and env maps. A nil map means the key is absent.
type wantStep struct {
	uses string // the repository of a listed action, or "" for a run step
	run  string
	with map[string]any
	env  map[string]any
}

func helperRun(args string) string { return `"$RUNNER_TEMP/fork-release" ` + args }

func tokenOnlyEnv() map[string]any { return map[string]any{"GITHUB_TOKEN": forkTokenValue} }

func setupGoStep() wantStep {
	return wantStep{uses: "actions/setup-go", with: map[string]any{"go-version-file": "go.mod", "cache": false}}
}

func helperBuildStep() wantStep { return wantStep{run: forkHelperBuild} }

// releaseSteps and stampSteps are the closed step lists, in order.
func releaseSteps() []wantStep {
	return []wantStep{
		{uses: "actions/checkout", with: map[string]any{"fetch-depth": 0, "persist-credentials": false}},
		setupGoStep(),
		helperBuildStep(),
		{run: helperRun("guard --in-run"), env: tokenOnlyEnv()},
		{run: helperRun("notes --in-run")},
		{run: helperRun("guard --recheck"), env: tokenOnlyEnv()},
		{
			uses: goreleaserAction,
			with: map[string]any{
				"version": forkPinReference,
				"args":    "release --clean " + forkNotesArgument + " ${{ runner.temp }}/fork-release-notes.md",
			},
			env: map[string]any{"GITHUB_TOKEN": forkTokenValue, "GORELEASER_CURRENT_TAG": forkCurrentTag},
		},
		{run: helperRun("assets --in-run"), env: tokenOnlyEnv()},
		{run: "echo \"Go: $(go version)\" >> \"$GITHUB_STEP_SUMMARY\"\necho \"GoReleaser: $GORELEASER_PIN\" >> \"$GITHUB_STEP_SUMMARY\""},
	}
}

func stampSteps() []wantStep {
	return []wantStep{
		{uses: "actions/checkout", with: map[string]any{"persist-credentials": false}},
		setupGoStep(),
		helperBuildStep(),
		{run: helperRun("stamp --in-run"), env: tokenOnlyEnv()},
	}
}

// wantJob is the expected shape of one job. The key set is exact.
type wantJob struct {
	id          string
	keys        []string
	condition   string // the if of the job, or "" for none
	needs       string // the needs of the job, or "" for none
	permissions map[string]any
	steps       []wantStep
}

func forkJobs() []wantJob {
	return []wantJob{
		{
			id: "release", keys: []string{"if", "runs-on", "permissions", "steps"},
			condition:   forkRepository,
			permissions: map[string]any{"contents": "write"},
			steps:       releaseSteps(),
		},
		{
			id: "stamp", keys: []string{"needs", "runs-on", "permissions", "steps"},
			needs:       "release",
			permissions: map[string]any{"contents": "read"},
			steps:       stampSteps(),
		},
	}
}

// checkForkWorkflow holds the shape of fork-release.yml when the file is
// present. A tree without it passes: presence is required by the actual-tree
// test, not here. A file that cannot be read or parsed is the shared rules'
// finding, so nothing is added for it.
func checkForkWorkflow(fsys fs.FS) []Finding {
	data, err := fs.ReadFile(fsys, forkReleasePath)
	if err != nil {
		return nil
	}
	value, _ := parseYAML(data)
	if value == nil {
		return nil
	}
	var out []Finding
	hold := func(detail string) { out = append(out, Finding{forkReleasePath, ruleWorkflowShape, detail}) }
	root, ok := value.(map[string]any)
	if !ok {
		hold("the workflow is not a mapping")
		return out
	}
	checkForkRoot(root, hold)
	return out
}

func checkForkRoot(root map[string]any, hold func(string)) {
	jobs := forkJobs()
	if !sameKeys(root, "name", "on", "permissions", "env", "concurrency", "jobs") {
		hold("the top-level keys are not exactly name, on, permissions, env, concurrency and jobs")
	}
	if !sameScalar(root["name"], forkWorkflowName) {
		hold("the name is not " + forkWorkflowName)
	}
	if !isForkTrigger(root["on"]) {
		hold("the trigger is not exactly push of the tags " + forkReleaseTag)
	}
	if !sameMap(root["permissions"], map[string]any{"contents": "read"}) {
		hold("the top-level permissions are not exactly contents: read")
	}
	if !pinEnv(root["env"]) {
		hold("the top-level env is not exactly " + workflowPinKey + " in the form vX.Y.Z")
	}
	if !sameMap(root["concurrency"], map[string]any{"group": forkConcurrency, "cancel-in-progress": false}) {
		hold("the concurrency block is not the group of the workflow name and ref with cancel-in-progress false")
	}

	jobMap, _ := root["jobs"].(map[string]any)
	if jobMap == nil || len(jobMap) != len(jobs) || !hasAll(jobMap, jobs) {
		hold("the jobs are not exactly release and stamp")
	}
	for _, j := range jobs {
		if raw, ok := jobMap[j.id]; ok {
			checkForkJob(j, raw, hold)
		}
	}

	checkGoreleaserSteps(jobMap, hold)
	checkCacheUse(jobMap, hold)
}

func hasAll(m map[string]any, jobs []wantJob) bool {
	for _, j := range jobs {
		if _, ok := m[j.id]; !ok {
			return false
		}
	}
	return true
}

// pinEnv reports whether env is exactly the pin key with a vX.Y.Z string.
func pinEnv(v any) bool {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 1 {
		return false
	}
	pin, ok := m[workflowPinKey].(string)
	return ok && goreleaserPinPattern.MatchString(pin)
}

func checkForkJob(want wantJob, raw any, hold func(string)) {
	where := "job " + want.id
	job, ok := raw.(map[string]any)
	if !ok {
		hold(where + " is not a mapping")
		return
	}
	if !sameKeys(job, want.keys...) {
		hold(where + " keys are not exactly " + strings.Join(want.keys, ", "))
	}
	if !sameScalar(job["runs-on"], forkRunner) {
		hold(where + " runs-on is not " + forkRunner)
	}
	if want.condition != "" && !sameScalar(job["if"], want.condition) {
		hold(where + " condition is not the repository identity")
	}
	if want.needs != "" && !sameScalar(job["needs"], want.needs) {
		hold(where + " needs is not " + want.needs)
	}
	if !sameMap(job["permissions"], want.permissions) {
		hold(where + " permissions are not exactly " + permissionText(want.permissions))
	}

	steps, ok := job["steps"].([]any)
	if !ok {
		hold(where + " steps is not a list")
		return
	}
	if len(steps) != len(want.steps) {
		hold(where + " has " + strconv.Itoa(len(steps)) + " steps, not " + strconv.Itoa(len(want.steps)))
		return
	}
	for i, w := range want.steps {
		checkForkStep(where+" step "+strconv.Itoa(i+1), w, steps[i], hold)
	}
}

func permissionText(p map[string]any) string {
	var parts []string
	for _, k := range sortedKeys(p) {
		parts = append(parts, fmt.Sprintf("%s: %v", k, p[k]))
	}
	return strings.Join(parts, ", ")
}

func checkForkStep(where string, want wantStep, raw any, hold func(string)) {
	step, ok := raw.(map[string]any)
	if !ok {
		hold(where + " is not a mapping")
		return
	}
	for _, k := range sortedKeys(step) {
		if !slices.Contains(stepKeys, k) {
			hold(where + " keys include one outside name, uses, run, with and env")
			break
		}
	}
	if want.uses != "" {
		entry, listed := listedAction(want.uses)
		if !listed {
			hold(where + " uses " + want.uses + ", which has no entry in the action list")
		} else if !sameScalar(step["uses"], entry) {
			hold(where + " uses is not the listed entry of " + want.uses)
		}
		if _, has := step["run"]; has {
			hold(where + " run is present beside uses")
		}
	} else {
		if _, has := step["uses"]; has {
			hold(where + " uses is present where a run step belongs")
		}
		got, isString := step["run"].(string)
		if !isString || strings.TrimRight(got, " \t\r\n") != want.run {
			hold(where + " run body differs from the pinned one")
		}
	}
	if !sameOptionalMap(step, "with", want.with) {
		hold(where + " with differs from the pinned one")
	}
	if !sameOptionalMap(step, "env", want.env) {
		hold(where + " env differs from the pinned one")
	}
}

// allSteps visits every step of every job that is a mapping, in job order.
func allSteps(jobs map[string]any, visit func(job string, index int, step map[string]any)) {
	for _, id := range sortedKeys(jobs) {
		job, _ := jobs[id].(map[string]any)
		steps, _ := job["steps"].([]any)
		for i, raw := range steps {
			if step, ok := raw.(map[string]any); ok {
				visit(id, i+1, step)
			}
		}
	}
}

func usesRepo(step map[string]any) (string, bool) {
	u, ok := step["uses"].(string)
	if !ok {
		return "", false
	}
	repo, _, _ := strings.Cut(u, "@")
	return repo, true
}

// checkGoreleaserSteps pins the one step that writes a release.
func checkGoreleaserSteps(jobs map[string]any, hold func(string)) {
	count := 0
	allSteps(jobs, func(job string, i int, step map[string]any) {
		repo, ok := usesRepo(step)
		if !ok || repo != goreleaserAction {
			return
		}
		count++
		where := "job " + job + " step " + strconv.Itoa(i)
		with, _ := step["with"].(map[string]any)
		env, _ := step["env"].(map[string]any)
		if !sameScalar(with["version"], forkPinReference) {
			hold(where + ": the GoReleaser version is not the reference to the pin env")
		}
		args, _ := with["args"].(string)
		if strings.Contains(args, "--skip") {
			hold(where + ": the GoReleaser arguments contain --skip")
		}
		if !strings.Contains(args, forkNotesArgument) {
			hold(where + ": the GoReleaser arguments lack " + forkNotesArgument)
		}
		if !sameScalar(env["GORELEASER_CURRENT_TAG"], forkCurrentTag) {
			hold(where + ": GORELEASER_CURRENT_TAG is not github.ref_name")
		}
	})
	if count != 1 {
		hold("exactly one GoReleaser step is required, found " + strconv.Itoa(count))
	}
}

// checkCacheUse holds a cache action and a setup-go step whose cache is not
// the boolean false.
func checkCacheUse(jobs map[string]any, hold func(string)) {
	allSteps(jobs, func(job string, i int, step map[string]any) {
		repo, ok := usesRepo(step)
		if !ok {
			return
		}
		where := "job " + job + " step " + strconv.Itoa(i)
		if strings.Contains(strings.ToLower(repo), "cache") {
			hold(where + " uses a cache action")
		}
		if repo == "actions/setup-go" {
			with, _ := step["with"].(map[string]any)
			if !sameScalar(with["cache"], false) {
				hold(where + ": the setup-go cache is not false")
			}
		}
	})
}

// listedAction returns the one entry of the action list for a repository.
func listedAction(repo string) (string, bool) {
	var found []string
	for entry := range allowedActions {
		if name, _, _ := strings.Cut(entry, "@"); name == repo {
			found = append(found, entry)
		}
	}
	if len(found) != 1 {
		return "", false
	}
	return found[0], true
}

// sameKeys reports whether m has exactly the given keys.
func sameKeys(m map[string]any, keys ...string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

// sameMap reports whether got is a mapping with exactly the keys and scalar
// values of want.
func sameMap(got any, want map[string]any) bool {
	m, ok := got.(map[string]any)
	if !ok || len(m) != len(want) {
		return false
	}
	for k, w := range want {
		g, ok := m[k]
		if !ok || !sameScalar(g, w) {
			return false
		}
	}
	return true
}

// sameOptionalMap compares the value under key of a step with want, where a nil
// want means the key is absent.
func sameOptionalMap(step map[string]any, key string, want map[string]any) bool {
	got, has := step[key]
	if want == nil {
		return !has
	}
	return has && sameMap(got, want)
}

// sameScalar compares a decoded scalar with an expected string, bool or int,
// by type as well as by value, so that the string "false" is not false.
func sameScalar(got, want any) bool {
	switch w := want.(type) {
	case string:
		g, ok := got.(string)
		return ok && g == w
	case bool:
		g, ok := got.(bool)
		return ok && g == w
	case int:
		v := reflect.ValueOf(got)
		switch v.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return v.Int() == int64(w)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return w >= 0 && v.Uint() == uint64(w)
		}
	}
	return false
}
