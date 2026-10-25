# Fork adoption brief — Ezopek/proton-cli release bootstrap

*Public-safe driving input for the native coordinator PRD. This is not a `bmad-product-brief` output and not a PRD. It condenses the operator mandate and the as-is evidence in this folder.*

## Why

`Ezopek/ezop-proton-calendar-mcp` (a local MCP server for Proton Calendar) runs the `proton` binary as its Backend and locks it by tag and sha256. Its version gate rejects every binary that does not match the lock, Upstream builds included. So the fork must publish its own builds from day one, before any functional patch exists (`FORK.md`; issue #1).

## Outcome

1. **Release `v5.1.0-ezop.1`:** Upstream `v5.1.0` plus fork release configuration only. Binaries for linux, darwin and windows on amd64 and arm64, plus `checksums.txt` (sha256), published **only** to the fork's GitHub Releases. `proton version` reports `5.1.0-ezop.1`.
2. **A repeatable fork release path:** a `vX.Y.Z-ezop.N` tag on `ezop` → a workflow → a fork GitHub Release. It never reaches AUR, Homebrew, winget, npm, apt or Pages, and `main` is never written.
3. **A safe Actions activation plan** with a named owner and gate, so that enabling Actions cannot set off Upstream workflows that still live on the default branch `main`.
4. **A weekly OpenAPI drift guard** that runs on the fork without committing to `main` (issue #2). This is a **required project outcome**: `FORK.md` › Releasing says the guard "stays enabled", and the MCP PRD §7 requires it. In this session it is **deferred** until after #1, by the operator's scheduling mandate. It is not dropped or waived, and no exception has been approved. Its mode and trigger stay open (D2).

## Users and stakeholders

- **Operator** (fork owner): decides repository settings, the activation strategy and the release go/no-go.
- **MCP server** (machine consumer): needs exact asset names, checksums and the version string.
- **Upstream maintainers:** must never receive fork publishing side effects. The fork offers patches Upstream only for functional gaps (#3–#13).

## In scope

- `.goreleaser.yaml` publisher cleanup (remove `aurs`, `homebrew_casks` and `winget`; resolve `nfpms`).
- A new tag-triggered fork release workflow. Neutralize Upstream's `release.yml` on `ezop`.
- An activation runbook for decision D1 (options A or B only), including the run-inventory preflight of S5. Execution is the operator's.
- Verification evidence: `just lint`, `just test-fast`, `just snapshot` before the tag; asset, checksum and `proton version` checks after.
- **The drift guard as a requirement (issue #2).** The PRD must carry it as a required outcome, with its timing and trigger unresolved. Building it is deferred until after #1 in this session. It is not designed or implemented here.

## Out of scope

- Any change under `internal/`, `cmd/` or `tests/`, including the update-target issue R3 and the calendar gaps #3–#13.
- The module path, Upstream docs, README, CONTRIBUTING and `CHANGELOG.md` sections.
- Upstream's live suite (`just test`) and any Proton account access.
- Changing `main`, or changing the default branch without an explicit operator decision.

## Constraints

Listed in `interfaces-and-upstream-preservation.md` (obligations 1–12) and `risks-and-constraints.md`. The ones that shape the design most:

- `workflow_run` and `schedule` use the default branch's files (Platform; verify), so an `ezop`-only fix is insufficient for activation safety.
- Workflow enable/disable state applies per file path, so a fork workflow must not share a path with an Upstream workflow that is to be disabled.
- GoReleaser already stamps `{{.Version}}` from the tag, so the version requirement is met by tag naming alone.

## Success criteria (from issue #1, plus the activation gate)

- S1: Tagging `v5.1.0-ezop.1` on `ezop` yields a fork GitHub Release with `proton-cli_{linux,darwin}_{amd64,arm64}`, `proton-cli_windows_{amd64,arm64}.exe` and `checksums.txt`, and every listed asset's sha256 matches.
- S2: `proton version` on a downloaded release binary reports `5.1.0-ezop.1`.
- S3: From the verified activation gate (S5) onward, no workflow run touches AUR, Homebrew, winget, npm, apt or Pages, or creates a release, tag or commit outside `ezop` tags and the fork's own Releases. In particular, no fork `v5.1.0` (non-`-ezop`) release appears. This covers the operational period after the gate only. It is not a claim about earlier periods, and the pre-activation run inventory is retained as evidence.
- S4: `main` stays byte-identical to `upstream/main` after activation and after the next mirror fast-forward.
- S5: Activation followed a written runbook with readback evidence of the workflow states and token or tag settings, **and** the run-inventory preflight in `workflows-and-release-inventory.md` §6. That preflight is: disable, then a complete paginated inventory of all workflow runs in every state (count-reconciled; fail closed if unreadable or incomplete), then a named owner and a safe disposition for each non-terminal or re-runnable run, then a fresh readback before enabling. The inventory is void after any new push, run, re-enable or workflow-definition change. Disabling a workflow does not cancel its existing runs.
- S6 (required project outcome, deferred past #1 in this session): a weekly OpenAPI drift run happens on the fork, makes Proton-side API drift visible, and never commits to `main` (issue #2 acceptance). Until S6 holds, the fork does not meet `FORK.md`'s "stays enabled" rule. That gap is tracked (R14), not waived.

## Open decisions to carry into the PRD

**D1** (activation strategy; operator; option A is the candidate default for #1 safety). Option A disables the inherited `openapi-sync.yml` and leaves no `ezop`-defined schedule able to fire while `main` is the default branch. So whichever D1 option is chosen must still leave a path to S6. **D2** (#2 mode and trigger; operator; open; scheduled after #1, never optional as a requirement). D3–D6 are for the architecture phase, D7 and D8 for the controller. Details are in `adoption-assessment.md` › Decision register.
