package main

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// realWorkflow is the fork-release.yml of this repository, the healthy fixture
// of every test here, so the file and its pinned shape cannot drift apart unseen.
func realWorkflow(t *testing.T) string {
	t.Helper()
	data, err := fs.ReadFile(repoRoot(), forkReleasePath)
	if err != nil {
		t.Fatalf("the real %s cannot be read: %v", forkReleasePath, err)
	}
	return string(data)
}

// shapeTree is the cleaned fork tree with the given text as fork-release.yml.
func shapeTree(t *testing.T, src string) fstest.MapFS {
	t.Helper()
	fsys := cleanTree(t)
	fsys[forkReleasePath] = fstestFile(src)
	return fsys
}

// wantShape fails unless a workflow-shape finding on fork-release.yml names the
// needle.
func wantShape(t *testing.T, got []Finding, needle string) {
	t.Helper()
	for _, f := range got {
		if f.Path == forkReleasePath && f.Rule == ruleWorkflowShape && strings.Contains(f.Detail, needle) {
			return
		}
	}
	t.Fatalf("no workflow-shape finding names %q; findings:\n%s", needle, details(got))
}

// nthIndex returns the byte offset of the nth (1-based) occurrence of sub.
func nthIndex(t *testing.T, src, sub string, nth int) int {
	t.Helper()
	at := 0
	for i := 1; ; i++ {
		j := strings.Index(src[at:], sub)
		if j < 0 {
			t.Fatalf("occurrence %d of %q not found", nth, sub)
		}
		if i == nth {
			return at + j
		}
		at += j + len(sub)
	}
}

// replaceNth replaces the nth occurrence of from, after asserting that the text
// occurs exactly total times, so a mutation can never silently miss.
func replaceNth(t *testing.T, src, from, to string, nth, total int) string {
	t.Helper()
	if n := strings.Count(src, from); n != total {
		t.Fatalf("%q occurs %d times, want %d", from, n, total)
	}
	i := nthIndex(t, src, from, nth)
	return src[:i] + to + src[i+len(from):]
}

// replaceOnce replaces text that occurs exactly once.
func replaceOnce(t *testing.T, src, from, to string) string {
	t.Helper()
	return replaceNth(t, src, from, to, 1, 1)
}

// stepSpan returns the byte range of the nth step with the given name.
func stepSpan(t *testing.T, src, name string, nth int) (int, int) {
	t.Helper()
	marker := "      - name: " + name + "\n"
	start := nthIndex(t, src, marker, nth)
	rest := src[start+len(marker):]
	end := len(rest)
	for _, stop := range []string{"\n      - ", "\n\n"} {
		if i := strings.Index(rest, stop); i >= 0 && i+1 < end {
			end = i + 1
		}
	}
	return start, start + len(marker) + end
}

func TestForkWorkflowHealthy(t *testing.T) {
	src := realWorkflow(t)
	if got := checkForkWorkflow(shapeTree(t, src)); len(got) != 0 {
		t.Fatalf("the real workflow is held by the shape check:\n%s", details(got))
	}
	if got := CheckRelease(shapeTree(t, src)); len(got) != 0 {
		t.Fatalf("the real workflow is held by the release profile:\n%s", details(got))
	}
}

func TestForkWorkflowAbsentPasses(t *testing.T) {
	if got := checkForkWorkflow(cleanTree(t)); len(got) != 0 {
		t.Fatalf("a tree without the workflow is held:\n%s", details(got))
	}
	if got := CheckRelease(cleanTree(t)); len(got) != 0 {
		t.Fatalf("the profile holds a tree without the workflow:\n%s", details(got))
	}
}

// The listed actions are the entries of the action list, which is the only place
// their SHAs are written.
func TestForkWorkflowUsesActionListEntries(t *testing.T) {
	src := realWorkflow(t)
	for _, repo := range []string{"actions/checkout", "actions/setup-go", goreleaserAction} {
		entry, ok := listedAction(repo)
		if !ok || !allowedActions[entry] {
			t.Fatalf("no entry of the action list for %s", repo)
		}
		if !strings.Contains(src, "uses: "+entry) {
			t.Fatalf("the workflow does not use %s", entry)
		}
	}
}

type shapeCase struct {
	name   string
	edit   func(t *testing.T, src string) string
	needle string
}

// swap exchanges two adjacent steps of the first job, named in file order.
func swapSteps(t *testing.T, src, first, second string) string {
	t.Helper()
	a0, a1 := stepSpan(t, src, first, 1)
	b0, b1 := stepSpan(t, src, second, 1)
	if a1 != b0 {
		t.Fatalf("steps %q and %q are not adjacent", first, second)
	}
	return src[:a0] + src[b0:b1] + src[a0:a1] + src[b1:]
}

func rep(from, to string) func(*testing.T, string) string {
	return func(t *testing.T, src string) string { return replaceOnce(t, src, from, to) }
}

func repN(from, to string, nth, total int) func(*testing.T, string) string {
	return func(t *testing.T, src string) string { return replaceNth(t, src, from, to, nth, total) }
}

const (
	guardRun = "        run: '\"$RUNNER_TEMP/fork-release\" guard --in-run'\n"
	notesRun = "        run: '\"$RUNNER_TEMP/fork-release\" notes --in-run'\n"
	buildRun = "        run: go build -o \"$RUNNER_TEMP/fork-release\" ./scripts/fork-release\n"
	tokenEnv = "        env:\n          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n"
)

func TestForkWorkflowShapeTopLevel(t *testing.T) {
	cases := []shapeCase{
		{"another name", rep("name: Fork Release\n", "name: Release\n"), "name"},
		{"an extra trigger", rep("    tags: ['v*-ezop.*']\n", "    tags: ['v*-ezop.*']\n  workflow_dispatch:\n"), "trigger"},
		{"another tag pattern", rep("['v*-ezop.*']", "['v*']"), "trigger"},
		{"a second tag pattern", rep("['v*-ezop.*']", "['v*-ezop.*', 'v*']"), "trigger"},
		{"a branch trigger", rep("    tags: ['v*-ezop.*']\n", "    branches: [ezop]\n"), "trigger"},
		{"an extra top-level key", rep("jobs:\n", "defaults:\n  run:\n    shell: bash\n\njobs:\n"), "top-level keys"},
		{"top-level write", rep("permissions:\n  contents: read\n\nenv:", "permissions:\n  contents: write\n\nenv:"), "top-level permissions"},
		{"top-level read-all", rep("permissions:\n  contents: read\n\nenv:", "permissions: read-all\n\nenv:"), "top-level permissions"},
		{"a second top-level scope", rep("permissions:\n  contents: read\n\nenv:", "permissions:\n  contents: read\n  actions: read\n\nenv:"), "top-level permissions"},
		{"no top-level permissions", rep("permissions:\n  contents: read\n\nenv:", "env:"), "top-level permissions"},
		{"no pin", rep("env:\n  GORELEASER_PIN: v2.17.1\n\nconcurrency:", "concurrency:"), "top-level env"},
		{"a pin without v", rep("GORELEASER_PIN: v2.17.1\n", "GORELEASER_PIN: 2.17.1\n"), "top-level env"},
		{"a two-part pin", rep("GORELEASER_PIN: v2.17.1\n", "GORELEASER_PIN: v2.17\n"), "top-level env"},
		{"a latest pin", rep("GORELEASER_PIN: v2.17.1\n", "GORELEASER_PIN: latest\n"), "top-level env"},
		{"a second env key", rep("GORELEASER_PIN: v2.17.1\n", "GORELEASER_PIN: v2.17.1\n  OTHER: x\n"), "top-level env"},
		{"no concurrency", rep("concurrency:\n  group: ${{ github.workflow }}-${{ github.ref }}\n  cancel-in-progress: false\n\n", ""), "concurrency"},
		{"another group", rep("group: ${{ github.workflow }}-${{ github.ref }}", "group: release"), "concurrency"},
		{"cancel in progress", rep("cancel-in-progress: false", "cancel-in-progress: true"), "concurrency"},
		{"a third job", func(t *testing.T, src string) string {
			return src + "\n  extra:\n    runs-on: ubuntu-24.04\n    steps:\n      - run: echo hi\n"
		}, "jobs"},
		{"no stamp job", func(t *testing.T, src string) string {
			return src[:nthIndex(t, src, "\n  stamp:\n", 1)] + "\n"
		}, "jobs"},
		{"no release job", func(t *testing.T, src string) string {
			return replaceOnce(t, src, "\n  release:\n", "\n  build:\n")
		}, "jobs"},
	}
	runShapeCases(t, cases)
}

func TestForkWorkflowShapeJobs(t *testing.T) {
	const stampPerms = "    permissions:\n      contents: read\n    steps:\n"
	cases := []shapeCase{
		{"release on ubuntu-latest", repN("runs-on: ubuntu-24.04", "runs-on: ubuntu-latest", 1, 2), "runs-on"},
		{"stamp on another label", repN("runs-on: ubuntu-24.04", "runs-on: ubuntu-22.04", 2, 2), "runs-on"},
		{"release on a matrix label", repN("runs-on: ubuntu-24.04", "runs-on: ${{ matrix.os }}", 1, 2), "runs-on"},
		{"an extra release key", rep("    permissions:\n      contents: write\n", "    timeout-minutes: 30\n    permissions:\n      contents: write\n"), "job release keys"},
		{"an extra stamp key", rep(stampPerms, "    continue-on-error: true\n"+stampPerms), "job stamp keys"},
		{"another release condition", rep("github.repository == 'Ezopek/proton-cli'", "github.repository == 'roman-16/proton-cli'"), "job release condition"},
		{"no release condition", rep("    if: github.repository == 'Ezopek/proton-cli'\n", ""), "job release"},
		{"no needs", rep("    needs: release\n", ""), "job stamp needs"},
		{"another needs", rep("    needs: release\n", "    needs: other\n"), "job stamp needs"},
		{"a needs list", rep("    needs: release\n", "    needs: [release]\n"), "job stamp needs"},
		{"release with read", rep("      contents: write\n", "      contents: read\n"), "job release permissions"},
		{"release with a second scope", rep("      contents: write\n", "      contents: write\n      packages: write\n"), "job release permissions"},
		{"release without permissions", rep("    permissions:\n      contents: write\n", ""), "job release permissions"},
		{"stamp with write", rep(stampPerms, "    permissions:\n      contents: write\n    steps:\n"), "job stamp permissions"},
		{"stamp with a second scope", rep(stampPerms, "    permissions:\n      contents: read\n      actions: read\n    steps:\n"), "job stamp permissions"},
		{"stamp without permissions", rep(stampPerms, "    steps:\n"), "job stamp permissions"},
	}
	runShapeCases(t, cases)
}

func TestForkWorkflowShapeSteps(t *testing.T) {
	cases := []shapeCase{
		// Count, order and duplication.
		{"a release step missing", func(t *testing.T, src string) string {
			a, b := stepSpan(t, src, "Write the release notes", 1)
			return src[:a] + src[b:]
		}, "job release has 8 steps"},
		{"a stamp step missing", func(t *testing.T, src string) string {
			a, b := stepSpan(t, src, "Build the release helper", 2)
			return src[:a] + src[b:]
		}, "job stamp has 3 steps"},
		{"an extra release step", rep("      - name: Write the release notes\n", "      - run: echo hi\n      - name: Write the release notes\n"), "job release has 10 steps"},
		{"an extra stamp step", func(t *testing.T, src string) string { return src + "      - run: echo hi\n" }, "job stamp has 5 steps"},
		{"two steps reordered", func(t *testing.T, src string) string {
			return swapSteps(t, src, "Guard the tag and the tree", "Write the release notes")
		}, "job release step 4"},
		{"the recheck before the notes", func(t *testing.T, src string) string {
			return swapSteps(t, src, "Write the release notes", "Re-read the record and the release")
		}, "job release step 5"},
		{"a duplicated step", func(t *testing.T, src string) string {
			a, b := stepSpan(t, src, "Guard the tag and the tree", 1)
			return src[:b] + src[a:b] + src[b:]
		}, "job release has 10 steps"},
		{"a step moved to the other job", func(t *testing.T, src string) string {
			a, b := stepSpan(t, src, "Check the published assets", 1)
			return strings.Replace(src[:a]+src[b:], "      - name: Run the uploaded binary\n", src[a:b]+"      - name: Run the uploaded binary\n", 1)
		}, "job release has 8 steps"},

		// uses.
		{"another checkout SHA", repN("actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", "actions/checkout@"+shaUnlisted, 1, 2), "step 1 uses"},
		{"checkout where setup-go belongs", repN("actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e", "actions/checkout@"+shaCheckout, 1, 2), "step 2 uses"},
		{"another stamp action", repN("actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e", "actions/checkout@"+shaCheckout, 2, 2), "job stamp step 2 uses"},
		{"another GoReleaser SHA", rep("goreleaser/goreleaser-action@"+shaGoreleas, "goreleaser/goreleaser-action@"+shaUnlisted), "step 7 uses"},
		{"a run in place of a uses", rep("        uses: goreleaser/goreleaser-action@"+shaGoreleas+" # v7.2.3\n", "        run: echo hi\n"), "step 7"},

		// run.
		{"a changed build", repN("./scripts/fork-release\n", "./scripts/fork-release ./x\n", 1, 2), "step 3 run"},
		{"a changed stamp build", repN("go build -o", "go build -v -o", 2, 2), "job stamp step 3 run"},
		{"a changed guard", rep("guard --in-run'", "guard --in-run --x'"), "step 4 run"},
		{"a changed notes", rep("notes --in-run'", "notes'"), "step 5 run"},
		{"a changed recheck", rep("guard --recheck'", "guard --in-run'"), "step 6 run"},
		{"a changed assets", rep("assets --in-run'", "assets'"), "step 8 run"},
		{"a changed summary", rep("echo \"GoReleaser: $GORELEASER_PIN\"", "echo \"GoReleaser: latest\""), "step 9 run"},
		{"a changed stamp", rep("stamp --in-run'", "stamp'"), "job stamp step 4 run"},
		{"an expression in a run body", rep("guard --in-run'", "guard --in-run ${{ github.ref_name }}'"), "step 4 run"},

		// with.
		{"a changed go-version-file", repN("go-version-file: go.mod", "go-version-file: go.sum", 1, 2), "step 2 with"},
		{"a changed stamp go-version-file", repN("go-version-file: go.mod", "go-version-file: go.sum", 2, 2), "job stamp step 2 with"},
		{"a shallow release checkout", rep("fetch-depth: 0", "fetch-depth: 1"), "step 1 with"},
		{"no fetch-depth", rep("          fetch-depth: 0\n", ""), "step 1 with"},
		{"an extra with value", repN("          persist-credentials: false\n", "          persist-credentials: false\n          token: x\n", 1, 2), "step 1 with"},
		{"persist-credentials true", repN("persist-credentials: false", "persist-credentials: true", 1, 2), "step 1 with"},
		{"persist-credentials true in stamp", repN("persist-credentials: false", "persist-credentials: true", 2, 2), "job stamp step 1 with"},
		{"no persist-credentials", repN("          persist-credentials: false\n", "", 1, 2), "step 1 with"},
		{"no persist-credentials in stamp", repN("          persist-credentials: false\n", "", 2, 2), "job stamp step 1 with"},
		{"a with on a run step", repN(buildRunStep(), buildRunStep()+"        with:\n          x: y\n", 1, 2), "step 3 with"},

		// env.
		{"no token on the guard", rep(guardRun+tokenEnv, guardRun), "step 4 env"},
		{"another token on the guard", rep(guardRun+tokenEnv, guardRun+"        env:\n          GITHUB_TOKEN: ${{ secrets.OTHER }}\n"), "step 4 env"},
		{"a second value on the guard", rep(guardRun+tokenEnv, guardRun+tokenEnv+"          X: y\n"), "step 4 env"},
		{"a token on the notes", rep(notesRun, notesRun+tokenEnv), "step 5 env"},
		{"no token on the assets", rep("assets --in-run'\n"+tokenEnv, "assets --in-run'\n"), "step 8 env"},
		{"no token on the stamp", rep("stamp --in-run'\n"+tokenEnv, "stamp --in-run'\n"), "job stamp step 4 env"},
		{"an env on the build", repN(buildRunStep(), buildRunStep()+tokenEnv, 1, 2), "step 3 env"},
	}
	for _, key := range []string{"if: always()", "continue-on-error: true", "shell: bash", "working-directory: x", "id: notes", "timeout-minutes: 5"} {
		cases = append(cases, shapeCase{"the step key " + key, rep(notesRun, notesRun+"        "+key+"\n"), "step 5 keys"})
	}
	runShapeCases(t, cases)
}

// buildRunStep is the first build step of the real workflow, up to its run line.
func buildRunStep() string {
	return "      - name: Build the release helper\n" + buildRun
}

func TestForkWorkflowShapeGoreleaserStep(t *testing.T) {
	const args = "args: release --clean --release-notes ${{ runner.temp }}/fork-release-notes.md\n"
	cases := []shapeCase{
		{"skip validate", rep("args: release --clean", "args: release --clean --skip=validate"), "--skip"},
		{"skip in a spaced form", rep("args: release --clean", "args: release --skip validate --clean"), "--skip"},
		{"no release notes", rep(" --release-notes ${{ runner.temp }}/fork-release-notes.md", ""), "--release-notes"},
		{"another notes path", rep("${{ runner.temp }}/fork-release-notes.md", "notes.md"), "step 7 with"},
		{"a literal version", rep("version: ${{ env.GORELEASER_PIN }}", "version: v2.17.1"), "GoReleaser version"},
		{"latest version", rep("version: ${{ env.GORELEASER_PIN }}", "version: latest"), "GoReleaser version"},
		{"no version", rep("          version: ${{ env.GORELEASER_PIN }}\n", ""), "GoReleaser version"},
		{"no current tag", rep("          GORELEASER_CURRENT_TAG: ${{ github.ref_name }}\n", ""), "GORELEASER_CURRENT_TAG"},
		{"another current tag", rep("GORELEASER_CURRENT_TAG: ${{ github.ref_name }}", "GORELEASER_CURRENT_TAG: ${{ github.ref }}"), "GORELEASER_CURRENT_TAG"},
		{"a literal current tag", rep("GORELEASER_CURRENT_TAG: ${{ github.ref_name }}", "GORELEASER_CURRENT_TAG: v5.1.0-ezop.1"), "GORELEASER_CURRENT_TAG"},
		{"a config argument", rep(args, args[:len(args)-1]+" --config other.yaml\n"), "step 7 with"},
		{"a second GoReleaser step", func(t *testing.T, src string) string {
			a, b := stepSpan(t, src, "Publish the Fork Release", 1)
			return src[:b] + src[a:b] + src[b:]
		}, "exactly one GoReleaser step"},
		{"a GoReleaser step in the stamp job", func(t *testing.T, src string) string {
			a, b := stepSpan(t, src, "Publish the Fork Release", 1)
			return src + src[a:b]
		}, "exactly one GoReleaser step"},
		{"no GoReleaser step", func(t *testing.T, src string) string {
			a, b := stepSpan(t, src, "Publish the Fork Release", 1)
			return src[:a] + src[b:]
		}, "exactly one GoReleaser step"},
	}
	runShapeCases(t, cases)
}

func TestForkWorkflowShapeCache(t *testing.T) {
	cases := []shapeCase{
		{"release cache true", repN("cache: false", "cache: true", 1, 2), "setup-go cache"},
		{"stamp cache true", repN("cache: false", "cache: true", 2, 2), "setup-go cache"},
		{"release cache absent", repN("          cache: false\n", "", 1, 2), "setup-go cache"},
		{"stamp cache absent", repN("          cache: false\n", "", 2, 2), "setup-go cache"},
		{"a cache string", repN("cache: false", "cache: 'false'", 1, 2), "setup-go cache"},
		{"a cache action", rep("      - name: Write the release notes\n", "      - uses: actions/cache@"+shaCheckout+"\n      - name: Write the release notes\n"), "cache action"},
		{"a cache action in stamp", func(t *testing.T, src string) string {
			return src + "      - uses: actions/cache@" + shaCheckout + "\n"
		}, "cache action"},
	}
	runShapeCases(t, cases)
}

// The shared enabled-workflow rules keep holding what they hold, beside the shape.
func TestForkWorkflowSharedRulesStillHold(t *testing.T) {
	src := realWorkflow(t)
	t.Run("a secret other than GITHUB_TOKEN", func(t *testing.T) {
		got := CheckRelease(shapeTree(t, replaceNth(t, src, "${{ secrets.GITHUB_TOKEN }}", "${{ secrets.OTHER }}", 1, 5)))
		wantHeld(t, got, ruleWorkflowSecrets)
		wantShape(t, got, "step 4 env")
	})
	t.Run("an expression in a run body", func(t *testing.T) {
		got := CheckRelease(shapeTree(t, replaceOnce(t, src, "guard --in-run'", "guard --in-run ${{ github.ref_name }}'")))
		wantHeld(t, got, ruleWorkflowExpression)
		wantShape(t, got, "step 4 run")
	})
	t.Run("a git push", func(t *testing.T) {
		got := CheckRelease(shapeTree(t, replaceOnce(t, src, "      - name: Write the release notes\n", "      - run: git push\n      - name: Write the release notes\n")))
		wantHeld(t, got, ruleWorkflowCommand, "git push")
		wantShape(t, got, "job release has 10 steps")
	})
	t.Run("a tag-reachable second trigger", func(t *testing.T) {
		got := CheckRelease(shapeTree(t, replaceOnce(t, src, "    tags: ['v*-ezop.*']\n", "    tags: ['v*-ezop.*']\n  pull_request:\n")))
		wantHeld(t, got, ruleWorkflowTrigger)
		wantShape(t, got, "trigger")
	})
}

// Unparsable and null documents are the shared rules' to report, and the shape
// check adds nothing to them.
func TestForkWorkflowShapeOnUnreadableFiles(t *testing.T) {
	for name, src := range map[string]string{"null": "null\n", "tilde": "~\n", "not yaml": "a: [\n"} {
		t.Run(name, func(t *testing.T) {
			if got := checkForkWorkflow(shapeTree(t, src)); len(got) != 0 {
				t.Fatalf("the shape check reports on an unreadable file:\n%s", details(got))
			}
		})
	}
	t.Run("a document that is a list", func(t *testing.T) {
		wantShape(t, checkForkWorkflow(shapeTree(t, "- a\n")), "mapping")
	})
}

func runShapeCases(t *testing.T, cases []shapeCase) {
	t.Helper()
	real := realWorkflow(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CheckRelease(shapeTree(t, real)); len(got) != 0 {
				t.Fatalf("control: the real workflow is held:\n%s", details(got))
			}
			src := c.edit(t, real)
			if src == real {
				t.Fatal("the edit changed nothing")
			}
			got := CheckRelease(shapeTree(t, src))
			wantShape(t, got, c.needle)
		})
	}
}

// ---- the real tree ----

// The release profile runs over the real repository tree and finds nothing. No
// prerequisite may be absent, and the test never skips: it is the first signal
// that a drift would make the first guard refuse.
func TestActualTreeReleaseProfile(t *testing.T) {
	root := repoRoot()
	for _, p := range []string{forkReleasePath, ".github/workflows/ci.yml", ".goreleaser.yaml"} {
		info, err := fs.Stat(root, p)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("prerequisite %s is not a regular file in the real tree: %v", p, err)
		}
	}
	for _, p := range []string{
		".github/workflows/release.yml", ".github/workflows/openapi-sync.yml", ".github/workflows/stats.yml", ".github/workflows/web.yml",
		// GoReleaser reads the first of these that exists, ahead of .goreleaser.yaml,
		// and the release profile checks only .goreleaser.yaml.
		".config/goreleaser.yml", ".config/goreleaser.yaml", ".goreleaser.yml",
	} {
		if _, err := fs.Stat(root, p); err == nil {
			t.Fatalf("%s must be absent from the real tree", p)
		}
	}
	if got := CheckRelease(root); len(got) != 0 {
		t.Fatalf("the release profile holds the real tree:\n%s", details(got))
	}
}

// The pin of the real workflow, go.mod and devbox.lock agree, so the first
// guard of a real run would not refuse on the toolchain.
func TestActualTreeToolchainConsistency(t *testing.T) {
	root := repoRoot()
	workflow, err := readRegular(root, forkReleasePath)
	if err != nil {
		t.Fatalf("the real workflow cannot be read: %v", err)
	}
	pin, err := workflowPin(workflow)
	if err != nil {
		t.Fatalf("the pin of the real workflow cannot be read: %v", err)
	}
	if !goreleaserPinPattern.MatchString(pin) {
		t.Fatalf("the pin is not vX.Y.Z: %q", pin)
	}
	goMod, err := readRegular(root, "go.mod")
	if err != nil {
		t.Fatalf("go.mod cannot be read: %v", err)
	}
	lock, err := readRegular(root, "devbox.lock")
	if err != nil {
		t.Fatalf("devbox.lock cannot be read: %v", err)
	}
	if err := checkToolchain(goMod, lock, pin); err != nil {
		t.Fatalf("the toolchain check refuses the real tree: %v", err)
	}
}

// The release profile now holds a missing or malformed pin before the toolchain
// stage of guard --pre-push is reached, so the pin reader's own refusals are
// kept under test here, on the tree of a commit, with the check name they carry.
func TestTreePinRefusalsStayUnderTheToolchainCheck(t *testing.T) {
	for name, pinLine := range map[string]string{
		"no pin":              "",
		"a pin without v":     "env:\n  GORELEASER_PIN: 2.17.1\n\n",
		"a pin only in a job": "",
	} {
		t.Run(name, func(t *testing.T) {
			w := newPPWorld(t)
			src := ppWorkflow(pinLine)
			if name == "a pin only in a job" {
				src = replaceNth(t, src, "    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    env:\n      GORELEASER_PIN: "+goodPin+"\n", 1, 2)
			}
			w.git.tree[forkReleasePath] = fstestFile(src)
			_, err := treePin(w.git, targetSHA)
			var r refusal
			if !errors.As(err, &r) || r.check != "toolchain" {
				t.Fatalf("want a toolchain refusal, got %v", err)
			}
		})
	}
	t.Run("control: the real pin is read", func(t *testing.T) {
		w := newPPWorld(t)
		if pin, err := treePin(w.git, targetSHA); err != nil || pin != goodPin {
			t.Fatalf("pin %q, err %v", pin, err)
		}
	})
}
