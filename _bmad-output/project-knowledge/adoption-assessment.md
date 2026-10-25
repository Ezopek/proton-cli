# Fork adoption assessment

Source-grounded assessment of the delta from "Upstream `v5.1.0` plus `FORK.md`" to "fork releases `v5.1.0-ezop.1` safely". The Upstream product design is taken as given. Only the adoption and release delta is assessed here.

## Current state

| Area | State | Ref |
|---|---|---|
| Branch base | `ezop` = tag `v5.1.0` (`03f5c25`) plus the `FORK.md` commit (`dfa2c1d`); equals `origin/ezop` | `.git` refs |
| Fork rules | `FORK.md` is the only fork-policy authority. At the as-is snapshot, uncommitted controller edits were: in `FORK.md`, the "Fork-only development governance" paragraph; in `AGENTS.md`, only the installer's `bmad-coordinator` opt-in block appended after the unchanged Upstream text. That block is operational coordinator instruction, not fork policy. Both were preserved as-is. The native project-context block and the `CLAUDE.md` adapter were added later, in the adoption-completion pass; see `project-context-proposal.md` › Applied | working tree |
| BMAD | 6.12.4 core+bmm in `_bmad/`, 29 skills in `.claude/skills/`, coordinator in `.bmad-coordinator/`. All untracked | working tree |
| Version stamping | already correct for tag-driven GoReleaser builds | `.goreleaser.yaml:29-30` |
| Fork release workflow | **missing** | – |
| Upstream publishers | all still present in `.goreleaser.yaml` and `release.yml` | inventory |
| Actions | disabled (controller readback) | – |
| Default branch | `main` | `origin/HEAD` |
| Drift guard | **required** (`FORK.md` › Releasing; MCP PRD §7) but **not running**: Actions are off, and the inherited `openapi-sync.yml` would write to `main` (H2). The requirement is currently unmet (R14) | inventory |

## Delta for issue #1 (no Go code)

1. **GoReleaser:** remove `aurs`, `homebrew_casks` and `winget`. Decide on `nfpms`; the recommendation is to remove it. Keep `builds`, the `binary` archive and `checksum`. Keep or drop the tar.gz/zip archives (D5). Keep `release` (D4).
2. **Fork release workflow:** triggered only by `push: tags: ['v*-ezop.*']`. Verify the tag matches `^v[0-9]+\.[0-9]+\.[0-9]+-ezop\.[1-9][0-9]*$` and that the tagged commit is reachable from `origin/ezop`. `permissions: contents: write` is scoped to the publish job. Run GoReleaser with fork-only config and notes (D3). Verify `checksums.txt` and the six raw binaries, and nothing `.deb`. Push no tag, use no git identity, use no third-party secret.
3. **Upstream `release.yml` on `ezop`:** remove it, or keep it inert (D6). Any `ezop` version must reference none of `AUR_KEY`, `TAP_WINGET_TOKEN`, `NPM_TOKEN` or the GPG secrets, and none of the `apt-repo`, `apt-deploy` or `npm-publish` jobs.
4. **Activation gate (D1):** neutralize `main`'s live triggers (H1–H4) before Actions are enabled, then complete the run-inventory preflight (`workflows-and-release-inventory.md` §6), because disabling does not cancel existing runs. This is outside the `ezop` diff and is operator-owned.
5. **Verification before tagging** (build phase, on the changed tree). The controller reports a baseline `just test-fast` and full `just lint` passing on the unchanged tree, using the `devbox.lock` tool pair. The author did not run them, and they must run again after the #1 changes: `just lint` (it runs `goreleaser check` and actionlint), `just test-fast`, `just snapshot`, then confirm that a snapshot binary's `proton version` shows the snapshot version format. A snapshot carries a snapshot version, not the tag, so the exact `5.1.0-ezop.1` string is proven only on the real release artifact.
6. **Release:** push tag `v5.1.0-ezop.1` on `ezop`, then download the assets, verify sha256 against `checksums.txt`, and check `proton version` on at least the host platform. This is acceptance evidence for #1. It is not authorized in this phase.

Out of scope for #1: any `internal/` or `cmd/` change, the R3 update-target fix, building the #2 drift guard, and #3.

## Delta for issue #2 (required outcome; execution deferred until after #1)

**Source requirement.** A weekly drift guard is required (`FORK.md` › Releasing: "stays enabled on this fork"; MCP PRD §7 as cited; #2 acceptance: "a weekly run happens on the fork"). No operator-approved exception exists.

**Scheduling priority.** In this adoption session, the operator's mandate puts building #2 after #1 and makes it optional for the session. That defers the work. It does not remove the requirement.

**Constraint, not resolved here.** The design depends on D1 and D2. Under D1 option A (`main` stays the default branch), Upstream's `openapi-sync.yml` is disabled and no `ezop`-only schedule can fire. A weekly guard would then need another mechanism, such as option B (default branch → `ezop`) or an external scheduler. A guard that runs only on manual dispatch does not meet the "weekly run" acceptance, and dispatch also needs the workflow on the default branch (Platform; verify with cited docs). This is the most constrained item, and this document picks neither its mode nor its trigger.

## Decision register

| ID | Decision | Real operator input? | Recommended default | Gate |
|---|---|---|---|---|
| **D1** | Actions activation strategy: A (distinct fork workflow + disable Upstream workflow IDs, `main` default) or B (default branch → `ezop`). Includes the token-permission and tag-ruleset settings. "No Actions; local release" (former C) is **not selectable**: it does not satisfy issue #1's Actions-pipeline acceptance and could only be a separately operator-approved scope change, which does not exist. No external scheduler or other infrastructure is added to make it work. Enablement also requires the run-inventory preflight (`workflows-and-release-inventory.md` §6) | **Yes.** Repository-settings authority, and the operator said not to change the default branch silently | A is the candidate for #1 release safety. A by itself leaves no weekly guard, so the chosen option must keep a path to the required #2 outcome | before any Actions enablement; after the #1 PR merges into `ezop`. **Blocking activation precondition:** under A, enabling Actions is refused unless the operator has approved a concrete viable path to the required weekly guard (S6), per `workflows-and-release-inventory.md` §6. A and B are both still assessed, no choice is assumed, and PRD and architecture stay unblocked. Building #2 stays after #1 and is not a prerequisite to #1's PR; the guard is neither optional nor waived |
| **D2** | #2 mode, report-only or write to `ezop`, and its trigger given the default-branch constraint | **Yes, deferred.** The requirement is not optional; only its timing is, after #1 | none chosen here (operator) | before #2 story work |
| D2b | Must drift visibility (#2) precede the first functional patch (#3)? | Only if the operator wants it. No document requires it | No | before #3 planning |
| D3 | Release-notes source for fork tags: GoReleaser default changelog, the tag annotation, or a fixed fork template | No (architecture) | short fork template naming the Upstream base, written to the runner temp directory | architecture |
| D4 | `prerelease: auto` (an `-ezop.N` release becomes a GitHub pre-release) vs explicit `false` | No (architecture); confirm the MCP lock does not use `/releases/latest` | keep `auto` (minimal diff) | architecture |
| D5 | Keep or drop the tar.gz/zip archives, and the `windows-alias` launcher that only the zip uses | No (architecture) | keep (no consumer harmed; minimal diff) | architecture |
| D6 | Edit Upstream files in place vs add fork-only files (`.goreleaser` fork config, a new workflow filename) and remove or neutralize Upstream's | No (architecture); D1 option A forces a distinct workflow filename | new fork workflow file; in-place removal of the publisher blocks in `.goreleaser.yaml` (as issue #1 states) | architecture |
| D7 | Track R3 (update targets Upstream) as a new fork issue | Operator or controller (a gh mutation; not done here) | yes, file it after #1 | after #1 |
| D8 | Splice the project-context block into `AGENTS.md` and add a repo `CLAUDE.md` adapter | Controller authorized it | **Resolved and applied** in the adoption-completion pass: native block appended to `AGENTS.md`; `CLAUDE.md` imports `@FORK.md` then `@AGENTS.md`; `FORK.md` governance paragraph extended. See `project-context-proposal.md` › Applied | done (uncommitted on `ezop`) |

Genuinely required operator input for the **next** phase (PRD and architecture): **none blocking**. D1 must be decided before Actions enablement, not before PRD or architecture. The PRD records D1 as an open decision with option A as the candidate default, and it carries the drift guard (#2) as a **required** outcome whose timing and trigger are open. The architecture designs the `ezop` diff so that it is safe under A or B and does not close off a path to the weekly guard.
