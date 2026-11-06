---
title: 'Story 1.5: A repeat run is refused, fail closed'
type: 'feature'
created: '2026-11-06'
status: 'done'
route: 'dispatch'
baseline_commit: 'beec3b2c7a9720d8dafb50197b74935fb7d719dc'
review_loop_iteration: 0
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md'
  - '{project-root}/_bmad-output/planning-artifacts/epics.md'
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-proton-cli-2026-10-25/ARCHITECTURE-SPINE.md'
  - '{project-root}/FORK.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Nothing yet stops a second run of the release workflow from creating or replacing a release that already exists. A re-run after a release object exists, or after a record has landed for the tag, must be refused, and an unreadable state must never be read as safe (AMD-FR18-1).

**Approach:** Add, in `scripts/fork-release/`, the one release lookup (an authenticated, fully paginated list of releases filtered on the tag), the pure refusal decision over the record state of Story 1.4 and the release state, and the two in-run modes of `guard`: `guard --in-run`, which runs every guard in the fixed AD-6 order, and `guard --recheck`, which repeats only the record and release reads and the same decision immediately before GoReleaser. Authoritative wording of the criteria: `epics.md`, Story 1.5.

## Boundaries & Constraints

**Always:**
- `FORK.md` and the accepted spine win. Outside `_bmad-output/`, the change touches only files under `scripts/fork-release/`; no `go.mod` or `go.sum` change (standard library only), nothing under `internal/`, `cmd/`, `tests/`, `fork/` or `.github/`, and `.goreleaser.yaml`, `justfile`, `devbox.lock` and the earlier fixtures stay byte-identical. Earlier stories' behavior and assertions are kept; the only edits to old files are the ones this story needs: the path check of the `gh` adapter in `recordlookup.go` (to allow the paginated releases path), the `guard` dispatch and `guardDeps` in `guard.go`, and an optional extra step in `checkTagBinding` that leaves its behavior without the step unchanged.
- **Release states** (one lookup shared by every command): **None** (the whole list was read and no release has the tag), **Published-Same** (exactly one release has the tag, it is not a draft, and its body holds exactly one line `Commit: ` followed by 40 lowercase hex characters equal to `E`; a trailing carriage return is tolerated), **Draft** (exactly one release has the tag and it is a draft), **Other** (exactly one release has the tag and it is none of the above, such as a published release whose body has no, a repeated, a malformed or a different `Commit:` line), **Unknown** (a failed call, a missing, non-array or malformed page, an entry without a string `tag_name` or a boolean `draft`, two entries with the tag, or a list that does not end within the page ceiling). A partial list never reads as None.
- **Reading the list:** `GET repos/Ezopek/proton-cli/releases?per_page=100&page=N` from page 1, through the same pinned `gh` adapter (explicit `--method GET`, the API-version header, owner and repository as constants, never `GH_REPO`), until the first empty page. A page ceiling of 50 pages is a termination guarantee: exceeding it is Unknown. The `Commit:` line prefix is one shared constant that the notes writer of Story 1.6 will also use.
- **Refusal decision** (pure): proceed only for record **Absent** with release **None**; refuse every other combination, including a record that is Present (malformed or naming another tag too, since only presence is read), NotFile or Unknown, and a release that is Published-Same, Draft, Other or Unknown. A tag with no release object and no record may be retried; once any release object exists the version is abandoned and the next `N` is used. The refusal is one line `fork-release: repeat: <reason>` whose reason names the cause from a closed vocabulary and never a response, URL or name from an answer.
- **`guard --in-run`** takes no other argument. It reads `GITHUB_REF_NAME` (the tag), `GITHUB_SHA` and `GORELEASER_PIN` (the workflow's one GoReleaser pin, `vX.Y.Z`) from the environment, and runs in this order, stopping at the first refusal: tag pattern; identity of `HEAD`, `GITHUB_SHA` and the tag on origin; `E` reachable from the `origin/ezop` fetched once; Upstream Base; the `release` profile on the tree of `E`; the toolchain check; the refusal decision. Each refusal is one line `fork-release: <check>: <reason>` with the checks `tag`, `identity`, `reachable`, `base`, `profile`, `toolchain`, `repeat`; a profile refusal prints one such line per finding. Success prints one line and exits 0. It makes no write call of any kind, creates or pushes no tag and needs no build-identity or origin-URL check (those belong to the Operator-side commands).
- **`guard --recheck`** takes no other argument, reads `GITHUB_REF_NAME`, resolves `E` from `HEAD`, reads the record state and the release state again and applies the same decision.
- Exit codes: 0 pass, 1 any refusal, 2 usage; the new modes do not change the output or exit codes of `guard --profile`.
- Tests use fakes and scratch repositories only (the existing helpers); no `gh` run, no network, no real `origin`, ref or tag of this repository, no skip. Comments and docs use US spelling and are public-safe. Gates run through the pinned toolchain with the recipes under Verification; never `just test` or any `go test` pattern that reaches `tests/live`.

**Never:**
- No `--pre-push` (Story 1.8), notes, assets or stamp code (Story 1.6), `verify` or `verify --status` (Story 1.7), workflow file (Story 1.9), runbook, record file, or `FORK.md` edit (the wording below travels in the pull request description). No lock, queue, service, state file or recovery protocol: overlapping attempts are covered by the maintainer procedure, not by code. No live GitHub, Proton or authenticated call; no write call; no new dependency.
- No skipped, weakened or disabled test or gate; no Actions, tag, release, settings or default-branch change.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| First run | tag valid; record Absent; release list complete without the tag | every guard passes; the decision proceeds; exit 0 | N/A |
| Retry before any release | same as the first run after a failed earlier attempt | proceeds | N/A |
| Release exists | published, draft or other release object for the tag | `fork-release: repeat: ...`, exit 1 | no later step runs |
| Record exists | record Present (any content), NotFile | refuse | N/A |
| Ambiguous | record Unknown or release Unknown (failed call, bad page, repeated tag, page ceiling) | refuse | reason from the closed vocabulary |
| Release classes | Published-Same with CRLF in the body; Other for a missing, repeated, malformed or different `Commit:` line; tag found on page 3 | classified as stated; a page-2 failure is Unknown, never None | N/A |
| Guard order | each earlier guard fails (tag, identity, reachable, base, profile, toolchain) | its one-line refusal; nothing after it is read | profile: one line per finding |
| Recheck | `guard --recheck` over the same states | same decision, only the two reads, no tree or tag reads | N/A |
| Usage | `guard` with no mode, an unknown mode, extra arguments | usage line, exit 2 | N/A |
| Adapter | each releases call | argv is `api --method GET -H X-GitHub-Api-Version: 2026-03-10 repos/Ezopek/proton-cli/releases?per_page=100&page=N` | a path outside the allowed shapes is refused before `gh` runs |

</frozen-after-approval>

## Code Map

- `scripts/fork-release/recordlookup.go` -- `ghGetter`, `execGH`, `ghArgs`, `endpointPattern` (the only allowed paths), `lookupRecord` returning `recordLookup{State, Commit, Data, Reason}` with `recordPresent/Absent/NotFile/Unknown`: reuse as is; extend `endpointPattern` for `releases?per_page=100&page=<1-50>` only.
- `scripts/fork-release/tagcheck.go` -- `checkTagBinding(g, tag, githubSHA, pin)` runs pattern → resolve → `OriginTags` → identity → `FetchEzop` and reachability → base → toolchain on `E`; its tests use trees without `.goreleaser.yaml`, so the profile step cannot simply be added. Add an optional step run between base and toolchain (for example `checkTagBindingWith(..., between func(e string) error)`, with `checkTagBinding` calling it with nil) so the old tests are unchanged. `checkTagPattern`, `checkIdentity`, `upstreamBase`, `checkBaseMatch`, `checkToolchainAt` stay as they are.
- `scripts/fork-release/guard.go` -- `guardDeps`, `productionDeps`, `run`, `runGuard` (`--profile` only; its usage cases must stay usage), `guardUsage`, exit constants, the profile reading via `commitTreeFS(...)` and `CheckRelease`. Add the two flags, new `guardDeps` fields for an environment reader and the `ghGetter` (nil-safe, production `os.Getenv` and `execGH{}`), and extend the usage text. Old tests compare against the `guardUsage` constant and do not use the spellings `--in-run` and `--recheck`.
- New: `releaselookup.go` (states, the paginated list, the `Commit:` line constant and parser, `lookupRelease`), `repeat.go` (the refusal decision and the two in-run modes), and a test file named after each.
- Facts from the official documentation read by the controller (cached copies, no API call; the list-releases page was re-fetched at adoption): a regular Git tag not associated with a release is not in the list, which is why "no release object" means the list holds no release with the tag; the releases list takes `per_page` (max 100, default 30) and `page`; each item carries `tag_name`, `draft`, `body`; only users with push access receive draft listings, and whether the Actions job token counts is not stated (Q5), so a draft the job cannot see reads as None here and the Operator's own pre-re-run check covers it; the list has no total to reconcile against, hence the empty-page end and the ceiling. Documentation examples are not proof of live behavior; the first real observation stays future (Q6).
- Do not change: the profile code, the tag and toolchain check logic, the record validator, the README, any fixture.

## Tasks & Acceptance

**Execution** (strict TDD, tests first with no production-first exception: write the tests and observe the missing-API compile failure; only then add the minimal stubs that give a runnable assertion RED and keep that output; then implement; never edit an oracle to make it pass; no skip; one cluster per step):
- [x] `scripts/fork-release/releaselookup.go`, `releaselookup_test.go`, `recordlookup.go` -- the path shape, the pinned argv of the paginated call, the list over a fake that serves pages, and the five states with the `Commit:` line parser: None (empty list, other tags, a tag only on a later page), Published-Same (including CRLF), Draft, Other (missing, repeated, malformed, different line), Unknown (each cause named in the frozen rules, a failure on page 2, the ceiling) -- AR-8, AD-7
- [x] `scripts/fork-release/repeat.go`, `repeat_test.go` -- the refusal decision as a table over every record state and release state, with only (Absent, None) proceeding and a distinct reason for each refusal, one line, nothing quoted from an answer -- AMD-FR18-1, FR-18
- [x] `scripts/fork-release/tagcheck.go`, `guard.go`, `repeat.go`, `repeat_test.go`, `guard_test.go` -- the optional step, the two flags, the environment inputs and the fixed order: the passing run, each failing guard stopping later reads (recorded by the fakes), the profile refusal lines, the success line and exit codes, `--recheck` reading only the two states, usage cases, and one scratch end-to-end case for the first run -- AR-7, AD-6

**Acceptance Criteria:**
- Given the one release lookup, when it runs, then it lists releases through the pinned adapter, paginates to the end, filters on the tag, and classifies None, Published-Same, Draft, Other or Unknown as stated, and a partial list never reads as None.
- Given the record state and the release state, when the refusal decision runs, then it proceeds only for (Absent, None) and refuses every other combination, proved with the positive case, the negative cases (a record; a public or draft release) and the ambiguous cases (Unknown), without touching a real release or tag.
- Given `guard --in-run`, when it runs in the fixed order, then each refusal is one `fork-release: <check>: <reason>` line with no secret or account data and exit 1, and `guard --recheck` applies the same decision to a fresh read of the two states.
- Given the pull request, when reviewed, then its diff touches only files under `scripts/fork-release/`, `just test-fast`, full `just lint` (no diff left) and `just build` pass, no test runs `gh` or reads the real tree, and the description carries the proposed wording. Traces: FR-4, FR-18, S1, S3; AD-6, AD-7, AD-14.

## Implementation Notes

**Execution roles (set before dispatch; the implementation child's sole source is this spec).**
- The implementation child owns the code and tests and the focused green result of the package tests, strict TDD. It runs only `go test ./scripts/fork-release/ -count=1` (with `-run`, `-v` or `-json` as needed) using the pinned Go 1.27.0 already on `PATH`, with `GOPROXY=off`, `GOFLAGS=-p=1`, `GOMAXPROCS=1`, `GOMEMLIMIT=768MiB` and no network. Save each red and green run's complete output together with its true exit status in a file under `$TMPDIR` (never pipe a run into `head` or `tail`, which hides the status; capture the status first), name the files by cluster and phase, and list them in the report.
- Not run by the child, because the controller runs them against the frozen candidate after the child returns: `just test-fast`, full `just lint`, `just build`. The child reports a focused checkpoint and says plainly that broad qualification is incomplete; it must not claim the acceptance criteria are fully verified.
- No mutation of this checkout's git state: no `git add`, `rm`, `reset`, `checkout`, `stash`, `commit`, `push`, `fetch`, tag, ref or configuration change, and no `git` call against the real repository except read-only inspection. Scratch `git` only inside temporary repositories the tests create. **Never run `gh`, any GitHub API, Proton or any network call**; the lookups are tested against fakes and, for the adapter, against the stand-in program that the Story 1.4 tests already use.
- Allowed paths: new `scripts/fork-release/releaselookup.go`, `releaselookup_test.go`, `repeat.go`, `repeat_test.go`; edits to `recordlookup.go`, `guard.go` and `tagcheck.go` exactly as the Code Map describes; and an existing test file only where compile compatibility or the old command-line behavior needs it, reported with the exact reason and with every old assertion preserved (never change an old test to hide an incompatible behavior). Nothing else: no README, `fork/`, workflow, `FORK.md`, `justfile`, module file or other product file. The spec explicitly grants the command-line wiring of `guard --in-run` and `guard --recheck`; it grants no workflow caller.
- Every existing test and subtest keeps its behavior; no count is hard-coded for the future. No new dependency, environment variable beyond the three named in the frozen rules, or configuration that could bypass a check. Leave no background process running.

**Test-first order, exactly.** For a cluster whose API does not exist yet, write the tests first and run them: keep the compile failure (exit status 1, missing names). Only then add the smallest stubs that compile and run the tests again: keep that runnable assertion failure (RED by assertion, not by compile error). Then implement the minimum and keep the green run. For work on an API that already exists (the path check, the dispatch, the optional step), the tests must fail by assertion directly; do not invent a compile failure.

**Fixture and design guidance.**
- Reuse the fakes and helpers of the existing tests (`fakeGit`, `testDeps`, `newScratch`, `newRelease`, `cleanTree`, the `fakeAPI` of the record tests, and the stand-in `gh` program with its `TestMain` hook). A fake serves release pages by page number, so tests can place the tag on a later page, fail one page, end on an empty page and exceed the ceiling.
- A release is Published-Same only with exactly one `Commit: <40 lowercase hex>` line equal to `E` in its body; a CRLF body is tolerated. Every release object makes the decision refuse, so the classification matters for the status of later stories, not for this decision; test both.
- The decision is a pure function of the two states and refuses with a reason from a closed vocabulary, never quoting an answer. Prove it as a full table of record states times release states with only (Absent, None) passing, each negative beside the realistic passing case.
- Draft visibility to the job token is not established (Q5): do not claim it, do not test it, and do not add a control for it.

**Implementation record (first pass, before the broad gates).**
- Four new files (`releaselookup.go`, `releaselookup_test.go`, `repeat.go`, `repeat_test.go`) and edits to three old source files under `scripts/fork-release/`: `recordlookup.go` (the path check also allows `releases?per_page=100&page=1..50`), `guard.go` (the two flags, two nil-safe `guardDeps` fields, the extended usage text) and `tagcheck.go` (`checkTagBinding` now calls `checkTagBindingWith(..., nil)`, which runs an optional step between the base and toolchain checks). No existing test file was edited and no old assertion changed; no README, `fork/`, workflow, `FORK.md` or dependency change; no `gh` or network call.
- Choices made by the implementation, within the frozen intent: the decision takes the two states only, so the refusal line does not say why a lookup was Unknown (less diagnosis in a job log, nothing quoted from an answer); a profile refusal collapses each finding to one line; `guard --profile release --in-run` still parses as the profile mode with a commit named `--in-run`, exactly as before; the success line reads `fork-release: guard: ok: <mode> <tag> at <E>`.
- The focused package run passed 1,405 distinct tests and subtests with none failed or skipped; the 1,192 that existed before all still pass and 213 are new. Test-first order: each of the three clusters kept a compile failure, a runnable assertion failure and a green run. Two slips in the implementation's own freshly written tests (an uppercase-hex case that tested nothing and a request count off by one) were corrected in the test, not in any old oracle.
- **Gate history.** The controller's first full gates passed `just test-fast` and failed `just lint` on one unused test helper (`setRelease` in `repeat_test.go`), so the build was not started; that attempt is kept as a failure. A correction removed only that helper (two lines, no assertion), and the corrected candidate then passed `just test-fast`, full `just lint` and `just build`, each exit 0 under a lowered memory limit, with the focused run unchanged at 1,405 distinct tests and subtests, none failed or skipped. (The first implementation receipt interchanged the manifest and diff hashes; the corrected values are recorded by the controller and the original text is kept.) No real GitHub API, `gh`, Proton or native Windows run was made. Whether the job token can see a draft release (Q5) is neither claimed nor tested.
- **Task and matrix audit (author).** All three tasks are done in the diff. Every matrix row has executed, passing, non-skipped tests among the 1,405 focused IDs: first run and retry before any release (5), a release exists (26), a record exists or is not a file (3), ambiguous states (39), the full decision table (17), release classes, pagination, CRLF, a tag on a later page and a failed middle page (87), guard order and refusals (50), recheck (27), usage and flag routing (23), adapter argv and path guard (5). The criterion about the pull request itself is future.

**Completion** (native present step). The three-lens native review and its 20-finding triage are kept as they stand. The independent review returned APPROVE with no material finding, and accepted the two optional low points (the comment about what "a partial list never reads as None" covers, and an extra whitespace test for profile findings) without a patch or new backlog. The controller's first full gates passed `just test-fast` and failed `just lint` on one unused test helper, so the build was not started; that failure is kept. A correction removed only that helper (two lines, no assertion), and the corrected candidate then passed `just test-fast`, full `just lint` and `just build`, each exit 0 under a lowered memory limit; the focused package run passed 1,405 distinct tests and subtests with none failed or skipped. The first implementation receipt interchanged the manifest and diff hashes; the correction is recorded by the controller and the original text is left as written. No real GitHub API, `gh`, Proton or native Windows run was made. Whether the Actions job token sees draft releases (Q5) is unverified, so the Operator's own pre-re-run check and the maintainer rule against overlapping attempts remain the cover for what the lookup cannot see. The in-run guard modes have no workflow caller until Story 1.9, nothing is published, and nothing is deferred. A done spec is not a done sprint entry, a completed epic, a release or production readiness.

## Spec Change Log

## Review Triage Log

First review pass (native step 4: three context-free layers, same model capability, launched together). The blind layer's floor and the edge layer's scenarios are not proven defects; each row is the result of checking the claim against the code, its callers, the accepted architecture and the official endpoint documentation read by the controller. The verification-gap layer reported no gap. No real API, `gh` or network was used.

| ID | Finding (short) | Verdict | Evidence |
|----|-----------------|---------|----------|
| BH-1 | Refusing on any release object contradicts `FORK.md`'s "retry a safe run" and the diff does not amend it | false | `FORK.md` says "safe", which the approved amendment defines as no release object and no record; the architecture states that `FORK.md` does not yet say it and carries the proposed wording in the pull request description, which the spec drafts. |
| BH-2 | Draft visibility to the job token is an unstated precondition; a hidden draft reads as None | low | True and known (Q5): the documentation lists drafts only for users with push access and does not say the job token counts. The spec records it as a limit that the Operator's own pre-re-run check covers, but the code comment "a partial list never reads as None" overstates what is covered. Direct fix: correct the comment (group G1). |
| BH-3 | The page walk has no snapshot; a release deleted or moved mid-walk can shift an entry across a page | low | Needs another actor changing releases during one walk of a few pages; a created release shows up as a repeated entry and is Unknown. A fix is a second pass or locking, which the spec excludes. The comment part is G1. |
| BH-4 | No concurrency guard between the check and GoReleaser | false | Accepted limit: the spine covers overlapping attempts by the maintainer procedure and the workflow's concurrency group (Story 1.9), and `--recheck` narrows the window by design. |
| BH-5 | `--recheck` takes `E` from `HEAD` and does not compare with `GITHUB_SHA` | low | The steps between the two calls are the workflow's closed list (Story 1.9 pins them) and none moves `HEAD`; a guard is new machinery for a misconfiguration. |
| BH-6 | The whitespace collapse of profile findings is untested; the test compares the raw text | low | Real: the code claims one line per finding but no test feeds a finding with a newline or run of spaces. Fix is one test (group G2). |
| BH-7 | `TestCommitLinePrefixIsShared` and the ceiling check are tautological | low | They pin a contract constant until Story 1.6's writer exists; cosmetic. |
| BH-8 | Hard-coded request counts make tests brittle | low | They fail loudly if the record lookup changes; cosmetic. |
| BH-9 | `runRecheck` folds a git error and a non-SHA result into one message | low | Both refuse at `identity`; the finer cause is not worth a taxonomy (as in earlier stories). |
| BH-10 | Comments cite planning IDs that are not in the repository | low | The same convention is used in the committed code of earlier stories; the rule is also stated in the comments. |
| BH-11 | Missing negative cases (lone CR, BOM, prefix-of-E, message difference between Published-Same and Other) | low | The parser and the decision tests already cover the cases that matter; these are speculative additions. |
| EH-1 | A release created or deleted during the walk | low | Same as BH-3. |
| EH-2 | A token that cannot see drafts reads None | low | Same as BH-2 (Q5, group G1). |
| EH-3 | `--recheck` after `HEAD` changed | low | Same as BH-5. |
| EH-4 | Two runs for one tag both read None | false | Same as BH-4. |
| EH-5 | A finding's path or detail with control characters or great length reaches the log | low | The text comes from the Operator's own reviewed commit; the whitespace collapse already covers line breaks. As in Story 1.2, hardening it is speculative. |
| EH-6 | A releases page above the 8 MiB cap leaves every call Unknown | low | A page of 100 releases with assets is about 2 MiB; this repository's list is a handful of entries (releases are not copied from Upstream). Speculative. |
| EH-7 | Claim: a profile refusal is several lines, not one | false | The frozen rules say exactly this: one such line per finding. |
| EH-8 | Claim: "a partial list never reads as None" is not met with hidden drafts or shifting pages | low | The claim holds for pages (failure, malformed, repeated, ceiling → Unknown); the two exceptions are BH-2 and BH-3. Documentation fix, group G1. |
| VG-0 | The gap layer reported no verification gap | n/a | Recorded for completeness: no row. |

**Declared implementation choices.**
| Choice | Ordinary path | Result |
|--------|---------------|--------|
| The decision takes the two states only, so an Unknown's cause is not printed | A job log shows "could not be read" without saying which call failed; nothing from an answer can leak. | Acceptable and simple; a later story may add a cause if the logs prove too thin. |
| A profile refusal collapses each finding to one line | Keeps one line per finding; the collapse is untested (BH-6). | Fine, with the one test of G2. |
| The success line `fork-release: guard: ok: <mode> <tag> at <E>` | Printed once on success. | No issue. |
| `guard --profile release --in-run` still parses as the profile mode | Unchanged old behavior. | No issue. |

**Groups and routes.**
- G1 (BH-2, BH-3 comment part, EH-2, EH-8), low, route **patch**: correct the comment in `releaselookup.go` that says a partial list never reads as None so that it states what the claim covers (a failed, malformed, repeated or unending page walk is Unknown) and what it does not (drafts the token cannot see, Q5; a list that changes while it is read). Comment only. Pending; not applied.
- G2 (BH-6), low, route **patch**: one test where a profile finding's path holds a newline, a tab and repeated spaces, asserting one line per finding. Test only. Pending; not applied.
- Rejected as low (unlikely, and the fix is a new guard or machinery): BH-3, BH-5, BH-7, BH-8, BH-9, BH-10, BH-11, EH-1, EH-3, EH-5, EH-6. Rejected as false: BH-1, BH-4, EH-4, EH-7.
- No intent_gap or bad_spec entry, so no loopback; `review_loop_iteration` stays 0. No deferral.

## Design Notes

- **Decisions made here, not intent gaps.** The spine fixes the order and the decision but not the wire: the in-run modes are the flags `--in-run` and `--recheck` (not a bare `guard`, which the Story 1.2 tests keep as usage); the inputs are the default variables `GITHUB_REF_NAME` and `GITHUB_SHA` plus `GORELEASER_PIN`, the name Story 1.9's workflow will give its single GoReleaser pin; the success line is one `fork-release: guard: ok` style line; a profile refusal prints one line per finding. The list ends at the first empty page rather than at a short page, because the API gives no total; a ceiling of 50 pages guarantees termination. `--recheck` derives `E` from `HEAD` because the first run already proved the identity. The in-run modes do not repeat the Operator-side origin-URL and build-identity checks, which the spine assigns to the Operator-side commands; the fork is public, so the in-run git reads need no credential beyond what `actions/checkout` leaves, and a private repository would need a change in Story 1.9.
- **Real journey.** The Operator pushes a tag after `--pre-push` (Story 1.8); the workflow's `release` job runs `guard --in-run`, then the notes, then `guard --recheck`, then GoReleaser (Story 1.9). A failed build before any release object exists is retried with the same tag and passes; once a draft or public release exists, or a record has landed, every re-run refuses and the Operator records abandonment (Story 1.4) and uses the next `N`.
- **Proposed `FORK.md` › Releasing wording (pull request description only):** "A retry of a Release Tag is safe only when no GitHub release object, draft or public, and no verification record exists for it. If either exists, record the abandonment and use the next explicitly authorized `N`; abandoned releases are never deleted. `FORK.md` does not say this yet; this wording is proposed here." The wording about verification before Handover belongs to Story 1.7.

## Verification

**Commands:**
- `just test-fast` -- expected: exits 0 (all packages but `tests/live`; no credentials, no network)
- `just lint` (full) -- expected: exits 0 and leaves no diff
- `just build` -- expected: exits 0
- In that order, through the isolated pinned toolchain (Go 1.27.0, golangci-lint 2.13.1, GoReleaser 2.17.1, offline). `just snapshot` is not needed: the release surface does not move. These broad gates belong to the controller after the implementation child returns; the child runs only the focused package tests.

**Manual checks (if no CLI):**
- The diff outside `_bmad-output/` lists only `scripts/fork-release/` paths; `.goreleaser.yaml`, `justfile`, `ci.yml`, `devbox.lock`, `go.mod` and `go.sum` are byte-identical.
