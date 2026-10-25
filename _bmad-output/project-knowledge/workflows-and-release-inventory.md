# Workflow, release and publisher inventory

Covers every file under `.github/workflows/` (there are five: `ci.yml`, `release.yml`, `openapi-sync.yml`, `web.yml`, `stats.yml`) and `.goreleaser.yaml`, as they are at `ezop` `dfa2c1d`. The author did not check out `main`. The controller has since confirmed with a git diff that the workflow and release-config bytes on `origin/main` (`147edc0`) are identical to `ezop`'s, so every finding below applies to `main`'s live copies too.

Repository state, from local refs: `origin/HEAD → origin/main`, which makes `main` the default branch. The local clone carries tags `v1.0.0` through `v5.1.0`, plus a `data` branch at `origin/data`. Actions are **disabled** on the fork, per the controller's readback. That state was not observed directly in this phase.

Labels in this document:

- **Source:** read in the file.
- **Platform:** GitHub Actions behavior taken from the author's documentation knowledge. No doc is cited for it here. The controller has retrieved official GitHub and GoReleaser documentation, but each Platform claim still needs a cited check in the architecture phase.
- **Derived:** reasoning that combines the two. Never executed.

## 1. Workflows

### `ci.yml` — CI

| Aspect | Value | Ref |
|---|---|---|
| Triggers | `pull_request` (any base), `push` to `main` | `ci.yml:10-13` |
| Permissions | `contents: read` | `ci.yml:15-16` |
| Jobs | `lint`: `devbox run just lint`, then `git diff --exit-code`. `nix`: macOS and Ubuntu `nix build`. `test`: `devbox run just test-fast` | `ci.yml:19-44` |
| Secrets | none | |
| Publishes | nothing | |
| Fork hazard | Low in itself, but its success on a `push` to `main` is what triggers `release.yml` | `release.yml:13-17` |

### `release.yml` — Release (Upstream changelog-driven)

| Aspect | Value | Ref |
|---|---|---|
| Trigger | `workflow_run` of `[CI]` `completed` on `branches: [main]` | `release.yml:13-17` |
| Guard | success, `event == 'push'`, head repository is this repository | `release.yml:34-37` |
| Permissions | top-level `contents: write`; `resolve` read; `apt-repo` adds `id-token: write`, `pages: write`; `npm-publish` adds `id-token: write` | `release.yml:19-20,39-40,233-236,299-302,314-316` |
| Decide | version = newest section of `CHANGELOG.md`; if no published release exists for `v<version>`, release. An existing tag is reused if it is an ancestor of the CI head | `release.yml:58-116` |
| Tag | annotated tag pushed with a hardcoded Upstream maintainer git identity | `release.yml:136-152` |
| Notes | `scripts/changelog --notes` plus a compare link | `release.yml:154-171` |
| GoReleaser | `release --clean --release-notes=…`, `continue-on-error: true`, env `AUR_KEY`, `GITHUB_TOKEN`, `TAP_WINGET_TOKEN` | `release.yml:181-192` |
| Verify | requires `checksums.txt`, six raw binaries **and** `*_linux_{amd64,arm64}.deb` | `release.yml:194-214` |
| `apt-repo` | downloads `.deb` from the last 10 releases, signs with `GPG_PRIVATE_KEY`/`GPG_PRIVATE_KEY_PASSPHRASE`, uploads a Pages artifact | `release.yml:230-295` |
| `apt-deploy` | `actions/deploy-pages` into environment `github-pages` | `release.yml:297-309` |
| `npm-publish` | `scripts/publish-npm.mjs` with `NPM_TOKEN` | `release.yml:311-349` |
| `report` | probes the **Upstream** apt Pages URL, AUR, npm, the Homebrew tap and winget | `release.yml:351-451` (probes at 370-372, 397, 443-445) |

**Derived hazard H1, critical.** Once Actions are enabled, the first successful CI `push` run on `main` (a mirror fast-forward) would start this workflow from `main`'s copy of the file. `scripts/changelog` names `5.1.0`. The fork has no published GitHub Release `v5.1.0`, because releases are not copied into forks. Tag `v5.1.0` is present and is an ancestor of `main`. So `resolve` would answer `release=true`, and GoReleaser would publish an **Upstream-built `v5.1.0` GitHub Release on the fork**, with Upstream's notes, marked latest. After that, `apt-repo` and `npm-publish` would fail for lack of secrets, AUR, Homebrew and winget would fail inside the `continue-on-error` step, and `report` would flag red channels. Every later Upstream version section merged into the mirror would repeat this, and where the tag is absent from the fork, the workflow would push a new tag with the Upstream identity. Nothing in an `ezop`-only change prevents it, because `workflow_run` is read from the default branch. (Platform: `workflow_run` only triggers when the workflow file exists on the default branch.)

### `openapi-sync.yml` — Sync OpenAPI

| Aspect | Value | Ref |
|---|---|---|
| Triggers | `schedule: "0 1 * * 1"` (Mondays 01:00 UTC), `workflow_dispatch` | `openapi-sync.yml:3-9` |
| Permissions | `contents: write` | `openapi-sync.yml:11-12` |
| Steps | `just openapi` (clones WebClients and the Drive SDK), `scripts/openapi-guard`, then commit `openapi.yaml` with the Upstream identity and `git push` to the checked-out (default) branch | `openapi-sync.yml:28-60` |
| Secrets | `GITHUB_TOKEN` only | |
| Fork hazard H2 | **Platform:** scheduled runs use the default branch's file and commit, so this commits a fork-generated `openapi.yaml` to `main`. That breaks the pure-mirror rule, and the next `merge --ff-only upstream/main` fails. This is issue #2. The weekly guard itself is **required** (`FORK.md:43`; MCP PRD §7), so neutralizing this workflow without replacing it leaves a requirement gap (R14). Fixing H2 and meeting the requirement are separate obligations. **Platform:** a push made with `GITHUB_TOKEN` does not start new workflow runs, so it would not chain into CI or Release. | |

### `web.yml` — Web

| Aspect | Value | Ref |
|---|---|---|
| Triggers | `push` to `main` on docs-ish paths, `workflow_dispatch` | `web.yml:11-22` |
| Publishes | the built site to the **external** repository `roman-16/proton-cli-web` `gh-pages` with `WEB_DEPLOY_KEY`, plus a custom domain | `web.yml:50-63` |
| Fork hazard H3 | Without the secret, the deploy step fails. A mirror push that touches `docs/**`, `README.md` or `CHANGELOG.md` produces a failing run on every mirror update. No publish is possible without the key. | Derived |

### `stats.yml` — Stats

| Aspect | Value | Ref |
|---|---|---|
| Triggers | `schedule: "0 0 * * *"` daily, `workflow_dispatch` | `stats.yml:17-24` |
| Permissions | `contents: write` | `stats.yml:32-33` |
| Behavior | Builds `scripts/stats` and reads **Upstream** counters (`scripts/stats/main.go:40,46`) with `STATS_TOKEN`. It commits to and pushes the orphan `data` branch | `stats.yml:47-75` |
| Fork hazard H4 | With no `STATS_TOKEN`, the collector treats the traffic refusal as fatal (`scripts/README.md` › Stats) and nothing is pushed: a daily failing run. If someone ever sets the token, Upstream's statistics get committed to the fork's `data` branch. | Derived |

## 2. GoReleaser publishers (`.goreleaser.yaml`)

| Block | Purpose | Secret | Issue #1 disposition | Ref |
|---|---|---|---|---|
| `before.hooks` | `gen-completions.sh` | – | keep | 3-5 |
| `builds` `unix`, `windows`, `windows-alias` | linux/darwin/windows × amd64/arm64; Windows `-tags=webauthn`; launcher | – | keep | 16-56 |
| `archives` `binary` | raw `proton-cli_<os>_<arch>[.exe]`, the names `selfmanage`, the installers and `release.yml` verification expect | – | keep | 59-62 |
| `archives` `archive`, `archive-windows` | tar.gz/zip with LICENSE and completions; comments say they feed AUR, Homebrew and winget | – | open: keep (harmless) or drop | 63-79 |
| `nfpms` | apk, deb, rpm; maintainer and homepage are Upstream's | – | "review". Recommended: remove (no apt channel; packages would carry the Upstream maintainer). If removed, also drop the `.deb` checks | 81-112 |
| `aurs` | AUR `proton-cli-bin` push over SSH | `AUR_KEY` | remove | 114-138 |
| `homebrew_casks` | push to `roman-16/homebrew-tap` | `TAP_WINGET_TOKEN` | remove | 140-162 |
| `winget` | branch on `roman-16/winget-pkgs`, then a PR to `microsoft/winget-pkgs` | `TAP_WINGET_TOKEN` | remove | 164-192 |
| `checksum` | `checksums.txt`, sha256 | – | keep | 194-196 |
| `release` | `prerelease: auto`, `mode: replace`, `replace_existing_artifacts: true`; no `github:` owner, so it targets the repository the run is in | `GITHUB_TOKEN` | keep. `prerelease: auto` makes `-ezop.N` a GitHub **pre-release** (decision D4) | 202-212 |

`release.yml` passes `--release-notes`. A run without it, and with no `changelog:` block, falls back to GoReleaser's default changelog generation. Platform: the default is a git-log changelog. Decision D3 covers which source the fork uses.

## 3. Secret inventory

| Secret | Used by | Fork stance |
|---|---|---|
| `GITHUB_TOKEN` | every job | the only secret the fork release needs |
| `AUR_KEY` | `.goreleaser.yaml:122`, `release.yml:190`, `justfile:149` (dummy) | remove the path |
| `TAP_WINGET_TOKEN` | `.goreleaser.yaml:146,186`, `release.yml:192`, `justfile:150` (dummy) | remove the path |
| `NPM_TOKEN` | `release.yml:347` | remove the path |
| `GPG_PRIVATE_KEY`, `GPG_PRIVATE_KEY_PASSPHRASE` | `release.yml:289-290` | remove the path |
| `WEB_DEPLOY_KEY` | `web.yml:58` | not in issue #1's list; it is inert without the secret. Decide alongside the activation strategy |
| `STATS_TOKEN` | `stats.yml:52` | not in issue #1's list; inert without the secret. Same decision |

`justfile` `snapshot` (149-150) exports dummy `AUR_KEY`/`TAP_WINGET_TOKEN`. Once the blocks are removed, those lines become dead but harmless. Whether to edit them is a minimal-diff call for the architecture phase.

## 4. Trigger semantics that decide the design (Platform; verify)

| Event | Which workflow file is used | Consequence |
|---|---|---|
| `push` (branch or tag) | the file at the pushed commit | A tag-triggered workflow living only on `ezop` runs when a `v*-ezop.*` tag on an `ezop` commit is pushed, whatever the default branch is |
| `pull_request` | the file at the PR merge ref | `ci.yml` runs for PRs into `ezop` |
| `workflow_run` | the default branch only | `main`'s `release.yml` is the live one (H1) |
| `schedule` | the default branch only | `main`'s `openapi-sync.yml` and `stats.yml` are the live ones (H2, H4). An `ezop`-only schedule never fires while `main` is default |
| `workflow_dispatch` | the file must exist on the default branch for the workflow to be dispatchable; the run can target another ref | A dispatch-only guard on `ezop` is not reachable while `main` is default |
| Enable/disable a workflow | applies per workflow file path, repository-wide | Disabling `release.yml` also disables any `ezop` file at the **same path**, so a fork release workflow must use a distinct filename if Upstream's path is to be disabled |

## 5. Activation strategies (for decision D1)

| Option | Mechanism | Pros | Cons |
|---|---|---|---|
| **A. Distinct fork workflow + disable Upstream workflow IDs** (candidate default for #1 release safety) | On `ezop`, add a fork release workflow under a new filename, triggered only by `push: tags: ['v*-ezop.*']`, which verifies the tagged commit is on `origin/ezop` and publishes only to this repository. Before or immediately at enablement, disable the `release.yml`, `openapi-sync.yml`, `stats.yml` and `web.yml` workflows repository-wide; keep `ci.yml` for PR checks. Read back the workflow states, and complete the run-inventory preflight in §6 before enabling Actions. | `main` and the default branch stay untouched. Uses supported settings only. `main` remains a byte-identical mirror. | Relies on settings state rather than code. The order of operations matters (enable → disable gap): do it in a quiet window away from 00:00 and 01:00 UTC, with no pending push. Someone could re-enable a workflow later. Whether workflows can be disabled while Actions is off is unverified. **It leaves the required weekly drift guard (#2) with no runnable schedule** while `main` is the default branch, so A alone does not meet `FORK.md:43`. |
| B. Default branch → `ezop` | `schedule`, `workflow_run` and `dispatch` read `ezop`'s files. `ezop` drops or neutralizes Upstream's workflows and owns the drift guard (issue #2) | Code, not settings, decides. Issue #2 can run on `ezop` | Changes the repository's default (clone default, PR base, README rendering). The operator said not to change it silently. `push`-to-`main` `ci.yml`/`web.yml` still run from `main`'s files |
| ~~C. No Actions; local GoReleaser and manual upload~~ **Not selectable under the current mandate** | `goreleaser release` on the operator machine with a fork-only config | Zero workflow exposure | **Does not satisfy issue #1's requested Actions pipeline acceptance** ("Tagging `v5.1.0-ezop.1` on `ezop` produces a GitHub Release…", "no workflow run touches…") and departs from `FORK.md` › Releasing step 3 ("the release workflow builds"). It could be considered only as a **separately operator-approved scope change**; no such approval exists. This document does not add an external scheduler, cron job or other infrastructure to make C meet the required weekly guard (S6) |

Only A and B are live choices, and no operator decision has selected either. The architecture gate (`NATIVE-BMAD-NEXT.md` › phase 3) covers exactly A and B.

None of the options settles issue #2. Whichever is chosen, the drift guard stays a required outcome, with its mode and trigger decided separately (D2). This inventory does not pick them.

Further guards to apply under A or B, all repository settings (operator-owned): default workflow token permission set to read-only; a tag ruleset limiting who can create `v*` tags; a rule that `main` accepts only fast-forwards from Upstream.

## 6. Activation preflight: existing workflow runs (documentation requirement)

Disabling a workflow "doesn't cancel its existing runs" (GitHub documentation, quoted in `_bmad-output/research/platform-reference-excerpts.md`), and re-running them is blocked only while the workflow stays disabled. So disabling is necessary but not sufficient. This section is a requirement on the activation runbook. It is not an API operation performed by this document, and nothing here has been executed.

Before Actions are enabled, the runbook must complete these in order, and record the evidence:

1. **Disable** the Upstream workflows (`release.yml`, `openapi-sync.yml`, `stats.yml`, `web.yml`) as in option A, or neutralize them as B requires.
2. **Inventory all workflow runs** in the fork, not only the ones tied to a disabled workflow. Paginate to the end and reconcile the counts, comparing the total reported by the API with the number of runs actually enumerated. Include every run state, not just "in progress": queued, in progress, requested, waiting, pending, and any other non-terminal state. Also list historical completed runs that could be re-run (see step 4).
3. **Fail closed.** If the inventory cannot be read in full, the counts do not reconcile, or a state is unrecognized, the preflight fails and Actions stay disabled.
4. **Disposition.** Any non-terminal run, and any historical run whose workflow could be re-run after re-enabling, needs a **named owner** and an explicit, safe disposition or stop decision. Do not cancel blindly. Do not re-enable or retry an unsafe Upstream workflow to clear a run.
5. **Fresh readback.** Immediately before enabling, read back the workflow states and the run inventory again.
6. **Stale-inventory invalidation.** The inventory is void, and the preflight restarts, after any new push, any new run, any workflow re-enable, or any change to a workflow definition between the inventory and the enablement.

### Blocking precondition under option A: an approved viable path to the required weekly guard (S6)

Option A disables `openapi-sync.yml`, and while `main` is the default branch no `ezop`-only schedule can run (§§4–5). So under A, **the preflight above must refuse activation** unless the operator has approved a concrete, viable path to the required weekly guard (S6), recorded with all of:

- a **named owner**;
- the **proposed supported trigger and default-branch arrangement**;
- the **dependency and timing**, relative to #1 and #2;
- **proof of viability**, checked against cited platform constraints.

A statement that a guard might exist eventually does not pass. No path has been selected or approved, and this document introduces no external infrastructure.

This gate does not move the work. Building the guard (#2) stays **after** #1. A running #2 is **not** a prerequisite to completing #1's pipeline work or its PR; the precondition applies only to enabling Actions. The requirement is neither optional nor waived.

If no approved viable path exists, Actions stay off. The owner then either selects B, or returns a genuine scope decision. Neither a silent waiver nor a silent default-branch change is acceptable.

Scope of the guarantee: the gate protects the operational period **after** a verified preflight and admitted activation. It makes no claim about any earlier period, and a "no run has ever…" statement is not a deliverable. The pre-activation inventory is retained as required evidence.
