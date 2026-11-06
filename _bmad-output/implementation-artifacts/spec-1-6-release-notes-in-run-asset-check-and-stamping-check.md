---
title: 'Story 1.6: Release notes, in-run asset check and stamping check'
type: 'feature'
created: '2026-11-06'
status: 'done'
route: 'dispatch'
baseline_commit: '893e343ca532a3543d5b1d0689e6b91a7e024ad0'
review_loop_iteration: 0
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md'
  - '{project-root}/_bmad-output/planning-artifacts/epics.md'
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-proton-cli-2026-10-25/ARCHITECTURE-SPINE.md'
  - '{project-root}/FORK.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A Fork Release has no notes that say what it is, nothing inside the run checks that every asset was uploaded and that `checksums.txt` lists each binary, and nothing proves that the uploaded binary reports the Fork Version String.

**Approach:** Add, in `scripts/fork-release/`, three commands for the release run: `notes --in-run` renders the fixed fork notes (naming the Upstream Base, warning against `proton update`, with exactly one `Commit: <E>` and one `Run: <run_id>/<run_attempt>` line from shared constants) into the runner temp directory; `assets --in-run` fails unless the published release holds the six binaries and `checksums.txt` and `checksums.txt` lists each binary; `stamp --in-run` downloads the uploaded Linux amd64 binary, runs `proton version -o json` and requires the Fork Version String. The six binary names come from `selfmanage.AssetName` and nowhere else; one strict `checksums.txt` parser is written for `assets` and the later `verify`. Authoritative wording of the criteria: `epics.md`, Story 1.6.

## Boundaries & Constraints

**Always:**
- `FORK.md` and the accepted spine win. Outside `_bmad-output/`, the change touches only files under `scripts/fork-release/`; no `go.mod` or `go.sum` change, nothing under `internal/` (the helper only imports `internal/selfmanage`; it changes no file there), `cmd/`, `tests/`, `fork/` or `.github/`, and `.goreleaser.yaml`, `justfile`, `devbox.lock`, `README.md` and the earlier fixtures stay byte-identical. Earlier stories' behavior and assertions are kept; the edits to old files are only the ones this story needs, listed in the Code Map.
- **Shared line constants.** `commitLinePrefix` (`Commit: `, from Story 1.5) and a new `runLinePrefix` (`Run: `) are used by the notes writer and by both parsers. A body has exactly one `Commit:` line (40 lowercase hex) and exactly one `Run:` line (`<run_id>/<run_attempt>`, two positive decimal integers without leading zeros); CRLF is tolerated; a repeated or malformed line is rejected (`Commit:` makes a release Other, as already implemented; a bad `Run:` is reported to the caller, and `verify` of Story 1.7 will refuse on it). The rest of the template never starts a line with either prefix.
- **`notes --in-run`** takes no other argument and reads `GITHUB_REF_NAME`, `GITHUB_SHA`, `GITHUB_RUN_ID`, `GITHUB_RUN_ATTEMPT` and `RUNNER_TEMP`. It computes the Upstream Base with the existing base function over the tags of `origin` for `E = GITHUB_SHA` (refusing like the guard when it cannot), requires the tag to match the Release Tag pattern and its `X.Y.Z` to equal the base, then creates exactly one new file `fork-release-notes.md` inside the existing directory `RUNNER_TEMP`, never overwriting a file. The notes name the Upstream Base (`vX.Y.Z`), tell the reader not to run `proton update` on a fork binary because it resolves Upstream's latest release and would replace the fork build, and carry the two lines. Nothing else is written, no network write, no tag or ref.
- **`assets --in-run`** takes no other argument and reads `GITHUB_REF_NAME` and `GITHUB_SHA`. It uses the one release lookup of Story 1.5 and requires the release to be Published-Same for `E`. It requires, in that release's asset list, every one of the six binary names and `checksums.txt`, each in state `uploaded`; it downloads `checksums.txt` and, with the one strict parser, requires every binary name to be listed. It requires no `.deb`, `.rpm` or `.apk` and reads no other asset; extra assets are ignored here. It is presence-level: it does not download or hash a binary.
- **The six binary names** are `selfmanage.AssetName(goos, goarch)` for linux and darwin on amd64 and arm64 and windows on amd64 and arm64; no second list or spelling exists in the helper, and a test pins the six results. If `AssetName` refuses a pair, the check fails.
- **The checksum parser** is strict and shared: lines `<64 lowercase hex>  <file name>` (two spaces), a final blank line tolerated, no other blank or malformed line, no repeated file name; it returns the map from name to hash. `assets` uses it, and Story 1.7 will use it for the two-way check.
- **`stamp --in-run`** takes no other argument and reads `GITHUB_REF_NAME`, `GITHUB_SHA` and `RUNNER_TEMP`. It requires a Published-Same release for `E`, downloads the asset `selfmanage.AssetName("linux", "amd64")` into a new file inside `RUNNER_TEMP` with owner-only execute permission, runs it with `version -o json` under a minimal environment without credentials, and requires the JSON `version` to equal the tag without its leading `v`. Anything else fails, with one refusal line and no output of the binary's text.
- Reads of the release use the same pinned `gh` adapter: only `GET`, the API-version header, owner and repository as constants, never `GH_REPO`; a call that downloads an asset uses the documented asset endpoint with an octet-stream `Accept` header and a larger output limit than the 8 MiB used for JSON, and its argv is pinned by a test. Every refusal is one line `fork-release: <check>: <reason>` with the checks `notes`, `assets` and `stamp` for these commands (and the guard's existing names for base), exit 1; usage is exit 2; success prints one line and exits 0. The reasons never quote a response, a URL or the binary's output.
- Tests use fakes, scratch repositories and a stand-in program only (the existing helpers); no `gh` run, no download, no network, no real tag, release or repository of this checkout, no skip. Comments and docs use US spelling and are public-safe. Gates run through the pinned toolchain with the recipes under Verification; never `just test` or any `go test` pattern that reaches `tests/live`.

**Never:**
- No workflow file or caller (Story 1.9), no `verify`, `verify --status` or record code (Story 1.7), no `--pre-push` (Story 1.8), no download of a binary other than the one `stamp` runs, no hashing of binaries, no `FORK.md` edit (no wording is owed by this story), no lock, queue, service or state file, no new dependency, no write call to GitHub, no change to a file under `internal/`.
- No skipped, weakened or disabled test or gate; no Actions, tag, release, settings or default-branch change.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Notes | valid tag on base `vX.Y.Z`, run id and attempt, existing temp directory | one new file with the Upstream Base, the `proton update` warning, one `Commit:` and one `Run:` line; the parsers read both back; exit 0 | tag not matching the base, no base, missing or non-numeric run values, missing or unusable `RUNNER_TEMP`, file already present: `fork-release: notes: ...`, nothing written, exit 1 |
| Line parsing | CRLF body; one good line each | accepted | repeated, malformed or missing `Commit:` or `Run:` line rejected |
| Assets pass | Published-Same release with the six binaries and `checksums.txt` uploaded, `checksums.txt` listing each binary, extra archives present | pass, exit 0 | N/A |
| Assets fail | each of the seven required names missing in turn; a required asset not `uploaded`; a binary absent from `checksums.txt`; malformed or repeated `checksums.txt` line; release None, Draft, Other or Unknown; failed download | `fork-release: assets: ...`, exit 1 | one healthy control beside each |
| Stamp pass | Published-Same release, asset present, binary reports `version` equal to the tag without `v` | pass, exit 0 | N/A |
| Stamp fail | other version, empty or non-JSON output, a failing binary, no such asset, release not Published-Same, failed or oversized download | `fork-release: stamp: ...`, exit 1 | no binary text echoed |
| Names | the six pairs | exactly the `selfmanage.AssetName` results | an unsupported pair fails the check |
| Adapter | the asset download call | argv pinned: `api --method GET -H X-GitHub-Api-Version: 2026-03-10 -H Accept: application/octet-stream repos/Ezopek/proton-cli/releases/assets/<id>` | a path outside the allowed shapes is refused before `gh` runs |
| Usage | bare `notes`, `assets` or `stamp`, extra arguments | one usage line, exit 2 (the old dispatch rows keep passing) | N/A |

</frozen-after-approval>

## Code Map

- `scripts/fork-release/releaselookup.go` -- `lookupRelease(g, tag, e)` returns `releaseLookup{State, Reason}` from the whole list; `commitLinePrefix`, `parseCommitLine`, `releasePublishedSame` and friends. Extend the answer to carry the matched release's asset list (name, id, state) for the commands that need it; a malformed asset list fails only the command that reads it, never the guard's classification. Do not change the existing classifications or their tests.
- `scripts/fork-release/recordlookup.go` -- `ghGetter`, `execGH`, `ghArgs`, `endpointPattern`, `limitWriter`/`maxGHOutput` (8 MiB). Add the asset download path shape (`releases/assets/<digits>`), its extra `Accept` header and a larger limit for downloads, as a separate interface or optional method so that the old fakes (`fakeAPI`, `fakePages`) still satisfy `ghGetter` unchanged.
- `scripts/fork-release/tagcheck.go` -- `upstreamBase(g, e, tags)` and `checkBaseMatch(tag, base)`: reuse for `notes`; `checkTagPattern`. `scripts/fork-release/repeat.go` -- `getenvOf`, `refuseWith`, `guardOK` and the in-run pattern: reuse the style.
- `scripts/fork-release/guard.go` -- `run` dispatch and `guardDeps` (`git`, `getenv`, `gh`, streams). Add dispatch for the three commands with their `--in-run` flag only. **Old dispatch rows:** `TestRunDispatch` pins bare `notes`, `stamp` and `assets` as usage with exit 2, one line and no git call, so the new commands are invoked with `--in-run` (the same spelling as `guard --in-run`) and a bare command stays usage; no old assertion changes.
- `internal/selfmanage/update.go` -- `AssetName(goos, goarch)` (exported; builds `proton-cli_<os>_<arch>` and the `.exe` form for windows) is the only owner of the six names; its package imports eleven other `internal/` packages and `minio/selfupdate`, all already in `go.mod`, so the helper binary grows and **the two scratch-build tests must copy the `internal/` sources too**: `buildInScratchClone` in `identity_test.go` and `buildHelperBinary` in `guard_test.go` copy only `scripts/fork-release/*.go` and the module files today, so a build with this import fails there. Adapt exactly those two helpers to also copy the non-test `.go` files under `internal/` (type of change: test setup only, every assertion kept). `selfmanage.ExpectedChecksum` is lenient and is not used for the shared parser.
- `internal/cli/self/self.go` (the product): `proton version -o json` prints an object with a `version` field (plus `go`, `platform`, optional `revision`): read only `version`.
- New: `notes.go` (template, `runLinePrefix`, `parseRunLine`, the writer), `assets.go` (the six names, the strict checksum parser, `assets` and `stamp` commands), and a test file named after each.
- Documentation position: the list-releases page (re-fetched by the controller at Story 1.5) documents each release's `assets` with `name`, `id`, `state` (`uploaded` in the example) and `size`. The official "Get a release asset" page (read by the controller at adoption) documents `GET /repos/{owner}/{repo}/releases/assets/{asset_id}`: with the `Accept: application/octet-stream` header the API either redirects to the content or streams it, and clients must handle both a 200 and a 302; without that header the answer is the asset's JSON description. This is documentation, not proof of live behavior: the adapter relies on `gh` following a redirect, any other answer fails the content checks that follow (the checksum parser, the executable run, the JSON `version`), and live behavior stays unobserved (Q6). Whether the Actions job token sees draft releases (Q5) does not matter here, because `assets` and `stamp` read a release that must already be published.
- Do not change: the record code, the profile, the tag, toolchain and repeat logic, `FORK.md`, the README.

## Tasks & Acceptance

**Execution** (strict TDD, tests first with no production-first exception: write the tests and observe the missing-API compile failure; only then add the minimal stubs that give a runnable assertion RED and keep that output; then implement; never edit an oracle to make it pass; no skip; one cluster per step):
- [x] `scripts/fork-release/notes.go`, `notes_test.go` -- the constants, `parseRunLine` and the notes writer: round trip through both parsers, exactly one of each line, CRLF, every rejection, the base and tag agreement, the exclusive create inside `RUNNER_TEMP` and no other file, and the `notes --in-run` command over fakes and a scratch origin -- FR-7, AR-2, AR-4
- [x] `scripts/fork-release/assets.go`, `assets_test.go`, `releaselookup.go`, `recordlookup.go` -- the six names pinned against `selfmanage.AssetName`, the strict checksum parser, the asset list on the lookup answer, the download path, header and limit with the pinned argv, and `assets --in-run` over fakes: the passing case and each failure beside it -- FR-6, AD-7, AD-14
- [x] `scripts/fork-release/assets.go`, `assets_test.go`, `guard.go` -- `stamp --in-run`: the passing case, each failing case, the stand-in program for the one real execution, and the dispatch of the three commands with their usage cases; plus the setup-only adaptation of the two scratch-build test helpers -- FR-11, AD-3, AD-6

**Acceptance Criteria:**
- Given a Release Tag, `E`, the Upstream Base and the run values, when `notes` renders, then the notes name the base, warn against `proton update`, and carry exactly one `Commit:` and one `Run:` line from the shared constants, and the parsers tolerate CRLF and reject a repeated or malformed line.
- Given a published release, when `assets` runs, then it fails unless all six binaries and `checksums.txt` are uploaded and `checksums.txt` lists each binary, requires no `.deb`, `.rpm` or `.apk`, takes the six names only from `selfmanage.AssetName`, and shares its checksum parser with the later `verify`.
- Given the uploaded `proton-cli_linux_amd64`, when `stamp` runs, then it requires a Published-Same release, runs the downloaded binary with `version -o json` and requires `version` equal to the tag without its leading `v`; anything else fails.
- Given the pull request, when reviewed, then its diff touches only files under `scripts/fork-release/`, all tests use fixtures, nothing is downloaded from the network in a test, `just test-fast`, full `just lint` (no diff left) and `just build` pass. Traces: FR-6, FR-7, FR-11, S1, S2; AD-2, AD-3, AD-6, AD-7, AD-14.

## Implementation Notes

**Execution roles (set before dispatch; the implementation child's sole source is this spec).**
- The implementation child owns the code and tests and the focused green result of the package tests, strict TDD. It runs only `go test ./scripts/fork-release/ -count=1` (with `-run`, `-v` or `-json` as needed) using the pinned Go 1.27.0 already on `PATH`, with `GOPROXY=off`, `GOFLAGS=-p=1`, `GOMAXPROCS=1`, `GOMEMLIMIT=768MiB` and no network. Save each red and green run's complete output together with its true exit status in a file under `$TMPDIR` (never pipe a run into `head` or `tail`, which hides the status; capture the status first), name the files by cluster and phase, and list them in the report. If a step of the test-first order was not observed, say exactly which; never fabricate it.
- Not run by the child, because the controller runs them against the frozen candidate after the child returns: `just test-fast`, full `just lint`, `just build`. The child reports a focused checkpoint and says plainly that broad qualification is incomplete; it must not claim the acceptance criteria are fully verified.
- No mutation of this checkout's git state: no `git add`, `rm`, `reset`, `checkout`, `stash`, `commit`, `push`, `fetch`, tag, ref or configuration change, and no `git` call against the real repository except read-only inspection. Scratch `git` only inside temporary repositories the tests create. **Never run `gh`, any GitHub API, a download, Proton or any network call**; the lookups are tested against fakes, the adapter against the stand-in program of the Story 1.4 tests, and the binary run of `stamp` against a stand-in.
- Allowed paths: new `scripts/fork-release/notes.go`, `notes_test.go`, `assets.go`, `assets_test.go`; edits to `releaselookup.go`, `recordlookup.go`, `guard.go`, and to the two scratch-build test helpers only (`buildInScratchClone` in `identity_test.go` and `buildHelperBinary` in `guard_test.go`) as the Code Map describes. Nothing else: no README, `fork/`, workflow, `FORK.md`, `justfile`, module file, file under `internal/` or other product file. Every existing test and subtest keeps its behavior and every old assertion stays; if an old test seems to need a change for any other reason, stop and report the exact reason instead of changing it.
- No new dependency, no new environment variable beyond the five named in the frozen rules, no configuration that could bypass a check. Leave no background process running.

**Test-first order, exactly.** For a cluster whose API does not exist yet, write the tests first and run them: keep the compile failure (exit status 1, missing names). Only then add the smallest stubs that compile and run the tests again: keep that runnable assertion failure (RED by assertion, not by compile error). Then implement the minimum and keep the green run. For work on an API that already exists (the dispatch, the lookup extension, the adapter path, the scratch-build helpers), the tests must fail by assertion directly; do not invent a compile failure.

**Fixture and design guidance.**
- Reuse the existing fakes and helpers (`fakePages` and the release entry builders of `releaselookup_test.go`, `testDeps`, `newScratch`, `newRelease` and the stand-in `gh` program with its `TestMain` hook). Extend a fake only in new test files or by composition so that old tests keep compiling.
- **Checksum format evidence.** The retained output of an earlier GoReleaser snapshot of this very configuration (a `checksums.txt` of twelve lines: the six archives and the six raw binaries) has the form `<64 lowercase hex>  <file name>` with two spaces, one line per file, and one line feed at the end, no blank line. Use this exact shape as the passing fixture, built from hash values you make up, and keep the strict parser as specified; if a real GoReleaser file ever deviates, the parser fails closed and that is reported, not narrowed. The twelve names in that snapshot had the form `proton-cli_<version>_<os>_<arch>.tar.gz` or `.zip` for archives and `proton-cli_<os>_<arch>` plus `.exe` on Windows for the raw binaries.
- The six names come from `selfmanage.AssetName` over the six platform pairs and nowhere else; assert the six results in a test and do not spell any of them in production code.
- A release asset entry has `name`, `id`, `state` and `size`; only `uploaded` counts. The download of one asset goes through the same `gh` adapter with the documented octet-stream `Accept` header, the asset path shape `releases/assets/<digits>`, and a larger output limit than the JSON limit (a binary is tens of megabytes; choose a documented constant). A fake serves bytes by asset id.
- `stamp` runs the downloaded file with `version -o json`; keep the process seam injectable so that the real execution is tested once against a stand-in and the other cases against a fake runner. Run it with a minimal environment and a timeout; never print the binary's own output in a refusal.
- `notes` creates its file exclusively inside the existing directory named by `RUNNER_TEMP` and writes nothing else.

### Implementation record

One native implementation child wrote the code in three test-first clusters (notes, assets, stamp). Each cluster has a compile failure on the missing names, a runnable assertion failure from minimal stubs, and a green run; the logs are kept with the story's private evidence. The scratch-build helpers then needed the setup-only adaptation described in the Design Notes. That step's first run failed while building the helper, not on an assertion, because `assets.go` now imports `internal/selfmanage`; it is recorded as a build-time failure.

Choices made inside the frozen intent: the commands are `notes --in-run`, `assets --in-run` and `stamp --in-run`, and any other spelling is usage with no read; the checksum parser accepts at most one closing line feed and otherwise requires the two-space form with 64 lowercase hex, which is what the retained GoReleaser output has; the stamp command writes the downloaded binary as `stamp-<asset name>` in `RUNNER_TEMP`, runs it with a temporary home and a minimal environment, and compares the `version` of its JSON output to the tag without the `v`. The download path is the documented octet-stream one and is tested offline only; the redirect is gh's to follow, and no live behavior is claimed (Q6).

Focused package run: 1,676 distinct test and subtest IDs pass, 0 fail, 0 skip. Every top-level test that existed before still passes. Not done yet: the broad gates (`just test-fast`, full `just lint`, `just build`), the Tasks and Matrix audit, and review. Tasks are left unticked.

Lint repair after the controller's full lint run: one finding, `ST1023` in the test that asserts the production `gh` adapter can download (`var g ghGetter = productionDeps().gh`). The field is already declared as `ghGetter`, so the declaration was shortened to `g := productionDeps().gh`; the type and the dynamic assertion to `ghDownloader` are unchanged. The first lint failure and its log are kept as evidence; the full gates are re-run by the controller.

Completion: the independent review of the nine-file candidate returned COMMENT with no blocking defect, and it was not relabeled as approval. Its two nonblocking residuals were accepted without a patch: the output-cap test prints invalid JSON, so a stronger fixture (a valid object followed by padding) is optional; and the stamped executable stays in the per-job runner temp directory, where the planned job runs `stamp` once, so cleanup is optional. The native review's 28 triage rows, its three reports and `review_loop_iteration` 0 are unchanged. Verification: the focused package run has 1,676 distinct passing test and subtest IDs, 150 of them top-level, with 0 failed and 0 skipped; `just test-fast`, full `just lint` and `just build` all exit 0 on the corrected candidate. Two earlier failures are preserved: the first full lint run failed on one `ST1023` finding in a test (since repaired), and the first whole-package run after the asset code failed to build the scratch helpers because the new import was missing there, which was a compile failure and not an assertion RED. Test-first order rests on the implementation child's saved logs. Not tested: a real GitHub API call, the real `gh` redirect handling of the asset download, and a native Windows run. Nothing calls these commands until Story 1.9 adds the workflow; nothing is published, activated or declared ready for release.

## Spec Change Log

## Review Triage Log

First review pass (three layers: blind, edge-case, verification-gap), working tree at the corrected candidate; broad gates `just test-fast`, full `just lint` and `just build` had all exited 0. The reviewers' own severities are disregarded. Every finding has one row. No finding routes to intent_gap or bad_spec, so there is no loopback and `review_loop_iteration` stays 0. Two low findings route to patch (not applied yet); nothing is deferred.

| ID | Finding (short) | Verdict | Evidence | Route |
|---|---|---|---|---|
| BH-1 | `stamp` never compares the binary's hash to `checksums.txt` | low | The frozen "Never" list says no hashing of binaries and `assets` is presence-level by intent; the checksum comparison is a later story's concern | rejected (intent excludes it) |
| BH-2 | only Linux amd64 is run | low | The criteria name that one binary; running the others is not asked | rejected (intent excludes it) |
| BH-3 | `parseChecksums` allows `/`, `..`, bidi or invalid UTF-8 in names (also EC-8) | maybe-false | No code in this change joins a listed name to a path; GitHub asset names cannot hold a slash; if a later reader needs a basename rule it adds it; at most low | rejected (low, fix adds branches) |
| BH-4 | the 256 MiB download limit also covers `checksums.txt` | low | Real, but the source is the run's own release and the buffer is bounded; a per-call limit adds a parameter | rejected |
| BH-5 | `stamp` refusal does not say timeout, exit status, start failure or output limit | low | The criteria ask for one refusal line without the binary's text; every cause ends the run red and the version is abandoned either way; distinguishing causes adds branches | rejected |
| BH-6 | the stamped binary stays in `RUNNER_TEMP`; a second `stamp` in the same job would refuse (also EC-3) | low | Confirmed in `runStamp` (only the home directory is removed) and pinned by `TestRunStampPass`; the directory is per job and the planned caller runs `stamp` once in a read-only job, so the harm is negligible, but the fix is one deferred `os.Remove` and the pinned assertion would follow | patch (optional, one line plus its oracle) |
| BH-7 | `notes.go` spells `proton update` and `proton-cli update` | low | The frozen Approach requires the `proton update` warning; the product's no-literal rule covers its command tree, which this helper is not part of | rejected |
| BH-8 | the notes do not explain how to verify a download | low | The frozen Approach defines the notes content; more text is not asked | rejected (intent excludes it) |
| BH-9 | `--in-run` is only a spelling (no check that Actions runs it) | low | Same spelling and trust as `guard --in-run` of Story 1.5; the Operator running it by hand with set variables is the Operator's own act | rejected |
| BH-10a | the "six platform pairs" subtest checks only the count | false | The same test compares the six names to a literal list and to `selfmanage.AssetName` per pair, which pins the pairs | rejected |
| BH-10b | no test through a real `gh` redirect | false | Stated limit (Q6): no live call is allowed; the argv and the byte return are tested through the stand-in | rejected |
| BH-10c | `copyInternalSources` copies all of `internal/` and only `.go` files (also EC-6, VG-3) | low | Real latent fragility: a future embedded non-Go file in the import closure would fail the scratch builds at build time, loudly, not silently; the closure has none today (all scratch builds pass) | rejected (fails loudly, fix adds code) |
| BH-11a | `publishedSame` returns an unused `e` | low | Cosmetic | rejected |
| BH-11b | `lineValue` lives in `notes.go` | low | Cosmetic; both parsers share it, which is the intent | rejected |
| BH-11c | `append(names, checksumsName)` relies on spare capacity | false | `names` has length equal to capacity, so `append` copies; `names` is not changed | rejected |
| BH-11d | `parseRunLine` has no production reader beyond the notes self-check | false | The frozen intent makes it the shared constant and parser for Story 1.7's `verify` | rejected |
| BH-11e | the embedded asset list might be truncated by GitHub | maybe-false | Not documented either way (Q6); the retained snapshot has 13 files, under a 30-entry page; a short list fails closed; at most low | rejected |
| BH-11f | no README or docs for the three commands | low | The frozen Boundaries forbid README changes; the caller is Story 1.9 | rejected (intent excludes it) |
| EC-1 | a malformed or duplicate extra asset fails the whole list | low | By design the list is trusted whole or not at all (Code Map); GitHub's documented shape has unique names and ids; fail-closed; a per-entry filter adds branches | rejected |
| EC-2 | `assets` and `stamp` do not compare the `Run:` line with the current run | low | Published-Same already binds the commit; Story 1.5 refuses a repeat run; the `Run:` check is `verify`'s | rejected |
| EC-3 | see BH-6 | low | grouped with BH-6 | patch (with BH-6) |
| EC-4 | duplicate `version` keys in the binary's JSON, last one wins | low | The binary is the fork's own Go build and cannot emit duplicates | rejected |
| EC-5 | `GITHUB_RUN_ID` and `GITHUB_RUN_ATTEMPT` have no digit-count bound | low | Values come from Actions; negligible | rejected |
| EC-6 | see BH-10c | low | grouped with BH-10c | rejected |
| EC-7 | no comparison with the asset's `size` field | low | A short or wrong body fails the parser, the empty check or the run; a size check adds a branch | rejected |
| EC-8 | see BH-3 | maybe-false | grouped with BH-3 | rejected |
| VG-1 | the 64 KiB output cap of `execProgram` has no test that fails without it | low | Confirmed: the oversized-output case prints non-JSON text, which the version check also refuses, so removing the cap changes nothing the tests see; the fix is a test input that prints a valid object then more than 64 KiB, expecting a refusal | patch (test only) |
| VG-2 | no test for a hung binary or the working directory of `execProgram` | low | Confirmed absent; the timeout is a constant and the binary is the fork's own; making it injectable adds a parameter | rejected |

**Own checks beyond the layers (not findings):** the retained GoReleaser `checksums.txt` (12 lines) was checked read-only with an independent script against the same line rules as `parseChecksums`, and its six non-archive names equal the `selfmanage.AssetName` results; the Go parser itself was not run on that file. GitHub's 200 or 302 download behavior through a real `gh` is documented but unverified (Q6); draft visibility to the job token (Q5) is unresolved and does not affect these two commands.


## Design Notes

- **Decisions made here, not intent gaps.** The architecture fixes what the three commands check but not their wire: they take their inputs from the Actions default variables and are invoked as `notes --in-run`, `assets --in-run` and `stamp --in-run`, the spelling of `guard --in-run`, because the old dispatch tests keep a bare spelling as usage. `E` for these commands is `GITHUB_SHA` (the guard has already proved it equals `HEAD` and the tag), so none of them needs a git checkout except `notes`, which reads the tags of `origin` to name the base. The notes file is always `fork-release-notes.md` in `RUNNER_TEMP`, the name Story 1.9 passes to GoReleaser's `--release-notes`. `assets` requires Published-Same because it runs after GoReleaser has published; `stamp` requires the same by the criteria. The one source of the six names is a production import of `internal/selfmanage`, as the criteria say ("come from ... with no second list"); the cost is the larger helper and the two test-helper adaptations above. The alternative, a helper-owned formatter checked against `AssetName` by a test, is cheaper to build but keeps a second spelling rule, and is not taken.
- **Real journey.** In Story 1.9's `release` job: `guard --in-run`, `notes --in-run`, `guard --recheck`, GoReleaser with `--release-notes "$RUNNER_TEMP/fork-release-notes.md"`, `assets --in-run`, the summary; then the read-only `stamp` job runs `stamp --in-run` on the uploaded Linux binary. A failed `assets` or `stamp` ends the run red and leaves the release unverified; the version is then abandoned (Story 1.4's record) and the next `N` is used.

## Verification

**Commands:**
- `just test-fast` -- expected: exits 0 (all packages but `tests/live`; no credentials, no network)
- `just lint` (full) -- expected: exits 0 and leaves no diff
- `just build` -- expected: exits 0
- In that order, through the isolated pinned toolchain (Go 1.27.0, golangci-lint 2.13.1, GoReleaser 2.17.1, offline). `just snapshot` is not needed: the release surface does not move. These broad gates belong to the controller after the implementation child returns; the child runs only the focused package tests.

**Manual checks (if no CLI):**
- The diff outside `_bmad-output/` lists only `scripts/fork-release/` paths; `.goreleaser.yaml`, `justfile`, `ci.yml`, `devbox.lock`, `go.mod`, `go.sum` and everything under `internal/` are byte-identical.
