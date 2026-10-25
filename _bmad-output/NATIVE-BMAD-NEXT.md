# Native BMAD — next phases

Installed: BMAD 6.12.4 (`core`, `bmm`), skills under `.claude/skills/`. Output folders come from `_bmad/config.toml`: `planning_artifacts = _bmad-output/planning-artifacts`, `implementation_artifacts = _bmad-output/implementation-artifacts`, `project_knowledge = _bmad-output/project-knowledge`.

The controller binds and drives these phases through the portable coordinator. This file specifies them. It does not start them.

## Completed: brownfield as-is (this phase)

- Route found with `bmad-help`: the catalog row for `bmad-project-context` says it "replaces document-project and generate-project-context". No legacy skill is installed.
- `bmad-project-context`, intent adopt. Output is in `_bmad-output/project-knowledge/`; start at `index.md`. The as-is pass stopped at a proposal. A later controller-authorized completion pass applied it: a managed `bmad:context` block appended to `AGENTS.md`, a fork-only `CLAUDE.md` adapter (`@FORK.md`, then `@AGENTS.md`), and a `FORK.md` governance sentence (`project-context-proposal.md` › Applied).
- The fork adoption brief (`project-knowledge/fork-adoption-brief.md`) is the PRD driving input.

## Phase sequence

| # | Phase | Installed skill (intent) | Inputs | Output (resolved path) | Acceptance gate |
|---|---|---|---|---|---|
| 1 | PRD create | `bmad-prd` (Create). Optional predecessor `bmad-product-brief` is **skipped**; the adoption brief takes its place | `project-knowledge/fork-adoption-brief.md`, `adoption-assessment.md`, `issues-snapshot.md`, `interfaces-and-upstream-preservation.md`, `risks-and-constraints.md`, `FORK.md` | `_bmad-output/planning-artifacts/prds/prd-proton-cli-<date>/prd.md` + `.memlog.md` (+ `addendum.md`) | `status: final`. FRs trace to S1–S6. "No functional patch / no `internal/` change" is explicit. D1 is recorded as an open decision with a candidate default. D2 is recorded as open, with no default. The weekly drift guard (#2) is a **required** FR/outcome (S6), with its timing set after #1 and its mode and trigger open. It must not be dropped, waived or marked optional as a requirement. #3 is out of scope |
| 2 | PRD validate | `bmad-prd` (Validate) | phase-1 PRD | validation report in the same run folder | no unresolved critical findings. Self-validation is labelled a self-check |
| 3 | Architecture create | `bmad-architecture` (create; brownfield; feature altitude "fork release pipeline + activation") | final PRD, `workflows-and-release-inventory.md`, `architecture-as-is.md`, `toolchain-build-test-release.md` | `_bmad-output/planning-artifacts/architecture/architecture-proton-cli-<date>/ARCHITECTURE-SPINE.md` + `.memlog.md` | AD entries settle D3–D6. Every "Platform" claim in inventory §4 is re-verified against current GitHub Actions and GoReleaser v2 docs, with the source logged. The design is safe under D1 options A **and** B, which are the only live choices (option C is not selectable; see D1 below), and keeps a path open to the required weekly drift guard. The design also carries the activation preflight in `workflows-and-release-inventory.md` §6: complete run inventory, disposition of non-terminal and re-runnable runs, fresh readback, fail closed. No Upstream workflow path is reused by the fork workflow |
| 4 | Architecture validate | `bmad-architecture` (validate) | spine | findings in the run folder | no blocking findings |
| 5 | Epics and stories | `bmad-create-epics-and-stories` | PRD + spine | `_bmad-output/planning-artifacts/` epics file(s) | Expected shape: **Epic 1 (#1)**: GoReleaser publisher cleanup; fork tag-triggered release workflow; neutralize Upstream `release.yml` on `ezop`; activation runbook (operator-executed, readback evidence); release verification checklist. **Epic 2 (#2): required outcome, sequenced after Epic 1.** It is optional only as work in the current session; its mode and trigger come from D2. Each story has ACs mapped to S1–S6 and a verification command list |
| 6 | Readiness | `bmad-sprint-planning` (readiness) | PRD, spine, epics | gate report | PASS (or CONCERNS with an owner per concern) |
| 7 | Sprint planning | `bmad-sprint-planning` (sprint-planning) | epics | `_bmad-output/implementation-artifacts/sprint-status.yaml` | file validates |
| 8 | Build (per story) | `bmad-build` | story + spine | topic branch → PR into `ezop` (never `main`, never `patch/*` if it carries BMAD files) | `just lint` clean with no diff after, `just test-fast`, `just snapshot` all run and passing with output captured; then independent review by a reviewer separate from the author (a blocked or unavailable review is not a pass). **No auto-merge, no tag, no Actions change** |

Model roles: use the planning model for phases 1–7 and a Sonnet-class model for phase 8 code, selected per invocation. Independent review must come from a reviewer separate from the author.

## Preconditions the controller owns

- **Toolchain:** `go`, `just`, `goreleaser` v2, `actionlint`, `shellcheck`, `golangci-lint`, `nixfmt` and `bun` (for `just lint` → `just web`), or `devbox`. The controller reports a baseline `just test-fast` and full `just lint` passing on the unchanged tree, but only with the `devbox.lock` pair Go 1.27.0 and golangci-lint 2.13.1. Phase 8 must use the locked versions. (Historical: the as-is author found none of these on the PATH it inspected.)
- **Actions stay disabled** until D1 is decided and the runbook is executed with readback, including the run-inventory preflight (`workflows-and-release-inventory.md` §6). Disabling a workflow does not cancel its existing runs.
- **Coordinator:** the controller holds the owner token, claims and records evidence. Workers do not.

## Real unresolved material decisions

| ID | Decision | Owner | Needed before |
|---|---|---|---|
| D1 | Actions activation strategy: A (distinct fork workflow + disable Upstream workflow IDs, `main` stays default) or B (default branch → `ezop`), plus token-permission and tag-ruleset settings. No option has been selected. A alone does not deliver the weekly guard. "No Actions; local release" (former option C) is **not** a selectable completion path: it does not satisfy issue #1's Actions-pipeline acceptance, and it could only be a separately operator-approved scope change, which does not exist. No external scheduler, cron or other infrastructure is added to rescue it | operator | **Actions enablement** (not PRD or architecture) |
| D2 | Issue #2 mode (report-only vs write to `ezop`) and its trigger under the default-branch constraint | operator | #2 story work (scheduled after #1; the requirement itself is not optional) |

D3–D6 (notes source, prerelease flag, archives, in-place vs fork-only files) belong to the architecture phase. D7 (file an issue for the update-target risk R3) belongs to the controller. D8 (splice the project-context block, add `CLAUDE.md`) is resolved and applied, but not committed.

**Operator input required to start phase 1: none.**
