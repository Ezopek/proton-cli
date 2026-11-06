package main

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	shaCheckout  = "3d3c42e5aac5ba805825da76410c181273ba90b1"
	shaSetupGo   = "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e"
	shaGoreleas  = "f06c13b6b1a9625abc9e6e439d9c05a8f2190e94"
	shaUnlisted  = "0123456789abcdef0123456789abcdef01234567"
	forkReleaseY = `name: Fork Release
on:
  push:
    tags: ['v*-ezop.*']
permissions:
  contents: read
jobs:
  release:
    runs-on: ubuntu-24.04
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@` + shaCheckout + `
        with:
          persist-credentials: false
      - uses: goreleaser/goreleaser-action@` + shaGoreleas + `
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
  stamp:
    needs: release
    runs-on: ubuntu-24.04
    permissions:
      contents: read
    steps:
      - run: echo done
`
)

var releaseRules = workflowRules{allowForkRelease: true}

// wfTree builds a tree from path to content. A name without a slash is a
// workflow file.
func wfTree(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, content := range files {
		if !strings.Contains(name, "/") && !strings.HasPrefix(name, ".") {
			name = ".github/workflows/" + name
		}
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

// job returns a workflow whose single job has the given extras, a permitted
// trigger and a read-only top-level permissions block.
func job(extra string) string {
	return "on:\n  pull_request:\nname: x\npermissions:\n  contents: read\njobs:\n  build:\n    runs-on: ubuntu-24.04\n" + extra
}

func steps(items ...string) string {
	return "    steps:\n      - " + strings.Join(items, "\n      - ") + "\n"
}

func okAction(name string) string {
	return "uses: " + name + "@" + shaCheckout
}

func TestCheckWorkflowsFileSet(t *testing.T) {
	ci := string(fixture(t, "ci.yml"))
	passes := map[string]map[string]string{
		"ci only":                {"ci.yml": ci},
		"fork-release only":      {"fork-release.yml": forkReleaseY},
		"both":                   {"ci.yml": ci, "fork-release.yml": forkReleaseY},
		"issue templates":        {".github/ISSUE_TEMPLATE/bug.md": "x", ".github/ISSUE_TEMPLATE/sub/config.yml": "x"},
		"no workflows directory": {".github/ISSUE_TEMPLATE/bug.md": "x"},
		"nothing under .github":  {"docs/README.md": "x"},
	}
	for name, files := range passes {
		t.Run("pass "+name, func(t *testing.T) {
			if fs := checkWorkflows(wfTree(files), releaseRules); len(fs) != 0 {
				t.Fatalf("findings:\n%s", details(fs))
			}
		})
	}

	held := map[string]struct {
		files map[string]string
		rule  string
		path  string
	}{
		"restored release.yml":  {map[string]string{"release.yml": "x: 1"}, "workflow-set", ".github/workflows/release.yml"},
		"restored openapi-sync": {map[string]string{"openapi-sync.yml": "x: 1"}, "workflow-set", "openapi-sync.yml"},
		"restored stats":        {map[string]string{"stats.yml": "x: 1"}, "workflow-set", "stats.yml"},
		"restored web":          {map[string]string{"web.yml": "x: 1"}, "workflow-set", "web.yml"},
		"new.yml":               {map[string]string{"new.yml": "x: 1"}, "workflow-set", "new.yml"},
		"ci.yaml spelling":      {map[string]string{"ci.yaml": "x: 1"}, "workflow-set", "ci.yaml"},
		"nested workflow":       {map[string]string{".github/workflows/sub/ci.yml": "x: 1"}, "workflow-set", "sub/ci.yml"},
		"dependabot.yml":        {map[string]string{".github/dependabot.yml": "version: 2"}, "github-file", "dependabot.yml"},
		"local action":          {map[string]string{".github/actions/x/action.yml": "name: x"}, "github-file", "actions/x"},
		"CODEOWNERS":            {map[string]string{".github/CODEOWNERS": "x"}, "github-file", "CODEOWNERS"},
		".github is a file":     {map[string]string{".github": "x"}, "github-file", ".github"},
	}
	for name, c := range held {
		t.Run("held "+name, func(t *testing.T) {
			wantHeld(t, checkWorkflows(wfTree(c.files), releaseRules), c.rule, c.path)
		})
	}

	t.Run("held symlinks anywhere under .github", func(t *testing.T) {
		for _, p := range []string{".github/workflows/ci.yml", ".github/ISSUE_TEMPLATE/bug.md", ".github/workflows/fork-release.yml", ".github/other"} {
			t.Run(p, func(t *testing.T) {
				fsys := fstest.MapFS{p: {Data: []byte("target"), Mode: fs.ModeSymlink}}
				wantHeld(t, checkWorkflows(fsys, releaseRules), "github-non-regular", p)
			})
		}
	})

	t.Run("a directory named like a workflow is held", func(t *testing.T) {
		fsys := fstest.MapFS{".github/workflows/ci.yml/x": {Data: []byte("a")}}
		wantHeld(t, checkWorkflows(fsys, releaseRules), "workflow-set")
	})

	t.Run("fork-release.yml is held when the exceptions are off", func(t *testing.T) {
		fsys := wfTree(map[string]string{"fork-release.yml": forkReleaseY})
		wantHeld(t, checkWorkflows(fsys, workflowRules{}), "workflow-set", "fork-release.yml")
	})

	t.Run("unparseable and unreadable workflows are held", func(t *testing.T) {
		wantHeld(t, checkWorkflows(wfTree(map[string]string{"ci.yml": "a: [1, 2\n"}), releaseRules), ruleYAMLParse, "ci.yml")
		wantHeld(t, checkWorkflows(wfTree(map[string]string{"ci.yml": ""}), releaseRules), ruleYAMLParse, "ci.yml")
		wantHeld(t, checkWorkflows(wfTree(map[string]string{"ci.yml": "- a\n"}), releaseRules), "workflow-shape", "ci.yml")
		wantHeld(t, checkWorkflows(wfTree(map[string]string{"ci.yml": "on: pull_request\npermissions: read-all\n"}), releaseRules), "workflow-shape", "jobs")
	})
}

func TestCheckWorkflowsPermissions(t *testing.T) {
	ci := string(fixture(t, "ci.yml"))
	t.Run("ci fixture passes", func(t *testing.T) {
		if fs := checkWorkflows(wfTree(map[string]string{"ci.yml": ci}), releaseRules); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
	t.Run("read-all and an empty block pass", func(t *testing.T) {
		for _, p := range []string{"permissions: read-all", "permissions: {}"} {
			src := strings.Replace(job(steps(okAction("actions/checkout"))), "permissions:\n  contents: read", p, 1)
			if fs := checkWorkflows(wfTree(map[string]string{"ci.yml": src}), releaseRules); len(fs) != 0 {
				t.Fatalf("%s:\n%s", p, details(fs))
			}
		}
	})

	held := map[string]string{
		"no top-level permissions": strings.Replace(job(steps(okAction("actions/checkout"))), "permissions:\n  contents: read\n", "", 1),
		"top-level write-all":      strings.Replace(job(steps(okAction("actions/checkout"))), "contents: read", "contents: read\n  issues: write", 1),
		"top-level string write":   strings.Replace(job(steps(okAction("actions/checkout"))), "permissions:\n  contents: read", "permissions: write-all", 1),
		"second job with contents: write": job(steps(okAction("actions/checkout"))) +
			"  other:\n    runs-on: ubuntu-24.04\n    permissions:\n      contents: write\n    steps:\n      - run: echo hi\n",
		"job write-all":       job("    permissions: write-all\n" + steps(okAction("actions/checkout"))),
		"job other write":     job("    permissions:\n      pull-requests: write\n" + steps(okAction("actions/checkout"))),
		"job unknown value":   job("    permissions:\n      contents: admin\n" + steps(okAction("actions/checkout"))),
		"job permissions odd": job("    permissions: 3\n" + steps(okAction("actions/checkout"))),
	}
	for name, src := range held {
		t.Run("held "+name, func(t *testing.T) {
			wantHeld(t, checkWorkflows(wfTree(map[string]string{"ci.yml": src}), releaseRules), "workflow-permissions")
		})
	}

	t.Run("a job named release in ci.yml may not write", func(t *testing.T) {
		src := strings.Replace(job("    permissions:\n      contents: write\n"+steps("run: echo hi")), "build:", "release:", 1)
		wantHeld(t, checkWorkflows(wfTree(map[string]string{"ci.yml": src}), releaseRules), "workflow-permissions")
	})

	t.Run("fork-release.yml", func(t *testing.T) {
		with := func(from, to string) map[string]string {
			if strings.Count(forkReleaseY, from) != 1 {
				t.Fatalf("%q appears %d times", from, strings.Count(forkReleaseY, from))
			}
			return map[string]string{"fork-release.yml": strings.Replace(forkReleaseY, from, to, 1)}
		}
		t.Run("release job may hold contents: write", func(t *testing.T) {
			if fs := checkWorkflows(wfTree(map[string]string{"fork-release.yml": forkReleaseY}), releaseRules); len(fs) != 0 {
				t.Fatalf("findings:\n%s", details(fs))
			}
		})
		t.Run("stamp job may not write", func(t *testing.T) {
			f := with("    permissions:\n      contents: read\n    steps:\n      - run: echo done", "    permissions:\n      contents: write\n    steps:\n      - run: echo done")
			wantHeld(t, checkWorkflows(wfTree(f), releaseRules), "workflow-permissions", "stamp")
		})
		t.Run("release job may write nothing else", func(t *testing.T) {
			f := with("      contents: write\n", "      contents: write\n      packages: write\n")
			wantHeld(t, checkWorkflows(wfTree(f), releaseRules), "workflow-permissions", "packages")
		})
		t.Run("top level may not write", func(t *testing.T) {
			f := with("permissions:\n  contents: read\njobs:", "permissions:\n  contents: write\njobs:")
			wantHeld(t, checkWorkflows(wfTree(f), releaseRules), "workflow-permissions")
		})
		t.Run("no top-level permissions", func(t *testing.T) {
			f := with("permissions:\n  contents: read\njobs:", "jobs:")
			wantHeld(t, checkWorkflows(wfTree(f), releaseRules), "workflow-permissions")
		})
	})
}

func TestCheckWorkflowsActions(t *testing.T) {
	check := func(t *testing.T, src string) []Finding {
		return checkWorkflows(wfTree(map[string]string{"ci.yml": src}), releaseRules)
	}
	t.Run("listed actions pass", func(t *testing.T) {
		src := job(steps("uses: actions/checkout@"+shaCheckout, "uses: actions/setup-go@"+shaSetupGo))
		if fs := check(t, src); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
	held := map[string]string{
		"tag ref":                    steps("uses: actions/checkout@v7"),
		"branch ref":                 steps("uses: actions/checkout@main"),
		"no ref":                     steps("uses: actions/checkout"),
		"local path":                 steps("uses: ./.github/actions/x"),
		"docker":                     steps("uses: docker://alpine:3"),
		"sha not in the list":        steps("uses: actions/checkout@" + shaUnlisted),
		"sha of another action":      steps("uses: actions/checkout@" + shaSetupGo),
		"unlisted action":            steps("uses: someone/else@" + shaCheckout),
		"short sha":                  steps("uses: actions/checkout@3d3c42e"),
		"goreleaser-action in ci":    steps("uses: goreleaser/goreleaser-action@" + shaGoreleas),
		"subpath of a listed action": steps("uses: actions/checkout/sub@" + shaCheckout),
	}
	for name, st := range held {
		t.Run("held "+name, func(t *testing.T) {
			wantHeld(t, check(t, job(st)), "workflow-action", "uses")
		})
	}
	t.Run("held reusable workflow call", func(t *testing.T) {
		src := "on:\n  pull_request:\npermissions:\n  contents: read\njobs:\n  call:\n    uses: ./.github/workflows/x.yml\n"
		wantHeld(t, check(t, src), "workflow-action")
	})
	t.Run("goreleaser-action passes in fork-release.yml", func(t *testing.T) {
		if fs := checkWorkflows(wfTree(map[string]string{"fork-release.yml": forkReleaseY}), releaseRules); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
}

func TestCheckWorkflowsTriggers(t *testing.T) {
	check := func(on string) []Finding {
		src := on + "permissions:\n  contents: read\njobs:\n  build:\n    runs-on: ubuntu-24.04\n    steps:\n      - run: echo hi\n"
		return checkWorkflows(wfTree(map[string]string{"ci.yml": src}), releaseRules)
	}
	passes := map[string]string{
		"pull_request":              "on: pull_request\n",
		"list":                      "on: [pull_request, workflow_dispatch]\n",
		"push branches":             "on:\n  push:\n    branches: [main]\n",
		"push branches-ignore":      "on:\n  push:\n    branches-ignore: [x]\n",
		"push branches with paths":  "on:\n  push:\n    branches: [main]\n    paths: ['a/**']\n",
		"quoted on key":             "\"on\":\n  push:\n    branches: [main]\n",
		"schedule":                  "on:\n  schedule:\n    - cron: '0 0 * * 1'\n",
		"pull_request with filters": "on:\n  pull_request:\n    branches: [main]\n",
	}
	for name, on := range passes {
		t.Run("pass "+name, func(t *testing.T) {
			if fs := check(on); len(fs) != 0 {
				t.Fatalf("findings:\n%s", details(fs))
			}
		})
	}
	held := map[string]string{
		"push scalar":                 "on: push\n",
		"push in a list":              "on: [push]\n",
		"push null":                   "on:\n  push:\n",
		"push empty mapping":          "on:\n  push: {}\n",
		"push tags":                   "on:\n  push:\n    tags: ['v*']\n",
		"push tags-ignore":            "on:\n  push:\n    tags-ignore: ['v*']\n",
		"push paths only":             "on:\n  push:\n    paths: ['a/**']\n",
		"push paths-ignore only":      "on:\n  push:\n    paths-ignore: ['a/**']\n",
		"push branches and tags":      "on:\n  push:\n    branches: [main]\n    tags: ['v*']\n",
		"push branches and tags-ign":  "on:\n  push:\n    branches: [main]\n    tags-ignore: ['v*']\n",
		"create scalar":               "on: create\n",
		"create in a list":            "on: [pull_request, create]\n",
		"create mapping":              "on:\n  create:\n",
		"quoted on key, push tags":    "\"on\":\n  push:\n    tags: ['v*']\n",
		"push value is a scalar":      "on:\n  push: yes\n",
		"no trigger":                  "name: x\n",
		"on is a number":              "on: 3\n",
		"list with a non-string item": "on: [3]\n",
	}
	for name, on := range held {
		t.Run("held "+name, func(t *testing.T) {
			wantHeld(t, check(on), "workflow-trigger")
		})
	}

	t.Run("fork-release.yml", func(t *testing.T) {
		with := func(on string) []Finding {
			i := strings.Index(forkReleaseY, "permissions:")
			src := "name: Fork Release\n" + on + forkReleaseY[i:]
			return checkWorkflows(wfTree(map[string]string{"fork-release.yml": src}), releaseRules)
		}
		t.Run("exactly the pinned trigger passes", func(t *testing.T) {
			if fs := with("on:\n  push:\n    tags: ['v*-ezop.*']\n"); len(fs) != 0 {
				t.Fatalf("findings:\n%s", details(fs))
			}
		})
		t.Run("quoted on key and double-quoted pattern pass", func(t *testing.T) {
			if fs := with("\"on\":\n  push:\n    tags:\n      - \"v*-ezop.*\"\n"); len(fs) != 0 {
				t.Fatalf("findings:\n%s", details(fs))
			}
		})
		heldOn := map[string]string{
			"wider pattern":       "on:\n  push:\n    tags: ['v*']\n",
			"two patterns":        "on:\n  push:\n    tags: ['v*-ezop.*', 'x']\n",
			"pattern as a scalar": "on:\n  push:\n    tags: 'v*-ezop.*'\n",
			"with branches":       "on:\n  push:\n    tags: ['v*-ezop.*']\n    branches: [main]\n",
			"with tags-ignore":    "on:\n  push:\n    tags: ['v*-ezop.*']\n    tags-ignore: [x]\n",
			"extra event":         "on:\n  push:\n    tags: ['v*-ezop.*']\n  workflow_dispatch:\n",
			"push scalar":         "on: push\n",
			"pull_request":        "on: pull_request\n",
			"branches only":       "on:\n  push:\n    branches: [main]\n",
			"list form":           "on: [push]\n",
		}
		for name, on := range heldOn {
			t.Run("held "+name, func(t *testing.T) {
				wantHeld(t, with(on), "workflow-trigger")
			})
		}
	})
}

func TestCheckWorkflowsSecretsAndScripts(t *testing.T) {
	check := func(extra string) []Finding {
		return checkWorkflows(wfTree(map[string]string{"ci.yml": job(extra)}), releaseRules)
	}
	t.Run("secrets.GITHUB_TOKEN passes", func(t *testing.T) {
		if fs := check(steps("run: echo hi\n        env:\n          T: ${{ secrets.GITHUB_TOKEN }}")); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
	heldSecrets := map[string]string{
		"secrets.X in env":         steps("run: echo hi\n        env:\n          T: ${{ secrets.AUR_KEY }}"),
		"index form":               steps("run: echo hi\n        env:\n          T: ${{ secrets['X'] }}"),
		"toJSON(secrets)":          steps("run: echo hi\n        env:\n          T: ${{ toJSON(secrets) }}"),
		"upper case":               steps("run: echo hi\n        env:\n          T: ${{ SECRETS.X }}"),
		"longer name":              steps("run: echo hi\n        env:\n          T: ${{ secrets.GITHUB_TOKEN_2 }}"),
		"in a with":                steps(okAction("actions/checkout") + "\n        with:\n          token: ${{ secrets.TAP_WINGET_TOKEN }}"),
		"in a job if":              "    if: ${{ secrets.X != '' }}\n" + steps("run: echo hi"),
		"secrets: inherit":         "    secrets: inherit\n" + steps("run: echo hi"),
		"secrets mapping":          "    secrets:\n      X: ${{ secrets.GITHUB_TOKEN }}\n" + steps("run: echo hi"),
		"no dot after secrets":     steps("run: echo hi\n        env:\n          T: ${{ secrets }}"),
		"bare secrets in a string": steps("run: echo hi\n        env:\n          T: x secrets y"),
	}
	for name, extra := range heldSecrets {
		t.Run("held "+name, func(t *testing.T) {
			wantHeld(t, check(extra), "workflow-secrets")
		})
	}

	t.Run("expressions in run", func(t *testing.T) {
		wantHeld(t, check(steps("run: echo ${{ github.ref_name }}")), "workflow-expression", "run")
		wantHeld(t, check(steps("run: |\n          set -e\n          echo ${{github.sha}}")), "workflow-expression")
		if fs := check(steps("run: echo \"$REF\"\n        env:\n          REF: ${{ github.ref_name }}")); len(fs) != 0 {
			t.Fatalf("env expressions must pass:\n%s", details(fs))
		}
	})

	t.Run("publisher jobs", func(t *testing.T) {
		for _, id := range []string{"apt-repo", "apt-deploy", "npm-publish"} {
			t.Run(id, func(t *testing.T) {
				src := job(steps("run: echo hi")) + "  " + id + ":\n    runs-on: ubuntu-24.04\n    steps:\n      - run: echo hi\n"
				wantHeld(t, checkWorkflows(wfTree(map[string]string{"ci.yml": src}), releaseRules), "workflow-job", id)
			})
		}
	})

	t.Run("run is not a string", func(t *testing.T) {
		wantHeld(t, check("    steps:\n      - run: [a]\n"), "workflow-shape")
	})
	t.Run("a step is not a mapping", func(t *testing.T) {
		wantHeld(t, check("    steps:\n      - just-a-string\n"), "workflow-shape")
	})
}

func TestForbiddenCommands(t *testing.T) {
	held := map[string]string{
		"git push":               "git push",
		"git push with args":     "git push origin HEAD:refs/tags/x",
		"git tag":                "git tag v1",
		"git config user.name":   "git config user.name x",
		"git config --global":    "git config --global user.email x@y",
		"git -C . push":          "git -C . push",
		"quoted":                 `"git" 'push'`,
		"after &&":               "true && git push",
		"second line":            "echo ok\ngit push",
		"after a semicolon":      "echo ok; git tag x",
		"after a pipe":           "echo ok | git push",
		"in a subshell":          "(git push)",
		"line continuation":      "git \\\n  push",
		"echoed text":            `echo "git push"`,
		"gh release create":      "gh release create v1",
		"gh release edit":        "gh release edit v1",
		"gh release upload":      "gh release upload v1 f",
		"gh release delete":      "gh release delete v1",
		"gh with a flag":         "gh -R a/b release create v1",
		"gh api without method":  "gh api repos/a/b/releases",
		"gh api POST via -X":     "gh api -X POST repos/a/b",
		"gh api POST via long":   "gh api --method POST repos/a/b",
		"gh api long equals":     "gh api --method=PUT repos/a/b",
		"gh api two methods":     "gh api --method GET -X GET repos/a/b",
		"gh api lower-case get":  "gh api --method get repos/a/b",
		"gh api second call bad": "gh api --method GET a\ngh api b",
		"curl":                   "curl https://example.com",
		"curl path":              "/usr/bin/curl x",
		"wget":                   "wget https://example.com",
		"goreleaser release":     "goreleaser release --clean",
		"goreleaser flag first":  "goreleaser --verbose release",
	}
	for name, body := range held {
		t.Run("held "+name, func(t *testing.T) {
			if got := forbiddenCommands(body); len(got) == 0 {
				t.Fatalf("not held: %q", body)
			}
		})
	}
	passes := map[string]string{
		"git status":          "git status",
		"git diff":            "git diff --exit-code",
		"git log":             "git log --tags",
		"go test":             "go test ./scripts/fork-release/...",
		"echo":                "echo hello world",
		"gh api GET":          "gh api --method GET repos/a/b",
		"gh api -X GET":       "gh api -X GET repos/a/b",
		"gh api equals":       "gh api --method=GET repos/a/b",
		"gh api -XGET":        "gh api -XGET repos/a/b",
		"gh api flags around": "gh api repos/a/b -H 'X-GitHub-Api-Version: 2026-03-10' --method GET",
		"two GET calls":       "gh api --method GET a\ngh api --method GET b",
		"gh run list":         "gh run list",
		"gh release view":     "gh release view v1",
		"goreleaser check":    "goreleaser check",
		"empty":               "",
		"a comment line":      "# curl is not run here",
	}
	for name, body := range passes {
		t.Run("pass "+name, func(t *testing.T) {
			if got := forbiddenCommands(body); len(got) != 0 {
				t.Fatalf("held %q: %v", body, got)
			}
		})
	}
}

func TestCheckWorkflowsCommandsInSteps(t *testing.T) {
	for body, label := range map[string]string{
		"git push":           "git push",
		"curl x":             "curl",
		"gh api repos/a/b":   "gh api",
		"goreleaser release": "goreleaser release",
	} {
		t.Run(body, func(t *testing.T) {
			fsys := wfTree(map[string]string{"ci.yml": job(steps("run: " + body))})
			wantHeld(t, checkWorkflows(fsys, releaseRules), "workflow-command", label)
		})
	}
	t.Run("also held in fork-release.yml", func(t *testing.T) {
		src := strings.Replace(forkReleaseY, "run: echo done", "run: git push", 1)
		wantHeld(t, checkWorkflows(wfTree(map[string]string{"fork-release.yml": src}), releaseRules), "workflow-command")
	})
}

// contains reports whether a label is among the labels forbiddenCommands returned.
func contains(labels []string, label string) bool {
	return strings.Contains("|"+strings.Join(labels, "|")+"|", "|"+label+"|")
}

// The enumerated patterns of the release rules include two near-synonyms of
// listed verbs: removing a release asset (gh release delete-asset, beside delete)
// and setting a git identity inline (git -c user.name=..., beside git config
// user.*). The match stays a conservative match of enumerated patterns, so the
// ordinary forms of the same commands are the controls.
func TestForbiddenCommandsAssetsAndIdentity(t *testing.T) {
	held := map[string]struct{ body, label string }{
		"delete-asset":               {"gh release delete-asset v1 x", "gh release delete-asset"},
		"delete-asset, spaced":       {"gh   release   delete-asset v1 x", "gh release delete-asset"},
		"delete-asset, flag first":   {"gh -R a/b release delete-asset v1 x", "gh release delete-asset"},
		"delete-asset after &&":      {"true && gh release delete-asset v1 x", "gh release delete-asset"},
		"git -c user.name":           {"git -c user.name=x commit -m y", "git -c user.*"},
		"git -c user.email":          {"git -c user.email=x@y commit -m y", "git -c user.*"},
		"git -c quoted value":        {`git -c user.name="A B" commit -m y`, "git -c user.*"},
		"git -c quoted pair":         {`git -c "user.name=A B" commit -m y`, "git -c user.*"},
		"git -c spaced":              {"git   -c   user.email=x@y   commit", "git -c user.*"},
		"git -C then -c":             {"git -C . -c user.name=x commit", "git -c user.*"},
		"git -c after a semicolon":   {"echo ok; git -c user.name=x commit", "git -c user.*"},
		"git -c on a continued line": {"git \\\n  -c user.name=x commit", "git -c user.*"},
		"git -c in a subshell":       {"(git -c user.email=x@y commit)", "git -c user.*"},
		"delete still held":          {"gh release delete v1", "gh release delete"},
		"git config user.name still": {"git config user.name x", "git config user.*"},
		"git push still held":        {"git push", "git push"},
	}
	for name, c := range held {
		t.Run("held "+name, func(t *testing.T) {
			if got := forbiddenCommands(c.body); !contains(got, c.label) {
				t.Fatalf("%q: labels %v do not include %q", c.body, got, c.label)
			}
		})
	}
	passes := map[string]string{
		"gh release view":         "gh release view v1",
		"gh release download":     "gh release download v1",
		"gh release list":         "gh release list",
		"gh api GET":              "gh api --method GET repos/a/b",
		"git status":              "git status",
		"git commit":              "git commit -m msg",
		"git -c core.pager":       "git -c core.pager=cat log",
		"git -c core.editor":      "git -c core.editor=true commit -m msg",
		"git -C . log":            "git -C . log --oneline",
		"user.name only in a msg": "echo user.name=x",
	}
	for name, body := range passes {
		t.Run("pass "+name, func(t *testing.T) {
			if got := forbiddenCommands(body); len(got) != 0 {
				t.Fatalf("held %q: %v", body, got)
			}
		})
	}
	for name, body := range map[string]string{
		"delete-asset": "gh release delete-asset v1 x",
		"git -c user":  "git -c user.name=x commit -m y",
	} {
		t.Run("held inside a workflow step: "+name, func(t *testing.T) {
			fsys := wfTree(map[string]string{"ci.yml": job(steps("run: " + body))})
			wantHeld(t, checkWorkflows(fsys, releaseRules), "workflow-command")
		})
	}
}

// A workflow that decodes to nothing has no permissions, triggers or jobs to
// check. It is held, and never read as a clean file. Nested nulls stay ordinary.
func TestCheckWorkflowsNullDocument(t *testing.T) {
	for _, name := range []string{"ci.yml", "fork-release.yml"} {
		for srcName, src := range map[string]string{
			"null":                   "null\n",
			"tilde":                  "~\n",
			"explicit document null": "--- null\n",
		} {
			t.Run(name+" "+srcName, func(t *testing.T) {
				got := checkWorkflows(wfTree(map[string]string{name: src}), releaseRules)
				path := ".github/workflows/" + name
				if len(got) != 1 || got[0].Path != path || got[0].Rule != ruleYAMLParse || !strings.Contains(got[0].Detail, "null") {
					t.Fatalf("want one %s finding naming null for %s, got:\n%s", ruleYAMLParse, path, details(got))
				}
			})
		}
	}
	t.Run("a nested null stays ordinary", func(t *testing.T) {
		src := job("    env:\n      X: ~\n" + steps("run: echo hi"))
		if fs := checkWorkflows(wfTree(map[string]string{"ci.yml": src}), releaseRules); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
}

// A value of a type the decoder should not produce is held at its path, and the
// verdict names the type. The location used (a top-level env value) is otherwise
// allowed, so the only possible cause is the type. A plain string there is clean.
func TestCheckWorkflowsUnexpectedType(t *testing.T) {
	ci := string(fixture(t, "ci.yml"))
	t.Run("a binary scalar in env is held with its path and type", func(t *testing.T) {
		got := checkWorkflows(wfTree(map[string]string{"ci.yml": ci + "env:\n  X: !!binary aGk=\n"}), releaseRules)
		if len(got) != 1 || got[0].Rule != ruleWorkflowShape || !strings.Contains(got[0].Detail, "value of an unexpected type at env.X ([]uint8)") {
			t.Fatalf("want one unexpected-type finding at env.X, got:\n%s", details(got))
		}
	})
	t.Run("the same location with a plain string is clean", func(t *testing.T) {
		if fs := checkWorkflows(wfTree(map[string]string{"ci.yml": ci + "env:\n  X: hi\n"}), releaseRules); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
}

// The workflow reader holds a sequence merge behind an anchor as well; an
// anchored mapping merge is not a sequence merge.
func TestCheckWorkflowsAnchoredSequenceMerge(t *testing.T) {
	ci := string(fixture(t, "ci.yml"))
	t.Run("an anchored sequence merge is held", func(t *testing.T) {
		got := checkWorkflows(wfTree(map[string]string{"ci.yml": ci + "env:\n  <<: &s [{X: hi}]\n"}), releaseRules)
		wantHeld(t, got, ruleYAMLMerge, "sequence merge")
	})
	t.Run("an anchored mapping merge is not held as a sequence merge", func(t *testing.T) {
		got := checkWorkflows(wfTree(map[string]string{"ci.yml": ci + "env:\n  <<: &m {X: hi}\n"}), releaseRules)
		if slices.Contains(rulesOf(got), ruleYAMLMerge) {
			t.Fatalf("a mapping merge was held as a sequence merge:\n%s", details(got))
		}
	})
}

// The workflow reader holds a reused anchor name too, and accepts several uses
// of one anchor.
func TestCheckWorkflowsReusedAnchor(t *testing.T) {
	ci := string(fixture(t, "ci.yml"))
	t.Run("a second definition of the same name is held", func(t *testing.T) {
		got := checkWorkflows(wfTree(map[string]string{"ci.yml": ci + "env:\n  A: &s hi\n  B: &s hi\n"}), releaseRules)
		wantHeld(t, got, ruleYAMLParse, "defined more than once", `"s"`)
	})
	t.Run("several uses of one anchor are accepted", func(t *testing.T) {
		if got := checkWorkflows(wfTree(map[string]string{"ci.yml": ci + "env:\n  A: &s hi\n  B: *s\n  C: *s\n"}), releaseRules); len(got) != 0 {
			t.Fatalf("findings:\n%s", details(got))
		}
	})
}

var mirrorRules = workflowRules{mirror: true}

func TestCheckWorkflowsMirror(t *testing.T) {
	ci := string(fixture(t, "ci.yml"))
	heldDisabled := []string{"release.yml", "openapi-sync.yml", "stats.yml", "web.yml"}

	t.Run("the permitted set is ci.yml and the four held-disabled names", func(t *testing.T) {
		got := mirrorRules.permitted()
		for _, name := range append([]string{"ci.yml"}, heldDisabled...) {
			if !slices.Contains(got, name) {
				t.Fatalf("%s is not permitted: %v", name, got)
			}
		}
		if slices.Contains(got, forkRelease) {
			t.Fatalf("%s is permitted by mirror: %v", forkRelease, got)
		}
		if rel := releaseRules.permitted(); slices.Contains(rel, "web.yml") {
			t.Fatalf("release permits web.yml: %v", rel)
		}
	})

	t.Run("pass ci.yml with the four held-disabled paths", func(t *testing.T) {
		files := map[string]string{"ci.yml": ci}
		for _, name := range heldDisabled {
			files[name] = "on:\n  workflow_dispatch:\n"
		}
		if fs := checkWorkflows(wfTree(files), mirrorRules); len(fs) != 0 {
			t.Fatalf("findings:\n%s", details(fs))
		}
	})
	t.Run("held fork-release.yml", func(t *testing.T) {
		wantHeld(t, checkWorkflows(wfTree(map[string]string{"fork-release.yml": forkReleaseY}), mirrorRules), "workflow-set", "fork-release.yml")
	})
	t.Run("held other workflow names", func(t *testing.T) {
		for _, name := range []string{"new.yml", "Release.yml", "web.yaml", "release.yml.bak"} {
			wantHeld(t, checkWorkflows(wfTree(map[string]string{name: "on: pull_request\n"}), mirrorRules), "workflow-set", name)
		}
		wantHeld(t, checkWorkflows(wfTree(map[string]string{".github/workflows/sub/web.yml": "on: pull_request\n"}), mirrorRules), "workflow-set", "sub/web.yml")
	})
	t.Run("a held-disabled name is not judged by the enabled rules", func(t *testing.T) {
		// Without a permissions block, with a write scope, an unlisted action
		// and curl: none of that matters for a path that is held disabled.
		src := "on: pull_request\njobs:\n  x:\n    permissions: write-all\n    steps:\n      - uses: a/b@v1\n      - run: curl ${{ secrets.X }}\n"
		for _, name := range heldDisabled {
			if fs := checkWorkflows(wfTree(map[string]string{name: src}), mirrorRules); len(fs) != 0 {
				t.Fatalf("%s:\n%s", name, details(fs))
			}
		}
	})
	t.Run("the same content is held under the release rules, as an unlisted name", func(t *testing.T) {
		wantHeld(t, checkWorkflows(wfTree(map[string]string{"web.yml": "on: pull_request\n"}), releaseRules), "workflow-set", "web.yml")
	})
	t.Run("ci.yml is judged by the enabled rules", func(t *testing.T) {
		wantHeld(t, checkWorkflows(wfTree(map[string]string{"ci.yml": job(steps("run: curl x"))}), mirrorRules), "workflow-command")
		wantHeld(t, checkWorkflows(wfTree(map[string]string{"ci.yml": "on: pull_request\n"}), mirrorRules), "workflow-permissions")
	})

	// Triggers: the shared rule holds every tag-reachable shape in a
	// held-disabled file.
	tagReachable := map[string]string{
		"push tags":         "on:\n  push:\n    tags: ['v*']\n",
		"push tags-ignore":  "on:\n  push:\n    tags-ignore: ['v*']\n",
		"push scalar":       "on: push\n",
		"push in a list":    "on: [push]\n",
		"push null":         "on:\n  push:\n",
		"push paths only":   "on:\n  push:\n    paths: ['a']\n",
		"create":            "on: create\n",
		"branches and tags": "on:\n  push:\n    branches: [main]\n    tags: ['v*']\n",
		"no trigger":        "name: x\n",
	}
	for _, name := range heldDisabled {
		for tname, on := range tagReachable {
			t.Run("held "+name+" "+tname, func(t *testing.T) {
				wantHeld(t, checkWorkflows(wfTree(map[string]string{name: on}), mirrorRules), "workflow-trigger", name)
			})
		}
	}
	notReachable := map[string]string{
		"branches":                   "on:\n  push:\n    branches: [main]\n",
		"branches with paths":        "on:\n  push:\n    branches: [main]\n    paths: ['a']\n",
		"branches-ignore":            "on:\n  push:\n    branches-ignore: [x]\n",
		"pull_request":               "on: pull_request\n",
		"workflow_dispatch and cron": "on:\n  workflow_dispatch:\n  schedule:\n    - cron: '0 0 * * 1'\n",
	}
	for tname, on := range notReachable {
		t.Run("pass web.yml "+tname, func(t *testing.T) {
			if fs := checkWorkflows(wfTree(map[string]string{"web.yml": on}), mirrorRules); len(fs) != 0 {
				t.Fatalf("findings:\n%s", details(fs))
			}
		})
	}

	// A held-disabled file that cannot be judged is held.
	unjudged := map[string]struct{ src, rule string }{
		"unparseable":    {"on: [push\n", "yaml-parse"},
		"empty":          {"", "yaml-parse"},
		"null":           {"null\n", "yaml-parse"},
		"not a mapping":  {"- a\n", "workflow-shape"},
		"a scalar":       {"hello\n", "workflow-shape"},
		"duplicate key":  {"on: pull_request\non: pull_request\n", "yaml-duplicate-key"},
		"two documents":  {"on: pull_request\n---\non: pull_request\n", "yaml-documents"},
		"reused anchor":  {"on: pull_request\na: &x 1\nb: &x 2\n", "yaml-parse"},
		"sequence merge": {"on: pull_request\na: &x {k: 1}\nb:\n  <<: [*x]\n", "yaml-merge"},
	}
	for tname, c := range unjudged {
		for _, name := range heldDisabled {
			t.Run("held "+name+" "+tname, func(t *testing.T) {
				wantHeld(t, checkWorkflows(wfTree(map[string]string{name: c.src}), mirrorRules), c.rule, name)
			})
		}
	}
	t.Run("a held-disabled symlink is held", func(t *testing.T) {
		for _, name := range heldDisabled {
			fsys := fstest.MapFS{".github/workflows/" + name: {Data: []byte("../x"), Mode: fs.ModeSymlink}}
			wantHeld(t, checkWorkflows(fsys, mirrorRules), "github-non-regular", name)
		}
	})
	t.Run("an unreadable held-disabled file is held", func(t *testing.T) {
		wantHeld(t, checkWorkflows(unreadableFS{wfTree(map[string]string{"web.yml": "on: pull_request\n"}), ".github/workflows/web.yml"}, mirrorRules), "workflow-unreadable", "web.yml")
	})
}

// unreadableFS lists a file but fails to open it. It wraps the file system
// rather than embedding it, so that no ReadFile fast path bypasses Open.
type unreadableFS struct {
	inner fs.FS
	bad   string
}

func (u unreadableFS) Open(name string) (fs.File, error) {
	if name == u.bad {
		return nil, fs.ErrPermission
	}
	return u.inner.Open(name)
}
