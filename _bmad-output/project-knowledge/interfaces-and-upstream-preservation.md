# Interfaces and upstream-preservation obligations

## Interfaces the fork exposes

| Interface | Consumer | Contract | Source |
|---|---|---|---|
| `proton version` (text, `-o json`) | MCP server version gate | must print the fork string `X.Y.Z-ezop.N` (`FORK.md` › Versions and tags). JSON fields `version`, `go`, `platform`, `revision` | `internal/cli/self/self.go:19-47` |
| `proton --version` | humans | cobra prints the same `version` | `internal/cli/root.go:102` |
| Release asset names | MCP lock, `selfmanage`, installers | `proton-cli_{linux,darwin}_{amd64,arm64}`, `proton-cli_windows_{amd64,arm64}.exe`, `checksums.txt` (sha256, `<hex>  <file>`) | `.goreleaser.yaml:59-62,194-196`; `selfmanage/update.go:50-60,103-113` |
| Release tag | MCP lock | `vX.Y.Z-ezop.N` on an `ezop` commit | `FORK.md` |
| Command grammar, flags, JSON output, exit codes | MCP server and users | Upstream's declared language. Unchanged by the fork release | `internal/cli/kit/lang.go`, `internal/cli/conformance_test.go`, `internal/ui` goldens |
| Go module path | rebases, Upstream PRs | `github.com/roman-16/proton-cli`, never renamed | `go.mod:1`; `FORK.md` › Branches |

The external MCP requirements (FR-49, FR-53, FR-56, D4, cited by issue #1) live in `Ezopek/ezop-proton-calendar-mcp` and were not read in this phase. This page holds only what this repository and issue #1 say.

## Upstream-preservation obligations

These come from `FORK.md` and the operator mandate. They bind every later BMAD phase.

1. **`main` is a pure mirror.** Fast-forward only from `upstream/main`, with no fork commit ever, whether from a person or a workflow (`FORK.md` › Branches). Workflow hazards H1 and H2 (`workflows-and-release-inventory.md`) are the threats to this.
2. **Releases only from `ezop`**, tagged `vX.Y.Z-ezop.N`, published **only** to the fork's GitHub Releases, and never to winget, npm, Homebrew, AUR, apt or Pages (`FORK.md` › Releasing; issue #1).
3. **First release `v5.1.0-ezop.1` has no functional patches.** Only release configuration and workflows change. Nothing under `internal/`, `cmd/` or `tests/` changes.
4. **The module path stays `github.com/roman-16/proton-cli`.** So do the Upstream-pointing constants in `selfmanage`, `install.sh` and `report`; changing those is functional (`architecture-as-is.md` › Code that names the Upstream repository).
5. **Upstream text stays unchanged, and `FORK.md` is the only fork-policy authority.** README, `CONTRIBUTING.md`, `docs/` and the Upstream prefix of `AGENTS.md` are not edited (`FORK.md` intro). The blocks appended after that prefix are fork-only **operational** instructions, not competing policy sources. The installer's `bmad-coordinator` block says how to use the coordinator. The native `bmad:context` block routes agents to `FORK.md`, the knowledge folder and the gates. Neither restates or overrides fork policy. `CLAUDE.md` is a loading adapter only. If one of them conflicts with `FORK.md`, `FORK.md` wins, and the derived text is the thing to fix.
6. **No `CHANGELOG.md` version section for fork releases.** A section is Upstream's release button, and `FORK.md` takes precedence over the changelog-driven process (`FORK.md` › Fork-only development governance).
7. **BMAD and coordinator files never enter `main` or `patch/*`.** They live only in the `ezop` lineage and in working branches opened as PRs into `ezop` (`FORK.md` › Fork-only development governance). A branch carrying BMAD artifacts must therefore not be named `patch/*`.
8. **Push to `upstream` is disabled.** Upstream contributions go as PRs from `patch/*` (`FORK.md` › Following Upstream).
9. **Rebase-friendliness.** `ezop` is rebased onto each Upstream tag, and every fork edit to an Upstream file is a future conflict. This favors fork-only files over in-place edits where both are viable (decision D6).
10. **Live testing.** Never Upstream's multi-account `just test`. Live checks happen only in the MCP repository, on `calendar-dev`/`MCP Test`, one at a time, and only when authorized (`FORK.md` › Safety; operator mandate).
11. **No secrets, sessions, account data or calendar content** in the repository, its issues or CI logs (`FORK.md` › Safety).
12. **The weekly OpenAPI drift guard is required** (`FORK.md` › Releasing; MCP PRD §7; issue #2), and it must never commit to `main` (obligation 1). In the current session its execution is scheduled after #1. That is a priority call, not an exception. No obligation here may be met by dropping the guard.

## Conflict noted in issue text

Issues #1, #2 and #3 (the only bodies read in this phase) end with the same "Process" footer: "branch `patch/<topic>` from the Upstream tag … offer it Upstream". For #1 and #2 that footer conflicts with `FORK.md`, because the release configuration is fork-only (`FORK.md` › Branches: `ezop` holds "the release configuration"), and Upstream has no reason to take a fork-only publisher removal. Resolution by precedence (`FORK.md` wins): #1 and #2 land as reviewed topic branches with PRs into `ezop`, are not offered Upstream, and are not named `patch/*` if they carry BMAD artifacts. The footer applies as written to the calendar gaps (#3, and presumably #4–#13, whose bodies were not read).
