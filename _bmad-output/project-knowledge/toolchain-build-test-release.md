# Toolchain, build, test and release entry points

Read from `justfile`, `devbox.json`, `flake.nix`, `go.mod`, `tests/AGENTS.md` and Upstream `AGENTS.md`/`CONTRIBUTING.md`. **The author ran none of these commands.** At the time of the as-is inspection, `go` and `just` were not on the PATH the author looked at, and running lint or build could have rewritten files while they were being documented. That is historical and describes the author's inspection only; it is not a current blocker. Everything about behavior below comes from reading the recipes.

Controller-reported baseline (not rerun by the author): on the unchanged tree, `just test-fast` passed and the full `just lint` passed, including actionlint, `goreleaser check`, shellcheck, golangci-lint with no issues, and the Windows, Windows-webauthn and Darwin builds. It passed only with the `devbox.lock` tool pair, Go 1.27.0 and golangci-lint 2.13.1. Newer Go or linter versions produced findings on unchanged code, so use the locked tools rather than whatever is installed.

## Toolchain

| Item | Declared in | Note |
|---|---|---|
| Go `1.27.0` | `go.mod:3` | CI uses `go-version-file: go.mod` (`release.yml:53-56`, `stats.yml:41-43`) |
| devbox packages, all `"latest"` | `devbox.json:3-20` | Actual pins live in `devbox.lock`. CI uses `jetify-com/devbox-install-action` (`ci.yml:23,43`) |
| `CGO_ENABLED=0` | `devbox.json:21-23`, `.goreleaser.yaml:21-22,48-49` | |
| devbox `init_hook` sources `.env` | `devbox.json:25-27` | `.env` holds the live-suite credentials (`CONTRIBUTING.md` › Tests) and is git-ignored (`.gitignore:5`). A devbox shell therefore exports them, and the next row is why that matters. |
| Without devbox | `CONTRIBUTING.md` › Getting set up | Go ≥1.27, actionlint, charm-freeze, golangci-lint, goreleaser, just, nixfmt, protoc, protoc-gen-go, shellcheck. `just lint` also runs `bun` for `web/` (`justfile:55,86-88`) |

## Recipes relevant to adoption

| Recipe | What it does (`justfile` lines) | Safe for an agent? |
|---|---|---|
| `just test-fast` | `go test $(go list ./... \| grep -v '/tests/live$\|/node_modules/') -count=1` (181-185, 167) | **Yes.** This is the only test an agent starts by itself (Upstream `AGENTS.md`). |
| `just lint` | `gofmt -w`, `go mod tidy`, `nixfmt`, `just docs`, `just web`, actionlint, `goreleaser check`, shellcheck, golangci-lint, cross-OS builds (49-62) | It rewrites files, so it is a write step. CI fails on any diff afterwards (`ci.yml:24-25`). |
| `just build` | `go build -o proton ./cmd/proton`, **no ldflags** (1-3) | Yes. The binary reports `dev`. |
| `just snapshot` | `goreleaser release --snapshot --clean --skip=publish` with dummy `AUR_KEY`/`TAP_WINGET_TOKEN` (145-151) | Yes, it publishes nothing. It runs `gen-completions.sh` and writes `dist/` and `completions/` (both git-ignored). |
| `just flake` | Nix build of the working tree (36-43) | Needs `nix`. |
| `just test`, `just test-one`, `just coverage*`, `just test-report` | Live suite against three accounts (177-179, 187-224) | **No.** `FORK.md` › Fork-only development governance forbids it for fork bootstrap work, and Upstream `AGENTS.md` makes it a user-only decision. |
| `just login`, `just seed`, `just demo` | Sign in or mutate the test accounts (64-66, 141-143, 10-26) | **No**, same reason. |
| `just openapi`, `just sources` | Clone WebClients and the Drive SDK into `/tmp`, then regenerate `openapi.yaml` (82-84, 112-113) | Network, and it rewrites a tracked file. Issue #2 only. |
| `just notes` | `go run ./scripts/changelog` and `--notes` (68-71) | Read-only. It prints Upstream's newest section (`5.1.0`, `CHANGELOG.md:9`). |

**Trap (from Upstream `tests/AGENTS.md`):** `go test ./...` or `./tests/...` starts the live suite whenever the nine `PROTON_CLI_TEST_*` variables are set. Inside a devbox shell, the `.env` hook may set them. Use `just test-fast` or name packages explicitly.

## Tests: what exists versus what this phase established

| Tier | Location | Status in this phase |
|---|---|---|
| Unit, golden, conformance | `internal/**/_test.go`; `internal/cli/conformance_test.go`; golden data in `internal/ui`, `internal/cli/testdata` | Present. **Not run.** |
| Offline binary suite | `tests/offline/` (includes `selfmanage_test.go`) | Present. **Not run.** |
| Rules, install, argv, account, fixture, paid | `tests/rules/`, `tests/install/`, ... | Present. **Not run.** |
| CHANGELOG format check | `scripts/changelog` via `just test-fast` (`scripts/README.md` › Changelog) | Present. **Not run.** The version pattern accepts a pre-release suffix (`internal/changelog/changelog.go:33,110-112`), so `5.1.0-ezop.1` is syntactically valid. This phase did not establish whether the step rule accepts it. The question does not need answering, because a fork release must not add a `CHANGELOG.md` section: a section is Upstream's release request, and on `main` it would set off the inherited Release workflow. |
| Live suite | `tests/live/` | Forbidden for this work. |

"Not run" above means not run by the author. The only passing results these artifacts cite are the controller-reported baseline above, which was taken on the unchanged tree.

## Release entry points

| Path | Trigger and behavior | Reference |
|---|---|---|
| Upstream Release workflow | `workflow_run` of CI on `main`; reads `CHANGELOG.md`; tags; GoReleaser; apt, npm, report | `.github/workflows/release.yml` (inventory in `workflows-and-release-inventory.md`) |
| GoReleaser config | Builds, archives, nfpms, AUR, Homebrew cask, winget, checksums, GitHub release | `.goreleaser.yaml` |
| Local dry run | `just snapshot` | `justfile:145-151` |
| Fork process | Rebase, `just lint` + `just test-fast`, tag `vX.Y.Z-ezop.N` on `ezop`, push the tag, workflow publishes **only** to fork GitHub Releases | `FORK.md` › Releasing |

`FORK.md` › Releasing step 3 describes a fork release workflow that does not yet exist. Creating it is issue #1.
