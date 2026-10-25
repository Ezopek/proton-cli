# Risks and constraints

Severity applies to the fork adoption and release goal. "Derived" means reasoned from source and platform behavior, never executed.

| ID | Risk | Severity | Evidence | Mitigation (owner) |
|---|---|---|---|---|
| R1 | Upstream `release.yml` on the default branch publishes an Upstream-built release (first `v5.1.0`, then every Upstream version) on the fork once Actions are on (H1) | Critical | `release.yml:13-17,58-116,181-192`; tags in clone; Derived | D1 activation strategy (A or B) before any enablement, plus the run-inventory preflight, because disabling a workflow does not cancel its existing runs (`workflows-and-release-inventory.md` §6) (operator) |
| R2 | Scheduled `openapi-sync.yml` commits to `main` and breaks the mirror (H2) | High | `openapi-sync.yml:3-12,40-60` | D1 plus issue #2 (operator, then architecture) |
| R3 | A fork binary's update notice and `proton update` resolve Upstream `latest`; semver ranks `5.1.0` above `5.1.0-ezop.1`, so an update installs Upstream | High for humans, Low for MCP (no TTY; can set `PROTON_NO_UPDATE_CHECK`) | `selfmanage/update.go:26-32,65-101`; `cli/self/notice.go:40-64` | Release notes must say "do not run `proton update`". A code fix is a later functional patch (operator: track as a new issue) |
| R4 | `prerelease: auto` marks `-ezop.N` as a pre-release, so the fork's `/releases/latest` will not point at it | Low | `.goreleaser.yaml:204-206` | D4 (architecture default: keep) |
| R5 | Rebase conflicts every time Upstream edits `.goreleaser.yaml` or `release.yml`, if the fork edits them in place | Medium | `FORK.md` › Following Upstream | D6: fork-only files vs in-place (architecture) |
| R6 | Gates for the **changed** tree (`just lint` incl. `goreleaser check` and actionlint, `just test-fast`, `just snapshot`) are not run yet. Historical note: the as-is author found no `go` or `just` on the PATH it inspected. That has since been superseded: the controller reports a baseline `just test-fast` and full `just lint` passing on the unchanged tree. Toolchain sensitivity: only the `devbox.lock` pair (Go 1.27.0 with golangci-lint 2.13.1) passed. Newer Go/linter combinations failed with no product code change | Medium | `devbox.lock`; controller report (not rerun by the author) | Run the gates with the `devbox.lock`-pinned tools in the build phase, after the #1 changes |
| R7 | `web.yml`/`stats.yml` produce recurring failing runs (or, with secrets, publish Upstream data) once Actions are on (H3, H4) | Low–Medium | `web.yml:11-63`; `stats.yml:17-75` | D1 option A disables them |
| R8 | The workflow token's default permission and tag-creation rights are unknown on the fork | Medium | not observable from the repo | Operator checks and sets settings before enablement |
| R9 | A devbox shell sources `.env`; a wildcard `go test` could start the live suite against real accounts | High if triggered | `devbox.json:25-27`; `tests/AGENTS.md` | Use only `just test-fast`; never `just test` (policy) |
| R10 | The issue footer says `patch/<topic>` and "offer Upstream" for fork-only release work | Low | issues #1, #2 | Resolved by `FORK.md` precedence (`interfaces-and-upstream-preservation.md`) |
| R11 | Repository `AGENTS.md` was not auto-loaded by Claude Code. The as-is session's context carried only the user-level instruction file | Medium → mitigated | observed in the as-is session | Applied: fork-only `CLAUDE.md` importing `@FORK.md` then `@AGENTS.md`. Loading in a fresh session is still to be verified (see `project-context-proposal.md` › Applied) |
| R12 | Release notes source undefined for fork tags. Running without `--release-notes` falls back to GoReleaser's default changelog | Low | `release.yml:154-171,188`; `.goreleaser.yaml:198-201` | D3 (architecture) |
| R14 | The **required** weekly OpenAPI drift guard (`FORK.md` › Releasing; MCP PRD §7) is not running on the fork. Actions are off, the inherited workflow cannot run safely (H2), and D1 option A would disable it outright. The requirement is unmet until #2 lands | High (requirement gap; early warning for Proton API changes missing) | `FORK.md:43`; `openapi-sync.yml:3-60`; issue #2 | Carry #2 as a required outcome in the PRD with its timing and trigger open (D2). It is deferred, not waived, and no operator exception exists. The D1 choice must keep a path to it |
| R13 | Git identity of the Upstream maintainer is hardcoded in workflow steps (`release.yml:149-150`, `openapi-sync.yml:51-52`, `stats.yml:56-57`) | Low | source | A fork workflow must not reuse it. A tag-triggered release does not tag, so it needs no identity |

## Constraints (hard)

- Documentation phases write only `_bmad-output/**`. The single exception was the controller-authorized adoption-completion pass, which added the managed block to `AGENTS.md`, the `CLAUDE.md` adapter and one `FORK.md` governance sentence.
- No functional patch in `v5.1.0-ezop.1`. The module path is unchanged.
- No `just test`, no Upstream multi-account live run, no credential retrieval, and no Proton account, API, calendar or Pass access.
- No gh mutations, no release, tag or Actions controls, no production binary use, no fetch, merge, pull, reset, stash, commit or push in this phase.
- `main` is never written. The default branch is not changed silently.
