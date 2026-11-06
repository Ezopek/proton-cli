---
title: 'Story 1.3: A Release Tag is bound to exactly one commit and its Upstream Base'
type: 'feature'
created: '2026-11-06'
status: 'done'
route: 'dispatch'
baseline_commit: '79f9e0e8968b033ed363cc89636f71d935dac551'
review_loop_iteration: 0
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md'
  - '{project-root}/_bmad-output/planning-artifacts/epics.md'
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-proton-cli-2026-10-25/ARCHITECTURE-SPINE.md'
  - '{project-root}/FORK.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Nothing yet decides whether a Release Tag, the commit it names, the Upstream release it is based on and the build toolchain agree. A wrong, moved or colocated tag, a commit that is not on `origin/ezop`, a wrong `X.Y.Z` or a drifted GoReleaser or Go version could reach a build.

**Approach:** Add the offline checks of AD-3, AD-10 and AD-11 as pure functions in `scripts/fork-release/`: the tag-name pattern, the identity of `HEAD`, `GITHUB_SHA` and the tag on origin, reachability from `origin/ezop`, the computed Upstream Base, and the toolchain pins. They sit behind the Story 1.2 git adapter (one new read-only method, the tags of `origin`) and a small orchestration function runs them in order. Every rejection is proved beside a passing case in scratch repositories. No command is wired here: Stories 1.8 and 1.9 call these checks. Authoritative wording of the criteria: `epics.md`, Story 1.3.

## Boundaries & Constraints

**Always:**
- `FORK.md` and the accepted spine win. Outside `_bmad-output/`, the change touches only files under `scripts/fork-release/`; no `go.mod` or `go.sum` change (standard library only), nothing under `internal/`, `cmd/` or `tests/`, and `.goreleaser.yaml`, `justfile`, `ci.yml`, `devbox.lock` and the existing fixtures stay byte-identical.
- Story 1.1 and 1.2 behavior is kept; no existing assertion is weakened. The only edits to old production code are the new adapter method on `gitRepo` and `execGit`, and the old test fake gains the same method so it still satisfies the interface (a type-only adaptation).
- Checks are pure functions over plain inputs and return the existing one-line `fork-release: <check>: <reason>` refusal. A refusal never prints a URL or raw git output. Anything missing, ambiguous or unreadable refuses.
- Tag pattern: only `^v[0-9]+\.[0-9]+\.[0-9]+-ezop\.[1-9][0-9]*$`, matched against the whole string. `v5.1.0-ezop.0`, `v5.1.0-ezop.1x`, a leading zero in the counter, surrounding whitespace or a newline all refuse.
- Identity: `HEAD`, `GITHUB_SHA` (peeled to a commit) and `refs/tags/TAG` as listed by `origin` (peeled) must be present and all equal one full lowercase 40-hex commit `E`; otherwise refuse, naming which input is missing or differs but printing no URL.
- Reachability: `E` must be equal to or an ancestor of the `origin/ezop` fetched once (`FetchEzop`); only `merge-base --is-ancestor` status 0 passes, status 1 is told apart from every other status and from a failed fetch.
- Upstream Base: candidates are only tags listed by `origin` whose names match `^v[0-9]+\.[0-9]+\.[0-9]+$` and whose peeled commit is `E` or an ancestor of `E`. Ordering is by the numeric triple `(X, Y, Z)` and never by date, topology or name; no candidate, or two names with the same numeric triple, refuses. A candidate whose ancestry cannot be decided refuses as unreadable; a tag that is not an ancestor of `E` is ignored. The Release Tag's `X.Y.Z` must equal the base name without `v` as an exact string. Fork tags and Upstream pre-releases never match the pattern. Tags that exist only locally are never candidates.
- Toolchain: with `go.mod` and `devbox.lock` read from the tree of `E` and the workflow's GoReleaser pin as input (`vX.Y.Z`), exactly one key of `packages` starting with `goreleaser@` has a `version` equal to the pin without `v`, and exactly one key starting with `go@` has a `version` equal to the `go` directive of `go.mod`, compared as exact strings. Zero or several keys, a missing or non-string version, a repeated key, an unreadable or non-regular file, or a malformed pin refuse.
- Tests use scratch repositories in `t.TempDir` with their own git configuration, a filtered environment and a local bare repository as `origin`; no network, no real `origin`, no ref, tag or configuration of this repository, no skip. Inline fixture strings stand for `go.mod` and `devbox.lock`; no test reads the real files. Comments and docs use US spelling and are public-safe. Gates run through the pinned toolchain with the recipes under Verification; never `just test` or a `go test` pattern that reaches `tests/live`.

**Never:**
- No subcommand, flag, `--pre-push`, in-run `guard`, workflow, `fork-release.yml`, notes, assets, stamp, record, verify or status code (Stories 1.4 to 1.9); no actual-tree test (Story 1.9); no execution of the Pre-Tag Gates (FR-9's gate run belongs to the later tag procedure).
- Production code never pushes, creates or deletes a tag or ref and makes no write call to `origin`; the tests create tags only in scratch repositories. No fetch of tags, no lock, state or recovery protocol, no live GitHub or Proton call.
- No `FORK.md` edit, no skipped, weakened or disabled test or gate, no Actions, tag, release, settings or default-branch change.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Tag pattern | `v5.1.0-ezop.1`, `v10.20.30-ezop.12` | accepted | `v5.1.0-ezop.0`, `v5.1.0-ezop.1x`, `v5.1.0-ezop.01`, `5.1.0-ezop.1`, `v5.1-ezop.1`, padded, newline, empty: `fork-release: tag: <reason>` |
| Identity | `HEAD`, `GITHUB_SHA` and the origin tag all peel to `E`; `v5.1.0` or a second `-ezop.N` sits on the same commit | accepted, `E` returned | any input missing, or any differs (moved tag, wrong SHA): `fork-release: identity: <which input>` |
| Reachable | `E` is `origin/ezop` or an ancestor | accepted | ancestry status 1: not reachable; other status or failed fetch: undecidable, told apart |
| Base | origin tags `v5.0.0`, `v5.1.0`, `v5.10.0`... with highest numeric triple that is `E` or its ancestor; unmerged Upstream tags ignored | base found; tag `X.Y.Z` equals it | no candidate, same triple twice, undecidable ancestry, base only local, `v5.2.0-ezop.1` or `v5.0.0-ezop.1` on a `v5.1.0` base: `fork-release: base: <reason>` |
| Toolchain | single `go@` and `goreleaser@` keys equal `go.mod` and the pin | accepted | any deviation above: `fork-release: toolchain: <reason>` |
| Binding | all of the above in order: pattern, identity, reachable, base, toolchain | accepted | the first failing check refuses and stops; nothing is pushed anywhere |

</frozen-after-approval>

## Code Map

- `scripts/fork-release/identity.go` -- `refusal{check, reason}` and its `Error()` (the one-line format), `fullSHA`, `ancestryDecision(status)` (wording is about the helper's own commit; new decisions for `E` get their own wording), `canonicalOrigins`. Reuse; do not change.
- `scripts/fork-release/gitrepo.go` -- `gitRepo` interface and `execGit`: `run`/`bound` (stderr dropped, timeout, `--no-replace-objects` for hash-bound reads), `FetchEzop`, `ResolveCommit` (peels any revision to a full SHA), `IsAncestor` (exit status), `Tree`/`Blob`, `commitTreeFS(g, commit, paths)` (opens only the named regular files; a symlink or gitlink is unreadable). Add `OriginTags() (map[string]string, error)`: `git ls-remote --tags origin`, one line `<oid>\trefs/tags/<name>`, an extra `<name>^{}` line carries the peeled commit and wins; a missing remote or failed call is an error; no local ref is written. Success with no line means no tags.
- `scripts/fork-release/guard.go`, `guard_test.go` -- the old `fakeGit` must gain `OriginTags` to keep satisfying `gitRepo` (type-only); `testDeps` and the scratch helpers `newScratch`, `newBare`, `scratchEnv`, `(*scratch).git/commit/write/adapter` in `gitrepo_test.go` are reusable for the new scratch fixtures.
- New: `tagcheck.go` (tag pattern, identity, reachability decision, base computation, and `checkTagBinding(g gitRepo, tag, githubSHA, pin string)` which runs pattern, resolve of `HEAD` and `GITHUB_SHA`, `OriginTags`, identity, one `FetchEzop` and reachability, base, then the toolchain check on the tree of `E`), `toolchain.go` (the `go.mod` directive scan and the `devbox.lock` key scan), and a test file named after each.
- Probed with the pinned Go 1.27.0 and git 2.43.0 in scratch repositories (private evidence): `ls-remote --tags` lists lightweight tags once and annotated tags twice (`refs/tags/x` with the tag object, `refs/tags/x^{}` with the commit), also for a tag of a tag; asking for `refs/tags/<name>` returns only that tag; an absent tag is exit 0 with no lines (so absence is told from a failed call, which exits 128); a peeled annotated tag works with `merge-base --is-ancestor`; an unknown commit gives status 128.
- Real tree facts, read-only, for the choice of key matching (not for tests): `devbox.lock` keys are `go@latest` and `goreleaser@latest` with `version` `1.27.0` and `2.17.1`; `golangci-lint@latest` does not start with `go@`; `go.mod` says `go 1.27.0`. So the key is matched by prefix and the version by its `version` field, as the criteria say. Nothing in the real files is edited.
- Do not change: `.goreleaser.yaml`, `devbox.lock`, `devbox.json`, `go.mod`, `go.sum`, `justfile`, workflows, the profile code, both Story 1.1 fixtures and the raw `v5.1.0` fixtures.

## Tasks & Acceptance

**Execution** (strict TDD, tests first with no production-first exception: write the tests and observe the missing-API compile failure; only then add the minimal stubs that give a runnable assertion RED and keep that output; then implement; never edit an oracle to make it pass; no skip; one cluster per step):
- [x] `scripts/fork-release/tagcheck.go`, `tagcheck_test.go` -- the tag pattern and the identity check as pure functions, with the passing and refusing cases of the matrix, including colocated tags and a moved tag -- FR-4, AR-4
- [x] `scripts/fork-release/gitrepo.go`, `gitrepo_test.go`, `guard_test.go` -- `OriginTags` on the interface and `execGit`, the fake gains the method; scratch tests with lightweight, annotated and nested-annotated tags, no tags, a missing remote, and tags that exist only locally
- [x] `scripts/fork-release/tagcheck.go`, `tagcheck_test.go` -- reachability from the fetched `origin/ezop` and the Upstream Base computation over scratch origin tags: highest numeric triple, ignored unmerged and excluded names, no candidate, tie, undecidable, local-only, exact `X.Y.Z` match including `v5.2.0-ezop.1` and `v5.0.0-ezop.1` on a `v5.1.0` base, an unreachable commit -- FR-4, AR-11
- [x] `scripts/fork-release/toolchain.go`, `toolchain_test.go` -- the toolchain check over inline `go.mod`, `devbox.lock` and pin strings: one pass and one rejection per predicate in the frozen rule -- FR-5, FR-9, AR-10
- [x] `scripts/fork-release/tagcheck.go`, `tagcheck_test.go` -- `checkTagBinding`: one end-to-end scratch case that passes, and one per failing stage showing the first refusal stops the run and nothing else is called (recorded by a fake)

**Acceptance Criteria:**
- Given a tag name, when the pattern check runs, then only the Release Tag pattern passes and the examples in the matrix refuse with a one-line `fork-release: tag:` message.
- Given `HEAD`, `GITHUB_SHA` and the tag listed by `origin`, when the identity check runs, then it passes only if all peel to one commit, and colocated and moved tags behave as the matrix says in scratch repositories.
- Given origin tags in a scratch repository, when reachability and the base checks run for `E`, then `E` must be on `origin/ezop` and the base is the highest numeric origin tag that is `E` or an ancestor, with every listed rejection beside a passing case and no tag pushed anywhere.
- Given `go.mod`, `devbox.lock` and the pin, when the toolchain check runs, then exactly one `goreleaser@` and one `go@` key must match the pin and the `go` directive, and anything else refuses.
- Given the pull request, when reviewed, then its diff touches only files under `scripts/fork-release/`, the checks are pure functions behind the Story 1.2 adapter, every fixture is a scratch repository or inline string, `just test-fast`, full `just lint` (no diff left) and `just build` pass, and no test reads the real tree. Traces: FR-4, FR-5, FR-9, S1; AD-3, AD-10, AD-11, AD-14.

## Implementation Notes

**Execution roles (set before dispatch; the implementation child's sole source is this spec).**
- The implementation child owns the code and tests, strict TDD, and the focused green result of the package tests. It runs only `go test ./scripts/fork-release/ -count=1` (with `-run`, `-v` or `-json` as needed) using the pinned Go 1.27.0 already on `PATH`, with `GOPROXY=off`, `GOFLAGS=-p=2` and no network. Save each red and green run's complete output together with its true exit status in a file under `$TMPDIR` (never pipe the run into `head` or `tail`, which hides the status; capture the status first), name the files by cluster and phase, and list them in the report.
- Not run by the child, because the controller runs them against the frozen candidate after the child returns: `just test-fast`, full `just lint`, `just build`. The child reports a focused checkpoint and says plainly that broad qualification is incomplete; it must not claim the acceptance criteria are fully verified.
- No mutation of this checkout's git state: no `git add`, `rm`, `reset`, `checkout`, `stash`, `commit`, `push`, `fetch`, tag or ref or configuration change, and no `git` call against the real repository except read-only inspection. Scratch `git init`, `add`, `commit`, `tag`, `update-index`, `push` to a scratch bare repository, `fetch` and `hash-object` are allowed only inside temporary scratch repositories the tests create. The controller owns the commit and the index.
- All old test oracles, inputs and fixtures are preserved; every existing test and subtest keeps its behavior (no count is hard-coded for the future). Allowed paths: `tagcheck.go`, `tagcheck_test.go`, `toolchain.go`, `toolchain_test.go`, `gitrepo.go`, `gitrepo_test.go` and `guard_test.go` under `scripts/fork-release/`; in the old files only the new `OriginTags` method and the matching method on the old fake.
- No new command-line flag, environment variable, subcommand or configuration; no new dependency.
- Leave no background process running and no file outside `scripts/fork-release/` in the checkout.

**Test-first order, exactly.** For a cluster whose API does not exist yet, write the tests first and run them: keep the compile failure (exit status 1, missing names). Only then add the smallest stubs that compile and run the tests again: keep that runnable assertion failure (RED by assertion, not by compile error). Then implement the minimum and keep the green run. For a cluster whose API already exists, the tests must fail by assertion directly; do not invent a compile failure. Never edit a test to make it pass.

**Fixture guidance.**
- Scratch repositories: a local bare repository as `origin`, a work clone, and commits and tags made with a fixed identity and a filtered environment (reuse the helpers in `gitrepo_test.go`). Tags that must be visible to the checks are pushed to the scratch bare repository.
- For the end-to-end passing case the work clone must really hold every object the checks ask for: fetch the origin tags and the `ezop` branch into the scratch clone in the test setup. The "unreadable base" case is a clone that lacks the commit of a listed origin tag, so the ancestry status is neither 0 nor 1; it must not be described as a tag missing from the remote.
- "A base that exists only locally" is a tag made in the work clone and never pushed. Annotated, lightweight and nested-annotated tags must each appear. `HEAD` and `GITHUB_SHA` are passed as plain inputs; the colocated and moved-tag cases are built by tagging several names on one commit and by moving a pushed tag in the bare repository.
- `go.mod` and `devbox.lock` are inline strings in the toolchain tests and in the end-to-end tree; do not read the real files. A `devbox.lock` string needs the shape `{"lockfile_version":"1","packages":{"go@latest":{"version":"1.27.0"},...}}`.

**Implementation record (first pass, before the broad gates).**
- Seven paths under `scripts/fork-release/`: new `tagcheck.go`, `toolchain.go` and their test files; `gitrepo.go` gains `OriginTags` (read-only `ls-remote --tags origin`, a peeled `^{}` line wins, a duplicate or malformed line is an error) and the helper that parses its output; `gitrepo_test.go` is extended; the old fake in `guard_test.go` gains the method (type-only). No old test line was removed.
- Choices made by the implementation, within the frozen intent: `OriginTags` first reads the origin URLs so that a missing `origin` cannot make git treat the word as a path; any tie among candidates refuses, not only a tie at the top; the toolchain scan also refuses trailing data, a repeated `packages` key, a repeated package key or a repeated `version`.
- The focused package run passed 972 distinct tests and subtests with none failed or skipped; the 782 that existed before all still pass and 190 are new. The test-first order was kept for all five clusters with a compile failure, a runnable assertion failure and a green run each (the compile-failure output of the second cluster was overwritten when the same command was re-run after a test-helper fix, so that one is the later run). A moved-tag test first failed because its scratch origin lacked a commit; the setup was fixed, not the assertion.
- **Verification record.** The controller then ran `just test-fast`, full `just lint` and `just build` on the candidate, each exit 0, under a lowered memory and parallelism setting (one process, one build job, a memory limit) with unchanged input hashes and modes. Two earlier attempts at the same gates were stopped by the host's memory limit; they are kept as failed attempts and count for nothing. No snapshot (the release surface does not move), no native Windows run; `just lint` includes the cross-OS builds.
- **Task and matrix audit (author).** All five tasks are done in the diff. Each matrix row has executed, passing, non-skipped tests among the 972 focused IDs: tag pattern (27 pattern IDs), identity (14, plus the binding cases for colocated and moved tags), reachable (9), base (19 base, 11 match, 1 compare, and the binding cases for `v5.2.0`/`v5.0.0` on a `v5.1.0` base), toolchain (52 plus 10 tree-read IDs), binding order (21). The criterion about the pull request itself is future.

**Correction after the independent review (verdict COMMENT: no blocking defect, three minor residuals; not a native loopback).** Test and comment changes only; no production behavior changed.
- *Wording of the frozen constraints.* The frozen "Never" line says production code never "creates ... a tag or ref", while the same frozen text requires the single `FetchEzop` for reachability. That fetch updates local bookkeeping: `refs/remotes/origin/ezop` and `FETCH_HEAD`. The draft's sentence overclaimed and was written by the planning agent, not chosen by the Operator. Read with the rest of the intent, its purpose is that nothing creates, moves or pushes a tag and nothing is written to `origin`; the required fetch's local bookkeeping is the specified mechanism. The frozen block is not changed here and no waiver is attributed to anyone; the controller adjudicates the reading from the epic and the architecture.
- The comment of `checkTagBinding` now says exactly that, and the test that compared refs is renamed "no tag or remote ref is written" (its clone already had the tracking ref). A new control deletes the tracking ref first and shows that the required fetch recreates it and `FETCH_HEAD`, while local tags and branches and every ref of `origin` stay unchanged; it began green because it describes existing behavior.
- The symlink cases of the toolchain tests now make the link in Git's index (mode 120000 over a valid regular target), preceded by a regular-commit control and followed by a check that the entry is a link, one case per file, and the refusal must name the file kind. The executable-file case sets its bit in the index too. No operating-system symlink or permission call remains, so the setup no longer depends on a host privilege. This was exercised on Linux only; no native Windows run was made. The old combined symlink case is replaced by two cases with new IDs.
- One subtest was added to the base tests: a tie (`v5.0.0` and `v05.0.0`) below a higher untied tag (`v5.9.0`) refuses. It began green. A private mutation that limits the tie check to the top pair makes it fail, and a mutation that treats links as files makes the symlink cases fail; removing only the regular-file check in the reader changes nothing, because the tree view already refuses to read a link.
- The focused package run passed 975 distinct tests and subtests, none failed or skipped. The earlier gate passes bind the tree before this correction.

**Completion** (native present step). The first independent review returned COMMENT (no blocking defect, three minor residuals), kept as history. After the test and comment correction recorded above, a focused renewal returned APPROVE with all three residuals addressed. The controller ran `just test-fast`, full `just lint` and `just build` on the corrected bytes, each exit 0 under a lowered memory limit; earlier attempts stopped by the host's memory limit, and one diagnostic that tried module downloads outside the offline setup, stay disclosed as failed or unsuccessful. The focused package run passed 975 distinct tests and subtests with none failed or skipped. No native Windows run was made. The frozen block is unchanged: its wording about never creating "a tag or ref" overclaimed against the fetch the same text requires, and the nonfrozen notes record the reading that was adopted (no tag created, moved or pushed; nothing written to `origin`; only the local bookkeeping of the required fetch). Two test-process deviations stand as facts: the retained compile-failure log of the second cluster is a later re-run, and the first-cluster helper-name correction and the moved-tag setup correction are known only from the child's report. Nothing is deferred and no backlog is added. A done spec is not a done sprint entry, a completed epic or a release; nothing was published, and the checks have no production caller until Stories 1.8 and 1.9.

## Spec Change Log

## Review Triage Log

First review pass (native step 4: three context-free layers, same model capability, launched together). The blind layer's floor and the edge layer's scenarios are not proven defects; each row is the result of checking the claim against the code, its callers and the accepted architecture. Scratch probes (not governed tests) confirmed two claims: a shallow clone, and the effect of the single fetch of `ezop`.

| ID | Finding (short) | Verdict | Evidence |
|----|-----------------|---------|----------|
| BH-1 | A failed resolve of `HEAD` or `GITHUB_SHA`, and a failed tag listing, lose their cause in `checkTagBinding` | low | Real but only wording: every case still refuses at `identity`, and an unknown well-formed `GITHUB_SHA` reads "missing". Unlikely, and the fix is a taxonomy of reasons. Same root as EH-4. |
| BH-2 | A `toolchain` line in `go.mod` can differ from the pinned Go | low | The accepted rule compares the `go` directive and ignores `toolchain`; adding a `toolchain` line is a reviewed fork edit, the real file has none, and the later workflow step checks `go version`. Changing the rule would change AD-10. |
| BH-3 | The Upstream tags on the fork are an unprotected trust root | false | AD-11 states the provenance explicitly (Operator-only writes, the tag ruleset required before activation, the peel comparison with `upstream`); those belong to other stories. |
| BH-4 | Two `recGit` assertions are weak (`urls: 0`, first-and-last order only) | low | The order is pinned by the refusal cases, which assert that `fetch`, `ancestor` and `tree` are not reached; the weak assertions are cosmetic. |
| BH-5 | `checkTagBinding` has no production caller; the caller contract is unenforced | false | The spec excludes any command or workflow (Stories 1.8 and 1.9 call it) and records the caller's duties. |
| BH-6 | A Release Tag on an old ancestor of `origin/ezop` passes | false | The frozen criterion is "an ancestor of `origin/ezop`"; the test asserts exactly that. |
| BH-7 | No bounds on process spawns, blob size or listing size | low | Tens of Upstream tags and two small files; no harm path was shown. Speculative hardening. |
| BH-8 | SHA-256 repositories, non-commit peels, parser corner cases | low | The fork is a SHA-1 repository; a non-commit peel refuses at the ancestry step. Speculative. |
| EH-1 | `checkTagBinding` is documented as "reads and never writes", but the one `FetchEzop` creates or moves `refs/remotes/origin/ezop` and `FETCH_HEAD`; the test passes because its clone has the ref already | low | Probe confirms the fetch creates the remote-tracking ref when absent. It is the Story 1.2 adapter call that the spec requires, so behavior is as intended, but the comment and the test name overstate it. Direct fix: correct the comment and the test name. Judged not a violation of the spec's "never creates a tag or ref", which is about tags and `origin`. |
| EH-2 | A shallow clone makes `is-ancestor` return 1 for a real ancestor, so a lower base can be chosen | low | Probe confirms status 1 in a depth-1 clone with the tag object present. The criterion's caller contract is full history (the later workflow's pinned checkout) and a lower base still has to equal the tag's `X.Y.Z`. A guard would be new machinery for a caller misconfiguration; recorded as a duty of Story 1.9. |
| EH-3 | The tag listing is a snapshot; a tag moved after it still passes | low | A run is one pass with no lock by design; the built tree is the checked-out `HEAD`, not the tag. |
| EH-4 | Resolve errors are blanked to "missing" | low | Same root as BH-1. |
| EH-5 | A quoted `go "1.27.0"` directive is compared literally | low | It refuses (fail closed); the real file is plain. |
| EH-6 | `1.27` versus `1.27.0` gives a bare mismatch | low | The exact-string rule is the criterion; the refusal names the expected value. |
| EH-7 | `ls-remote` may read a different URL than the one validated | false | The caller's earlier step requires every effective fetch URL to be canonical; `OriginTags` only reads `origin`. |
| VG-1 | A tie below a higher untied Upstream tag is not tested | low | Confirmed: the one tie test has only two candidates, so a weakening to "tie at the top" would pass. The strict rule is a disclosed choice. Fix is one test. |
| VG-2 | Nothing calls the new checks in production | false | By design (see BH-5). |
| VG-3 | The old fake's `OriginTags` is unused except to satisfy the interface | low | That is its specified purpose (type-only); no harm. |

**Groups and routes.**
- G1 (EH-1), low, route **patch**: correct the "never writes" comment of `checkTagBinding` and the matching test name so they say what is true (no tag, nothing pushed; the single fetch may update the remote-tracking ref and `FETCH_HEAD`). Comment and test name only, no behavior change. Pending; not applied in this pass.
- G2 (VG-1), low, route **patch**: add one subtest to `TestUpstreamBase` with a tie below a higher untied tag (for example `v5.0.0`, `v05.0.0` and `v5.9.0` with `E` on top), expecting the `base` refusal about the same version. Test only. Pending; not applied.
- Rejected as low with a larger or speculative fix: BH-1, BH-2, BH-4, BH-7, BH-8, EH-2, EH-3, EH-4, EH-5, EH-6, VG-3. Rejected as false: BH-3, BH-5, BH-6, EH-7, VG-2.
- No intent_gap or bad_spec entry, so no loopback; `review_loop_iteration` stays 0. No deferral.
- **After the independent review.** G1 and G2 were applied as test and comment corrections, together with two minor residuals of the independent review (the index-built symlink cases and the executable-file case). Their rows above are left as graded; the reading of the frozen "never creates a ref" line is recorded in Implementation Notes for the controller.

## Design Notes

- **Decisions made here, not intent gaps.** The checks are library functions for Stories 1.8 and 1.9 to call; the origin-URL identity check (Story 1.2) is the caller's step and is not repeated. Origin tags are read with `ls-remote`, which reflects the remote and writes nothing locally; the objects of those tags must already be present, because the caller has fetched (the in-run job checks out with full history, and the pre-push command fetches tags first). A candidate tag whose commit object is missing refuses as unreadable instead of being guessed. Numeric ordering compares the digit strings after removing leading zeros, by length then text, so there is no overflow; two spellings of one triple (`v5.1.0` and `v05.1.0`) are a tie and refuse. The `go` directive is read by a line scan (exactly one `go` line; a `toolchain` line is ignored). `devbox.lock` keys are counted from the token stream so a repeated key is not hidden by decoding. Refusal check names: `tag`, `identity`, `reachable`, `base`, `toolchain`.
- **What this does not do.** It does not run the Pre-Tag Gates, does not read the real lock file in a test (the real-tree test lands in Story 1.9), and does not fetch. A wrong origin remote is refused by the caller's earlier step.

## Verification

**Commands:**
- `just test-fast` -- expected: exits 0 (all packages but `tests/live`; no credentials, no network)
- `just lint` (full) -- expected: exits 0 and leaves no diff
- `just build` -- expected: exits 0
- In that order, through the isolated pinned toolchain (Go 1.27.0, golangci-lint 2.13.1, GoReleaser 2.17.1, offline). `just snapshot` is not needed: the release surface does not move. These broad gates belong to the controller after the implementation child returns; the child runs only the focused package tests with `GOFLAGS=-p=2`.

**Manual checks (if no CLI):**
- The diff outside `_bmad-output/` lists only `scripts/fork-release/` paths; `.goreleaser.yaml`, `justfile`, `ci.yml`, `devbox.lock`, `go.mod` and `go.sum` are byte-identical.
