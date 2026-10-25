# Fork issue snapshot (read-only, 2026-10-25)

Source: `gh issue list --repo Ezopek/proton-cli --state all`, plus `gh issue view` for #1, #2 and #3. Both were read-only. All 13 issues are open.

| # | Title | Labels |
|---|---|---|
| 1 | Release pipeline for fork builds; bootstrap v5.1.0-ezop.1 | release, priority |
| 2 | OpenAPI drift guard on the fork without breaking the main-mirror rule | release |
| 3 | Preserve unmodelled VEVENT properties on update, import and export | calendar-gap, priority |
| 4 | Add and remove attendees on events update | calendar-gap |
| 5 | Expose attendee RSVP status (PARTSTAT) in JSON output | calendar-gap |
| 6 | RSVP for a single occurrence, and an RSVP comment | calendar-gap |
| 7 | Move an event to another calendar | calendar-gap |
| 8 | Clear an event's own colour | calendar-gap |
| 9 | Set a calendar's description | calendar-gap |
| 10 | Video conferencing: read and write Proton Meet and Zoom on events | calendar-gap |
| 11 | Per-event time zone, including separate start and end zones | calendar-gap |
| 12 | Calendar show/hide (Display): expose and set | calendar-gap |
| 13 | Keep participants on ICS import (as a record) | calendar-gap |

## #1 — requirements as written (condensed)

- Version `X.Y.Z-ezop.N` via ldflags; `proton version` prints it.
- Tag `v` + version. The first is `v5.1.0-ezop.1`, with no functional patches.
- Keep builds for linux, darwin and windows on amd64 and arm64, plus `checksums.txt` (sha256).
- Remove or disable every Upstream publish target **before** Actions are enabled: in `.goreleaser.yaml` `aurs`, `homebrew_casks` and `winget`, reviewing `nfpms`; in `release.yml` the `apt-repo`, `apt-deploy` and `npm-publish` jobs, and every use of `AUR_KEY`, `TAP_WINGET_TOKEN`, `NPM_TOKEN` and the GPG keys.
- Publish only to the fork's GitHub Releases.
- Replace the `workflow_run`/`resolve` decision with a trigger on `v*-ezop.*` tags from `ezop`.
- Enable Actions only after the cleanup is merged into `ezop`.
- Acceptance: tagging `v5.1.0-ezop.1` on `ezop` yields a Release with every platform binary and `checksums.txt`; a downloaded binary's `proton version` reports `5.1.0-ezop.1`; no run touches AUR, Homebrew, winget, npm, apt or Pages.

Gap the operator mandate adds, not stated in the issue text: "Enable Actions only after the cleanup is merged into `ezop`" is **insufficient on its own**. `main` (the default branch) keeps Upstream's `release.yml`, `openapi-sync.yml`, `stats.yml` and `web.yml`, and those define the `workflow_run` and `schedule` triggers (hazards H1–H4). Activation needs its own gate (decision D1).

## #2 — requirements as written (condensed)

- The weekly drift guard is required (MCP PRD §7). `openapi-sync.yml` must not commit to `main`.
- Options: run against `ezop`, or report only (an issue or job summary diffing against `tests/api-coverage.golden`, committing nothing).
- Acceptance: a weekly run happens on the fork and makes drift visible; `main` never gets a fork commit.

Platform constraint, not verified with cited docs in this phase: `schedule` reads the default branch's file. While `main` is the default branch, a weekly guard defined only on `ezop` never fires, in either mode. Option B of D1 (default branch → `ezop`) or an external scheduler would change that. This document does not choose between them.

## #3 — sequencing only

#3 calls itself "the first functional patch" (MCP PRD §7, A-14). It is not implemented or planned in detail here.

## Sequencing: #1 → #2 (required outcome, deferred) → #3

There are two separate questions here, and they have different answers:

- **Is it required?** Yes. A safe weekly OpenAPI drift guard is a **required project outcome**. `FORK.md` › Releasing says the inherited guard "stays enabled on this fork", the MCP PRD §7 requires it (as cited by `FORK.md` and #2), and #2's acceptance demands a weekly run. No operator-approved exception exists.
- **When does it run?** Later. The operator's mandate for the current adoption session makes executing #2 optional, and only after #1. That decides scheduling priority in this session. It does not waive the requirement, and #2 stays open until the requirement is met.

The apparent conflict is that #2 is "release"-labelled and blocked on the same default-branch problem as #1's activation, while #3 is "priority" and the hub names it the first functional patch.

Resolution, consistent with the operator mandate:

1. **#1 first.** `v5.1.0-ezop.1` has no functional patches and no drift guard dependency.
2. **#2 is required, deferred until after #1.** It is CI-only, not a functional patch, so it does not compete with #3 for the "first functional patch" slot. `FORK.md` says the guard "stays enabled", but it is not enabled today: Actions are off, and Upstream's `openapi-sync.yml` cannot run safely on the fork (H2). Until #2 lands, the fork does not meet this requirement, and that gap is tracked as risk R14. Its design depends on D1 and D2, and neither is decided here. Any D1 choice must still leave a path to a weekly guard.
3. **#3 is the first functional patch**, released as a later `v5.1.0-ezop.N` (N ≥ 2) or on a new Upstream base. It does not wait for #2 unless the operator decides that drift visibility must precede functional patches. That would be D2b, and it is not required by any document read here.
