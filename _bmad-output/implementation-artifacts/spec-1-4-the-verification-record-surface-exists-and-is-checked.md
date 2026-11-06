---
title: 'Story 1.4: The Verification Record surface exists and is checked'
type: 'feature'
created: '2026-11-06'
status: 'done'
route: 'dispatch'
baseline_commit: '54996ec5b85a69a54914a1277b2089470dbb93f9'
review_loop_iteration: 0
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md'
  - '{project-root}/_bmad-output/planning-artifacts/epics.md'
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-proton-cli-2026-10-25/ARCHITECTURE-SPINE.md'
  - '{project-root}/FORK.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A Verified Fork Release must end in one terminal, sanitized record on `ezop` that tools can find and that a typo cannot corrupt, but neither the record format, its README, a check of hand-committed records, nor the lookup that finds a record on the live `ezop` exists.

**Approach:** Add, in `scripts/fork-release/`, the record grammar with its closed key set (the helper source owns it), a validator for every file under `fork/verification/`, and the one record lookup, which reads the live tip of `ezop` through the GitHub API and walks commit, tree and blob with the documented non-recursive endpoints. Add `fork/verification/README.md`, which fixes the field names, and run the validator and a README-against-key-set test over the real tree, as the README is their only prerequisite. Authoritative wording of the criteria: `epics.md`, Story 1.4.

## Boundaries & Constraints

**Always:**
- `FORK.md` and the accepted spine win. Outside `_bmad-output/`, the change touches only files under `scripts/fork-release/` and the new `fork/verification/README.md`; no `go.mod` or `go.sum` change (standard library only), nothing under `internal/`, `cmd/` or `tests/`, and no other existing file. Earlier stories' behavior and assertions are kept.
- The record is plain text: UTF-8 restricted to printable ASCII, LF line ends, one `key: value` per line, no blank line, no comment, a final LF, no repeated key, and no key outside the closed set (the pattern keys `asset.<name>` and `attested.<key>` included). Every value is one line without leading or trailing space; only `exceptions` may be empty. The file never holds raw API output, credentials, account data, local paths or personal content.
- Order and fields: `schema` (value `1`), `tag`, `status` (`verified` or `abandoned`) and `time` (UTC, `YYYY-MM-DDThh:mm:ssZ`, a real instant) come first, in that order. `tag` equals the file name without `.md` and matches the Release Tag pattern. `abandoned` adds exactly one more key, `reason`, from the closed list `refused`, `failed`, `draft`, `window-passed`, `rebased`, `operator-decision`. `verified` adds, in this order: `commit` (`E`, 40 lowercase hex); one `asset.<name>` per downloaded asset with its sha256 (64 lowercase hex), names sorted and unique; `exceptions` (comma-separated asset names that appear among the assets, sorted, possibly empty); `host-platform`; `proton-version` (equal to `tag` without its `v`); `goreleaser-config-blob` and `devbox-lock-blob` (40 hex); `go-version` and `goreleaser-version` (dotted numbers); `run-id` and `run-attempt` (positive integers) and `run-conclusion` (must be `success`); and the attested gate evidence from the gate-capture file as `attested.gate-commit` (must equal `commit`), `attested.gates` (exactly `test-fast,lint,build,snapshot`), `attested.lint-diff` (exactly `none`), `attested.go-version`, `attested.goreleaser-version` and `attested.snapshot-version`. The semantic completeness of a record (all six binaries present, the observed values) belongs to `verify` in Story 1.7; the validator judges shape and the stated invariants only.
- The validator takes a file system rooted at the repository root and returns findings in the existing `Finding` form. Under `fork/verification/` every entry other than `README.md` must be a regular file named by the Release Tag pattern plus `.md` that parses as one valid record naming that tag; `README.md` must exist as a regular file. A non-regular entry (directory, symlink, submodule), a name outside the pattern, a malformed record, a record naming another tag, `attested.gate-commit` not equal to `commit` and a `run-conclusion` other than `success` each produce a finding. A finding names the path, the rule and the key or line, never the file's content.
- The helper source owns the key set: one list in code is used by the grammar, and a `just test-fast` test compares it with the README in both directions. The README holds sanitized field names, the reason codes and the rules of the record only.
- The lookup is a pure decision over a thin `gh` adapter. It reads the live tip of `ezop` in the repository `Ezopek/proton-cli` (owner and repository are constants, never `GH_REPO`) and resolves it to one commit SHA, then walks that commit with the documented endpoints only: the commit, then its tree, then the trees of `fork` and `verification` (each non-recursive), then the blob of the record. It returns one state with the commit SHA: **Present** (the entry `<TAG>.md` is a regular file whose blob was read and whose bytes hash, as a git blob, to the entry's id; the bytes are returned), **Absent** (no such entry while `README.md` is a regular file in the same directory of the same commit), **NotFile** (an entry of that name exists and is a directory, symlink or submodule) or **Unknown** (everything else: a failed or malformed response, a tip that is not a commit, an id mismatch, a truncated tree at any level, a missing `fork` or `verification` directory or README, a repeated entry name, a bad blob encoding, size or hash). The contents endpoint is not used.
- Every `gh` call passes `--method GET`, the header `X-GitHub-Api-Version: 2026-03-10` and a path built from the constants and validated SHAs and names; a test pins the exact argv. Tests never run `gh`, touch the network, a real remote or this repository's refs; the adapter is replaced by a fake that serves canned JSON. The real-tree test reads only `fork/verification/` of the repository tree and lands in this story because its only prerequisite, the README, lands here.
- Comments, docs and the README use US spelling and are public-safe. Gates run through the pinned toolchain with the recipes under Verification; never `just test` or any `go test` pattern that reaches `tests/live`.

**Never:**
- No `verify`, `verify --status`, refusal decision, release lookup, notes, assets, stamp, `--pre-push`, workflow or runbook code (Stories 1.5 to 1.10); no record file for any tag; no `FORK.md` edit (the wording below travels in the pull request description); no live GitHub, Proton or authenticated call; no write call of any kind; no new dependency, lock, service or state.
- No skipped, weakened or disabled test or gate; no Actions, tag, release, settings or default-branch change.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Valid tree | `README.md`, one valid `verified` record and one valid `abandoned` record of the right names | no findings | N/A |
| Surface | missing `fork/verification`, missing or non-regular `README.md` | a finding | N/A |
| File kind and name | directory, symlink or submodule entry; `v5.1.0.md`, `notes.md`, `v5.1.0-ezop.0.md`, `v5.1.0-ezop.1.txt` | a finding per entry | N/A |
| Malformed record | empty file; first four keys missing, reordered or repeated; unknown key; bad `time`, hex, number or reason; CRLF; no final LF; non-ASCII; wrong key order or extra key for the status | a finding naming the rule and key/line | no content echoed |
| Invariants | `tag` differs from the file name; `attested.gate-commit` differs from `commit`; `run-conclusion` is not `success`; `attested.gates` or `attested.lint-diff` wrong; `proton-version` differs from the tag; `exceptions` names an unlisted asset | a finding | N/A |
| README against keys | README names a key the helper lacks, or the helper has a key the README lacks (fields, patterns and reasons) | the test fails | N/A |
| Lookup Present | tip commit, trees and blob served; entry is a regular file; bytes hash to the entry id | Present, commit SHA, bytes | N/A |
| Lookup Absent | no `<TAG>.md` entry; `README.md` regular file in the same directory | Absent, commit SHA | N/A |
| Lookup NotFile | entry is a directory, symlink or submodule | NotFile, commit SHA | N/A |
| Lookup Unknown | any failure, malformed, mismatching or truncated answer at any step; README or a directory missing | Unknown | no raw response or URL in any message |
| Adapter | each call | argv is exactly `api --method GET -H X-GitHub-Api-Version: 2026-03-10 repos/Ezopek/proton-cli/<endpoint>` | a path from an unvalidated name is refused before `gh` runs |
| Real tree | the repository's `fork/verification/` holding only `README.md` | validator passes; README matches the key set | N/A |

</frozen-after-approval>

## Code Map

- `scripts/fork-release/finding.go` -- `Finding{Path, Rule, Detail}` and `sortFindings`: reuse for validator output. `scripts/fork-release/identity.go` -- `refusal`, `fullSHA`. `scripts/fork-release/tagcheck.go` -- `checkTagPattern` and `releaseTagPattern`: reuse for names and the `tag` key. Do not change these.
- `scripts/fork-release/gitrepo.go`, `gitrepo_test.go` -- the git adapter style (`run` with stderr dropped, timeout, an interface that tests replace) and the scratch helpers `newScratch`, `newBare`, `scratchEnv`, `(*scratch).git/commit/write`, plus `blobID` in `profile_test.go` (SHA-1 of `blob <size>\0<data>`): reuse; the local blob-id check needs no new helper in production beyond the same SHA-1 computation.
- New: `record.go` (key set, grammar, `parseRecord`, `validateVerificationDir`), `recordlookup.go` (the `gh` adapter interface and exec implementation, the walk, `lookupRecord`), a test file named after each, and `fork/verification/README.md`. The real-tree tests live in `record_test.go` and read `../../fork/verification` through a file system rooted at the repository root.
- Facts established read-only (private evidence): the repository has no `fork/` directory yet, and `just lint` runs no markdown check, so a new README adds no lint rule. The accepted spine fixes: first four keys and the closed reason list; the `verified` content (tag and commit, asset hashes and `exceptions`, host platform and `proton version`, the tool identity read from `E`'s tree, the attested gate evidence with `gate-commit` equal to `E`, the run id, attempt and a `success` conclusion); presence-only reading by the refusal; Present, Absent, NotFile and Unknown as the lookup states; the commit, tree and blob fallback with a truncated tree as Unknown (Q6, AQ-4).
- GitHub documentation (official pages for the four endpoints, read by the controller from cached copies, no API call): `GET /repos/{owner}/{repo}/git/ref/{ref}` takes `heads/<branch>` and answers 404 for a missing ref, and its example response has `object.type` (`commit`) and `object.sha`; `git/commits/{commit_sha}` and `git/trees/{tree_sha}` exist, the tree response has `tree[]` entries with `path`, `mode`, `type`, `sha` and a `truncated` flag (when true, fetch sub-trees one at a time; the documented 100,000-entry and 7 MB limits apply to the recursive form); `git/blobs/{file_sha}` returns `content` as base64 with `encoding`, `size` and `sha` (up to 100 MB); the examples send `X-GitHub-Api-Version: 2026-03-10`. Documentation examples are not proof of live behavior, and the first real observation (Q6) stays in the future. The implementation parses these shapes strictly, ignores unknown extra fields, and maps a response that does not fit them to Unknown, without turning an ordinary valid response into Unknown.
- Do not change: any other existing file, the `release`/`mirror` profile code, the tag and toolchain checks.

## Tasks & Acceptance

**Execution** (strict TDD, tests first with no production-first exception: write the tests and observe the missing-API compile failure; only then add the minimal stubs that give a runnable assertion RED and keep that output; then implement; never edit an oracle to make it pass; no skip; one cluster per step):
- [x] `scripts/fork-release/record.go`, `record_test.go` -- the key set, `parseRecord` and the per-file rules, with one passing and one rejecting case for each predicate of the frozen rules (first four keys, reasons, each `verified` key and its value rule, pattern keys, ordering, characters, line ends, repeated and unknown keys, the three invariants) -- AD-7, AD-14
- [x] `scripts/fork-release/record.go`, `record_test.go` -- `validateVerificationDir` over scratch trees (`fstest.MapFS` with modes for directory, symlink and gitlink-like entries): the valid tree, the surface cases, file kind and name, malformed and invariant cases, each beside a passing record -- AD-7, AD-14
- [x] `scripts/fork-release/recordlookup.go`, `recordlookup_test.go` -- the adapter interface and the exec implementation behind it, the pinned argv test, the walk and the four states over a fake that serves canned JSON, including every Unknown cause named in the frozen rules, a truncated tree at each of the three levels, the local blob-id check and a path built from an unvalidated name refused before any call -- AR-5, AR-8, AQ-4
- [x] `fork/verification/README.md` and the real-tree tests in `record_test.go` -- write the README (purpose, the immutability and landing rules, the field list with one bullet per key, the pattern keys and the reason list), then the tests: the validator over the real `fork/verification/` passes and the README matches the key set in both directions -- AQ-3, AR-3, AR-15, NFR-6
- [x] Proposed `FORK.md` wording -- drafted in Design Notes (done); delivery in the pull request description is future and not part of this diff

**Acceptance Criteria:**
- Given the record design, when this story lands, then `fork/verification/README.md` fixes the fields and reasons exactly as the helper's key set does, and the test that compares them fails if either side changes alone.
- Given files under `fork/verification/`, when the validator runs, then every file other than `README.md` must be a regular file named by the Release Tag pattern plus `.md` holding one valid record that names that tag, and each failure listed in the matrix is a finding, proved in scratch trees beside a passing record.
- Given the record lookup, when it runs against a fake serving the documented answers, then it reads the live tip of `ezop`, resolves one commit, and returns Present, Absent, NotFile or Unknown with the commit SHA through non-recursive calls, every call is a `GET` with the API-version header and a pinned argv, a truncated tree and every doubt are Unknown.
- Given the real repository tree after this story lands, when `just test-fast` runs, then the validator passes over the real `fork/verification/` and the README matches the helper's key set.
- Given the pull request, when reviewed, then its diff touches only files under `scripts/fork-release/` and the new `fork/verification/README.md`, `just test-fast`, full `just lint` (no diff left) and `just build` pass, and the description carries the proposed wording. Traces: FR-9, FR-18, S1; AD-7, AD-14, AD-15.

## Implementation Notes

**Execution roles (set before dispatch; the implementation child's sole source is this spec).**
- The implementation child owns the code, the tests and the README, strict TDD, and the focused green result of the package tests. It runs only `go test ./scripts/fork-release/ -count=1` (with `-run`, `-v` or `-json` as needed) using the pinned Go 1.27.0 already on `PATH`, with `GOPROXY=off`, `GOFLAGS=-p=1`, `GOMAXPROCS=1`, `GOMEMLIMIT=768MiB` and no network. Save each red and green run's complete output together with its true exit status in a file under `$TMPDIR` (never pipe a run into `head` or `tail`, which hides the status; capture the status first), name the files by cluster and phase, and list them in the report.
- Not run by the child, because the controller runs them against the frozen candidate after the child returns: `just test-fast`, full `just lint`, `just build`. The child reports a focused checkpoint and says plainly that broad qualification is incomplete; it must not claim the acceptance criteria are fully verified.
- No mutation of this checkout's git state: no `git add`, `rm`, `reset`, `checkout`, `stash`, `commit`, `push`, `fetch`, tag, ref or configuration change, and no `git` call against the real repository except read-only inspection. Scratch `git` only inside temporary repositories the tests create. **Never run `gh`, any GitHub API, Proton or any network call.**
- Allowed paths: new `scripts/fork-release/record.go`, `record_test.go`, `recordlookup.go`, `recordlookup_test.go` and new `fork/verification/README.md`. Nothing else, including all existing tests, fixtures, the other sources and `testdata/`. Every existing test and subtest keeps its behavior. No record file for any tag, no command, flag, environment variable or dependency.
- Leave no background process running and no file outside the allowed paths in the checkout.

**Test-first order, exactly.** For a cluster whose API does not exist yet, write the tests first and run them: keep the compile failure (exit status 1, missing names). Only then add the smallest stubs that compile and run the tests again: keep that runnable assertion failure (RED by assertion, not by compile error). Then implement the minimum and keep the green run. For the README cluster the APIs exist: write the real-tree tests first and keep their failure by assertion while `fork/verification/README.md` is absent, then write the README and keep the green run; do not invent a compile failure. Never edit a test to make it pass.

**Fixture and design guidance.**
- Records in tests are built from Go strings (a helper that builds a valid `verified` record and a valid `abandoned` record, then mutated per case, with one healthy control beside each rejection). The validator tests use `fstest.MapFS` with explicit modes for directory, symlink and gitlink-like entries. Reuse `checkTagPattern`, `Finding`, `refusal`/`fullSHA` and the test helper `blobID`.
- The lookup is a function over an interface with one method that performs a `GET` of a path under the repository and returns the body; the production implementation runs `gh` with the exact argv of the frozen rules, with the `gh` program name injectable. A fake serves canned JSON by endpoint. To test the real exec adapter without `gh` or the network, run the test binary itself as a stand-in `gh` (the helper-process pattern with an environment variable), record its argv and print canned JSON; this must also work without a shell.
- Present needs the record bytes: decode the base64 `content` (it contains line breaks), require `encoding` `base64`, `size` equal to the byte length, and the git blob id of the bytes equal to the tree entry's id. The three tree walks (root, `fork`, `verification`) use the id of the previous entry; a `truncated` flag at any level is Unknown; a tree entry counts as a regular file only for type `blob` and mode `100644` or `100755`.
- Do not bake a caller's state into the lookup: it returns the state, the commit SHA and, for Present, the bytes; it makes no refusal or status decision.
- The README lists each key of the code's key set as one bullet starting with a backticked key, including the pattern keys written as `asset.<name>` and `attested.<key>` placeholders, and each reason code in its own list; the key-set test compares the sets in both directions.

**Implementation record (first pass, before the broad gates).**
- Five new files only: `scripts/fork-release/record.go`, `record_test.go`, `recordlookup.go`, `recordlookup_test.go` and `fork/verification/README.md`. No existing file, no dependency, no record for any tag, no `gh` or network call.
- Choices made by the implementation, within the frozen intent: the closed key set lists the `attested.*` fields as concrete keys beside the two pattern entries; an empty `exceptions` is written `exceptions:` and a trailing space after the colon is a finding; Absent needs `README.md` as a regular file, while Present and NotFile do not; the adapter drops `GH_REPO` and `GH_HOST`, turns prompts off, caps an answer at 8 MiB and times out after 60 seconds; Unknown carries a reason from a fixed vocabulary and never a raw response; the validator also reports a record file larger than 64 KiB (`record-size`), which the criteria did not ask for. The exec adapter is tested by running the test binary itself as a stand-in `gh`, through a package-level `TestMain` hook in `recordlookup_test.go` that acts only when a private environment variable is set.
- The focused package run passed 1,192 distinct tests and subtests with none failed or skipped; the 975 that existed before all still pass and 217 are new. Test-first order: clusters 1 to 3 kept a compile failure, a runnable assertion failure and a green run each; the README cluster has no compile failure by design, only the real-tree failure while the README was absent, then green.
- **Verification record.** The controller then ran `just test-fast`, full `just lint` and `just build` on the candidate, each exit 0, under a lowered memory and parallelism setting with unchanged input hashes and modes. No live GitHub, `gh`, Proton or native Windows run; the lookup has been exercised only against canned documented-shape answers and a stand-in program.
- **Task and matrix audit (author).** All five tasks are done in the diff. Every matrix row has executed, passing, non-skipped tests among the 1,192 focused IDs: valid tree (15), surface (7), file kind and name (12), malformed record (91), invariants (13), README against the key set (5, with the real-tree validator 1), lookup Present (3), Absent (1), NotFile (5), Unknown (57), adapter argv and path guard (10). The criterion about the pull request itself is future.

**Completion** (native present step). The three-lens native review and its triage are kept as they stand. The independent review returned COMMENT (no blocking correctness or security finding, one minor documentation discrepancy), accepted as COMMENT and not read as an approval. The controller ran `just test-fast`, full `just lint` and `just build` on the product bytes, each exit 0 under a lowered memory limit; the focused package run passed 1,192 distinct tests and subtests with none failed or skipped. Two optional extra tests named in the review (a symlinked `fork` or `fork/verification`, and the DEL byte) are not mandated and add no backlog, since the source already handles both. After the review, `fork/verification/README.md` was clarified to match the code it describes: an asset name starts with a letter or digit, continues with letters, digits, `.`, `_` or `-`, and is at most 100 characters; sorting is bytewise; a record file over 64 KiB is a finding, checked after the file is read (it is not a memory bound). That was a documentation edit only: no source or test file changed, and the existing README-against-key-set test and the real-tree validator test, run alone afterwards, passed. No real GitHub API, `gh`, Proton or native Windows run was made; the lookup was exercised only against canned documented-shape answers and a stand-in program. Nothing is deferred. A done spec is not a done sprint entry, a completed epic, a release or production readiness; the lookup and validator have no production caller until Stories 1.5 and 1.7.

## Spec Change Log

## Review Triage Log

First review pass (native step 4: three context-free layers, same model capability, launched together). The blind layer's floor and the edge layer's scenarios are not proven defects; each row is the result of checking the claim against the code, its callers and the accepted architecture and the official endpoint documentation read by the controller. The verification-gap findings arrive pre-verified. No real API, `gh` or network was used.

| ID | Finding (short) | Verdict | Evidence |
|----|-----------------|---------|----------|
| BH-1 | `host-platform`, `attested.snapshot-version` and `proton-version` are free printable text, so the "no local paths" rule is not enforced | low | The values are produced by `verify` (Story 1.7) and reviewed in the record's pull request; `proton-version` is already tied to the tag. A shape rule per value is new policy beyond the criteria. |
| BH-2 | The `gh` call does not pin the host; a non-default `gh` host would be asked for the repository | low | Needs an operator whose default `gh` host is another host that also serves a repository of that name; the answer would still have to pass every structural check. The frozen argv names no host option. |
| BH-3 | Every `gh` failure collapses to "call failed" | low | Unknown is the safe answer and the reason vocabulary is fixed on purpose; a finer set would be new surface with no caller yet. |
| BH-4 | A tip answer without a `ref` field is accepted | low | The path pins `heads/ezop`, the documented answer carries `ref`, and nothing wrong is accepted. See the choices table: accepting an absent optional field is the robust reading. |
| BH-5 | Variable `fs` shadows the `io/fs` import; `crypto/sha1` may be flagged by a security linter | false | The controller's full `just lint` (golangci-lint 2.13.1, standard set) exited 0; SHA-1 here is the git object id, as in the existing tests. |
| BH-6 | The README states rules the diff does not enforce (immutability, spent tags, rebase) | false | They are procedure rules of the Git workflow, carried by the pull request wording; the README says so. History checks belong to later stories and to review. |
| BH-7 | The validator is not wired in and can be bypassed with Actions off | false | The gate is `just test-fast`, run by the Operator (AD-4's manual ledger); the criteria require exactly that. |
| BH-8 | The validator reads the working tree, not the committed tree | false | `just test-fast` checks the candidate tree by design; checking the landed tree is the profile and `verify` path of later stories. |
| BH-9 | Version patterns reject pre-release spellings; the pairs of Go and GoReleaser versions are not related | low | Real values are plain dotted numbers (`1.27.0`, `2.17.1`); relating the two sources is not in the frozen rules. |
| BH-10 | The README says less than the code enforces (64 KiB cap, byte order of "sorted", asset-name limits, hidden entries) | low | Real: the README is the document that fixes the format, and it omits limits the code applies. Direct fix: README sentences (group G3). |
| BH-11 | Value faults are reported unevenly (early return after a bad `status`; a mid-record gap reads as an order fault) | low | Cosmetic; each case is still a finding naming the line or key. |
| BH-12 | A verified record may list no assets | low | The frozen text leaves completeness to `verify`; a hollow hand-made record is not a plausible typo, and a floor would be a new rule. |
| EH-1 | The 64 KiB cap is checked after the whole file is read, so it does not bound memory | low | True; the cap only produces a finding. The files are the Operator's own committed records. See the choices table and G3. |
| EH-2 | Free-text values (same as BH-1) | low | See BH-1. |
| EH-3 | A `run-id` with hundreds of digits passes | low | No producer can emit one; later numeric parsing is Story 1.7's. |
| EH-4 | A missing `ref` is accepted | low | Same as BH-4. |
| EH-5 | No `--hostname` option | low | Same as BH-2. |
| EH-6 | Present is returned without a README check, Absent needs one | false | The frozen rule asks the README only to prove Absent; a record that exists is a regular file whatever the directory holds, and a test documents it. |
| EH-7 | Claim: the README's asset-name description omits the leading-alphanumeric and 100-character limits | low | Confirmed against the code; real names (`proton-cli_...`, `checksums.txt`) satisfy both. Same documentation gap as BH-10 (group G3). |
| EH-8 | Claim: the README test compares key sets only, so groupings or value rules can drift | false | The criterion is a key-set comparison in both directions; value rules are covered by the parser tests. |
| VG-1 | A symlinked `fork` or `fork/verification` is never exercised; replacing `Lstat` with `Stat` passes the package | low | Pre-verified by the layer's mutation. The "never followed" rule is stated and untested at the directory level. Fix is two test cases (group G1). |
| VG-2 | The DEL byte boundary (0x7f) of "printable ASCII" is not pinned | low | Pre-verified by mutation. Fix is test cases (group G2). |
| VG-3 | `GH_PROMPT_DISABLED=1` is not asserted | low | A hang risk only under a non-interactive caller that does not exist yet; the stand-in would need extending for it. Not worth the weight now. |
| VG-O1 | No production caller of the new code | false | Expected: callers are Stories 1.5 and 1.7. |
| VG-O2 | An equivalent mutant on the asset sort comparison | false | The layer itself shows it cannot be observed. |

**Declared implementation choices, assessed on their concrete ordinary path.**
| Choice | Ordinary path | Result |
|--------|---------------|--------|
| Unrequested 64 KiB `record-size` finding | A real record is about 2 KiB; the cap is never reached, rejects only oversized junk, and does not bound memory (EH-1). It is undocumented in the README (BH-10). | Harmless but undocumented and hollow as a memory guard. Route: document it in the README (G3), or delete it; the controller may choose. |
| 8 MiB cap on a `gh` answer | A tree or a record is far smaller; the cap stops a runaway answer and turns it into Unknown. About ten lines, tested. | No defect, no change. |
| A missing `truncated` flag reads as false | The documented examples carry the flag as a boolean, and a truncated listing sets it true. If the API ever omitted a false value, strictness would make every valid lookup Unknown and block all releases, which the lookup must not do for ordinary valid answers; a truncated tree cannot go unnoticed because it is flagged true. A real directory of a few files cannot reach any limit. | Accepted as a robust reading of an optional flag; not a defect. The same reasoning covers a missing `ref`. |
| Package-level `TestMain` for the stand-in `gh` | Acts only when a private environment variable is set. A future second `TestMain` in the package would fail to compile, loudly. The helper-process pattern would avoid it but means rewriting the harness. | Low and left as is; a note for any later story that needs its own `TestMain`. |

**Groups and routes.**
- G1 (VG-1), low, route **patch**: two cases in `TestValidateVerificationDirRejects` for a symlinked `fork` and a symlinked `fork/verification`, each asserting a `record-surface` finding. Test only. Pending; not applied.
- G2 (VG-2), low, route **patch**: cases for the DEL byte (rejected as `record-characters`) and for `~` (accepted). Test only. Pending; not applied.
- G3 (BH-10, EH-7, and the `record-size` choice), low, route **patch**: README sentences for the asset-name limits (starts with a letter or digit, at most 100 characters), byte-order sorting, hidden entries being rejected, and the 64 KiB cap; alternatively delete the cap and its test. README text (or a small deletion), no behavior change otherwise. Pending; not applied.
- Rejected as low (unlikely, and the fix is a new guard or policy): BH-1, BH-2, BH-3, BH-4, BH-9, BH-11, BH-12, EH-1, EH-2, EH-3, EH-4, EH-5, VG-3. Rejected as false: BH-5, BH-6, BH-7, BH-8, EH-6, EH-8, VG-O1, VG-O2.
- No intent_gap or bad_spec entry, so no loopback; `review_loop_iteration` stays 0. No deferral.

## Design Notes

- **Decisions made here, not intent gaps.** The architecture fixes the first four keys, the reasons and the content of a `verified` record but not every key name; the names above are the helper's, kept flat (`asset.<name>`, `attested.<key>`) so that every line is one `key: value`. The `gate-commit` of the spine is the record line `attested.gate-commit`, copied from the gate-capture file's `gate-commit`. The gate-capture file (checked by `verify` in Story 1.7) uses the same keys without the `attested.` prefix, so Story 1.7 reuses this key set and no second list exists. The lookup uses only the git-database walk instead of trying the contents endpoint first, because a commit SHA as the contents `ref` is undocumented; the spine's 404-with-README rule for Absent is expressed as "no entry while the README entry is a regular file in the same directory". Present returns the record bytes (after the local blob-id check) because `verify --status` in Story 1.7 judges the content while the refusal of Story 1.5 reads presence only. The age and outcome of a Drift Guard run join the attested keys only when that guard exists (Epic 2), with the README and the key-set test changing together.
- **Proposed `FORK.md` › Releasing wording (pull request description only):** "A Fork Release is handed over only after `verify --status <tag>` exits zero. Each Release Tag has at most one record, `fork/verification/<tag>.md`, with status `verified` or `abandoned`. It is written once, through the Git workflow above, and is never edited, replaced or removed; a tag whose record exists is spent and the next release uses the next `N`. `fork/verification/README.md` fixes the fields."

## Verification

**Commands:**
- `just test-fast` -- expected: exits 0 (all packages but `tests/live`; no credentials, no network)
- `just lint` (full) -- expected: exits 0 and leaves no diff
- `just build` -- expected: exits 0
- In that order, through the isolated pinned toolchain (Go 1.27.0, golangci-lint 2.13.1, GoReleaser 2.17.1, offline). `just snapshot` is not needed: the release surface does not move. These broad gates belong to the controller after the implementation child returns; the child runs only the focused package tests.

**Manual checks (if no CLI):**
- The diff outside `_bmad-output/` lists only `scripts/fork-release/` paths and `fork/verification/README.md`; `.goreleaser.yaml`, `justfile`, `ci.yml`, `devbox.lock`, `go.mod` and `go.sum` are byte-identical.
