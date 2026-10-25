# Project context — `bmad-project-context` adopt run

**Workflow:** installed skill `bmad-project-context` (BMAD 6.12.4 bmm). The `bmad-help` catalog lists it as the successor to the retired document-project and generate-project-context skills. Intent: **adopt**, because `AGENTS.md` had content and no `bmad:context` block. Customization resolved to the defaults: no prepend, append, persistent facts or external sources.

The run took two passes:

1. **As-is pass (proposal only).** Steps 1–3 (assess, ledger, discover/verify) and the step-5 "show the block". Nothing outside `_bmad-output/` was written.
2. **Completion pass (applied).** The controller authorized the bounded source writes under the operator's adoption mandate. That authorization stands in for the skill's per-write approval for exactly this file set: the `AGENTS.md` append, the `FORK.md` governance paragraph, and the new `CLAUDE.md`. It ran step 4 (with controller-supplied facts), step 5 (splice), the step-6 close, and the Audit self-validation.

Discovery was sequential, by one author. All validation below is a **self-check**, not an independent review.

Provenance: verified 2026-10-25 against `dfa2c1dfcd7de0025f85b73b3968f23d05c96ae9`.

## Step 1 — assessment of existing instructions (as-is snapshot)

| File | Loaded by | State |
|---|---|---|
| `AGENTS.md`: Upstream text, then the installer's `bmad-coordinator` block | harnesses that read `AGENTS.md`. Claude Code did **not** load it in the as-is session | Upstream text current; coordinator block operational |
| `tests/AGENTS.md` | nested; loading unverified | Upstream test rules |
| `.agents/skills/live-test/SKILL.md`, `.agents/skills/user-docs/SKILL.md` | on trigger | Upstream skills |
| `FORK.md` | nothing loaded it automatically | the only fork-policy source |
| `CLAUDE.md` | absent at the as-is snapshot | – |

Unverifiable or stale: none found. Every `AGENTS.md`-cited path that was opened exists. The `/tmp/proton-cli-*` reference checkouts are created on demand by `just sources` and were not checked.

## Step 4 — interview facts (controller-supplied)

These were supplied by the controller from the operator mandate and source. No human interview was performed by the author.

1. **Harnesses:** Claude Code and Codex, working through the portable coordinator.
2. **Context location:** the managed block in `AGENTS.md`. `FORK.md` is the sole fork-policy source, and the block only routes to it.
3. **Upstream prefix:** the generated and rebased Upstream text of `AGENTS.md` is preserved byte-for-byte, and fork additions are appended after it.
4. **Earlier agent mistakes:** none recorded. No pitfalls are admitted, and no "Known pitfalls" heading is written.

One run-caveat is admitted on observed evidence. The controller reports that `just lint` passed only with the `devbox.lock` tool pair (Go 1.27.0, golangci-lint 2.13.1; versions read from `devbox.lock`), and that newer versions failed on unchanged code. The author did not run it.

## Ledger (adopt, settled)

| Entry | Decision | Reason / evidence |
|---|---|---|
| `AGENTS.md` › Project Context | retain | Upstream policy. Its "release is caused by `CHANGELOG.md`" line is overridden on `ezop` by `FORK.md`, and the block routes to `FORK.md` rather than editing Upstream text |
| › Feature Scope | retain | Upstream policy; binds #3–#13 |
| › The changelog is the release button | retain | true for Upstream and `main`. Fork precedence comes from `FORK.md` |
| › The interface is a language | retain | conformance-enforced Upstream policy |
| › A failure has to leave a trace | retain | Upstream policy |
| › Quality Gates | retain | consistent with `FORK.md`'s live-suite prohibition for fork work |
| › Testing | retain | – |
| › Reference Source | retain | – |
| `bmad-coordinator` block | retain, byte-exact | installer-managed operational instruction, not fork policy |
| `tests/AGENTS.md` | retain | – |

There are no deletions, relocations or rewrites. Automation: none proposed. The prose rules that matter (no `CHANGELOG.md` sections for fork releases, no `main` writes) depend on `FORK.md`, which is outside any linter's reach.

Contradiction check:

- Upstream "a release is caused by `CHANGELOG.md`" and `FORK.md`'s tag-driven release are reconciled by `FORK.md` › Fork-only development governance (precedence) and by the block's routing line.
- Upstream "`just build` produces the release-shaped binary" is compatible with the block's caveat that it reports `dev`: the binary is release-shaped but not version-stamped.

No live contradiction remains.

## Applied

| File | Change | Verification (self-check) |
|---|---|---|
| `AGENTS.md` | `bmad:context` block appended after `<!-- bmad-coordinator:end v1 -->`. Sections: orientation, Policy (routing only), Where things are, Running and verifying. Zero emphasis markers; no empty section | First 18505 bytes are sha256-identical to `HEAD:AGENTS.md` (the Upstream prefix). First 19811 bytes are sha256-identical to the pre-edit file (Upstream + coordinator block). `git diff --numstat`: 33 added, 0 removed |
| `FORK.md` | › Fork-only development governance gains 5 lines. They permit the native block and the `CLAUDE.md` adapter as fork-only additions and state that they are operational instructions and derived pointers, not policy, with `FORK.md` winning on conflict | additions only; no other section touched |
| `CLAUDE.md` (new, fork-only) | a comment plus `@FORK.md` then `@AGENTS.md` | file did not exist before |

Every file and command the block names was path-checked or verified against its source: `FORK.md`, `CHANGELOG.md`, `devbox.lock` (Go 1.27.0, golangci-lint 2.13.1), `.goreleaser.yaml`, `.github/workflows/`, `_bmad-output/project-knowledge/index.md`, `workflows-and-release-inventory.md`, `adoption-assessment.md` and `_bmad-output/NATIVE-BMAD-NEXT.md`. The `just` recipes `lint`, `test-fast`, `snapshot` and `build` are at `justfile:49-62,181-185,145-151,1-3`, and `just build` has no ldflags.

**Not verified:**

- Whether Claude Code actually expands `@FORK.md` and `@AGENTS.md` from `CLAUDE.md` in a **fresh** session. This session started before the file existed.
- Whether Codex loads `AGENTS.md`, and so the block, in this repository.

The skill requires loading to be verified for every harness in use, so both remain an acceptance gate for the controller.

## Close

- **What went in:** a routing-only policy section pointing at `FORK.md`, two pointers to the knowledge folder, and two verified run caveats (the locked tool versions; `just build` reports `dev`).
- **What stayed out:** every fork rule itself (it lives in `FORK.md`), anything Upstream's `AGENTS.md` and `CONTRIBUTING.md` already state, and unobserved pitfalls.
- **Loading:** `CLAUDE.md` imports `FORK.md` first, so fork precedence reaches Claude Code before the Upstream text. Codex reads `AGENTS.md` directly, and the block's first policy line routes it to `FORK.md`.
- **Submitting:** the changes are uncommitted on `ezop`. They belong only in the `ezop` lineage or a PR branch into it, never on `main` or `patch/*`. This pass made no commits.
- **Maintenance:** re-run `refresh` after the #1 merge (new workflow file and commands) and after each Upstream rebase, which must keep the Upstream prefix byte-identical and re-append the coordinator and context blocks. Use `record` the moment an agent gets something wrong.
