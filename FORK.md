# Ezopek/proton-cli — fork rules

This is the operator's fork of [`roman-16/proton-cli`](https://github.com/roman-16/proton-cli) (Upstream, MIT). It exists to supply the Backend binary for [`Ezopek/ezop-proton-calendar-mcp`](https://github.com/Ezopek/ezop-proton-calendar-mcp), a local MCP server for Proton Calendar. That server's PRD records why the fork exists and what it should fix (PRD §7, addendum §B and §F).

This file is the only place where the fork's own conventions are written down. Everything else in the repository is Upstream's: its README, AGENTS.md, CONTRIBUTING.md and docs apply unchanged unless this file says otherwise.

## Branches

| Branch | Content | Rules |
|---|---|---|
| `main` | Pristine mirror of Upstream `main`; no longer the default branch | Never author commits on it. Only an explicitly authorized fast-forward from `upstream/main`. |
| `ezop` | The fork's trunk and the repository's default branch: the chosen Upstream release tag, plus the fork's own commits (this file, the release configuration, the patches) | The only branch fork releases are built from. Rebased onto each next Upstream tag; `upstream/main` is never merged into it. |
| `<type>/<short-kebab-slug>` | A fork-only topic | See [Git workflow](#git-workflow). |
| `patch/<topic>` | One focused change, cut from the Upstream tag `ezop` is based on | A clean contribution branch offered Upstream as an issue or PR. Reaches `ezop` by cherry-pick, see [Git workflow](#git-workflow). |

Keep the Go module path `github.com/roman-16/proton-cli` unchanged, so rebases and Upstream PRs stay clean.

## Git workflow

The fork uses trunk-based GitHub flow with `ezop` as the trunk. `main` is the exception: it only mirrors Upstream.

### Topics and worktrees

- **Fork-only topics** are named `<type>/<short-kebab-slug>`, where `<type>` is a Conventional Commits prefix: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `ci`, `build` or `perf`.
- A topic is short-lived and lands as one self-contained PR to `ezop`. Commit messages and PR titles are Conventional Commits.
- Each session works in its own linked worktree, `.worktrees/<branch>`, and never commits in another session's worktree. Existing in-progress worktrees and branches are kept as they are, not silently reset or renamed.
- A PR opened with `--body-file` skips the template, so write out the shared template's sections explicitly. Policy references say "the internal wiki".

### Patches

1. `patch/<topic>` stays a clean contribution branch cut from the Upstream release tag. Do not rebase it onto `ezop`, and do not bring fork governance into an Upstream PR.
2. To integrate a patch, keep the contribution branch and cherry-pick only its selected commits into a fork-only topic based on the current `ezop`.
3. Record each mapping, Upstream patch SHA to fork SHA, in the PR.
4. Validate and independently review the fork candidate, not the contribution branch, before it is integrated.

### Integrating into `ezop`

Keep the fork-only patch series linear. The preferred integration is a reviewed exact fast-forward of `ezop`: refresh refs, require that the reviewed base and head still match, stop on any divergence. No merge commit, and no force-push as normal PR integration.

The GitHub UI allows **Rebase and merge** only. Squash and merge commits are disabled, and merged topic branches are deleted automatically. Rebase and merge always creates new SHAs, so it does not preserve evidence bound to the old ones. After any GitHub rebase, and after any later rebase onto an Upstream tag, before release:

- record an explicit old-to-new SHA mapping;
- compare the actual source and tree, not just the messages;
- refresh the validation and review against the new hashes.

Earlier receipts stay as historical records. They are never rewritten, and never reused for a different commit.

### Force-push

Force-push is not a routine agent operation.

- Only the operator rebases `ezop` onto an Upstream tag, and pushes it with `--force-with-lease=ezop:<exact observed old SHA>`, never plain `--force`.
- Rewriting your own unpublished topic before review is fine. Never rewrite another session's work, shared work or reviewed work.
- No command in this repository's documentation or tooling should force-push automatically.

### Repository rulesets

These GitHub rulesets are active, and their configuration and effective branch rules have been read back. No destructive push has been attempted to test them.

| Ruleset | Target | Rules | Bypass |
|---|---|---|---|
| [`main-mirror`](https://github.com/Ezopek/proton-cli/rules/25840214) | `refs/heads/main` | deletion, non-fast-forward | none |
| [`fork-release-tags`](https://github.com/Ezopek/proton-cli/rules/25840216) | `refs/tags/v*-ezop.*` | deletion, update | none |
| [`ezop-deletion`](https://github.com/Ezopek/proton-cli/rules/25840218) | `refs/heads/ezop` | deletion | none |
| [`ezop-rebase`](https://github.com/Ezopek/proton-cli/rules/25840220) | `refs/heads/ezop` | non-fast-forward | user `Ezopek` only, always |
| [`main-operator-only`](https://github.com/Ezopek/proton-cli/rules/26741791) | `refs/heads/main` | update | user `Ezopek` only, always |
| [`tags-operator-only`](https://github.com/Ezopek/proton-cli/rules/26741823) | `refs/tags/v*` | creation, update, deletion | user `Ezopek` only, always |

Deletion and rebase are separate rulesets on purpose: the rebase bypass lets the operator rewrite `ezop` for an Upstream-tag rebase, and that must not also allow deleting the trunk.

`main-operator-only` and `tags-operator-only` were added on 2026-11-07 for activation and read back from the saved responses. `fork-release-tags` is unchanged: its no-bypass update and deletion rules keep fork release tags immutable even for the operator. Whether any ruleset binds a workflow's `GITHUB_TOKEN` is not established.

Rulesets cannot tell the human owner from automation using the same owner's account. They also do not by themselves enforce that `main` holds only Upstream content, that `ezop` is updated only through PRs, or the whole boundary for enabling Actions in the future.

### Fork-only development governance

BMAD Method and the portable BMAD coordinator are installed only in the `ezop`
lineage (including working branches opened as pull requests to `ezop`), never on
`main` or upstream-contribution `patch/*` branches. Their generated instructions,
project artifacts and the installer's managed opt-in block appended to `AGENTS.md`
are fork-only additions; the preceding Upstream guidelines remain unchanged.
The same holds for the native `bmad-project-context` block, which belongs after
the unchanged Upstream text and before the installer's coordinator block (the
coordinator block's exact bytes stay last, ending the file), and for a fork-only
`CLAUDE.md` that only imports `FORK.md` and then `AGENTS.md`. Those blocks and that adapter are operational instructions
and derived pointers, not fork policy: this file stays the only source of fork
policy and wins wherever they disagree with it.
`FORK.md` takes precedence wherever Upstream's changelog-driven release process
conflicts with the fork's tag-driven release rules below. Do not run Upstream's
multi-account live suite for fork bootstrap work: its credentials and mutation
scope are not authorized by a `calendar-dev` / `MCP Test` smoke-test permission.

## Versions and tags

- **Version string:** `X.Y.Z-ezop.N`, set through ldflags. `X.Y.Z` is the Upstream release the branch is based on, and `N` counts fork releases on that base, starting at 1.
- **Release tag:** `v` + the version string, for example `v5.1.0-ezop.1`.
- **The first release, `v5.1.0-ezop.1`,** contains no functional patches. It only exists so the MCP server can lock onto a fork build from day one.
- **`proton version` must report the fork string.** The MCP server's version gate depends on this to tell fork builds from Upstream builds.
- **Fork release tags are immutable, even before publication.** Never delete, move, reuse or re-push an existing `v*-ezop.*` tag.
  - A failed run is retried only as a full re-run of the original run, and only when neither a verification record nor a GitHub release object (draft or public) exists for the tag. `fork-release verify --status <tag>` reports both (`record: none` and `release: None`).
  - If the state cannot be read (token `unknown`, or `release: Unknown`), stop and read it again. Do not re-run, hand over or abandon on that alone.
  - A published release of the tagged commit that has no record yet (`record: none`, `release: Published-Same`) after a green run is not a failure: run `verify` and land the record. If instead a release object exists without a record and cannot be verified (a draft, a release of another commit, a red run, or `verify` refusing on a mismatch), the tag is never retried: record its abandonment and use a new, explicitly authorized version and tag.
  - An existing record is never replaced and a second one is never added. `verified` is done; `abandoned` and `invalid` are spent, so use a new version. An abandonment record is created only when none exists. Abandoned releases are never deleted.
  - A tag pushed before Actions were activated, with no run, needs an explicit recovery decision, not a re-tag.
  - Abandoning `v5.1.0-ezop.1`, the first release, needs its own operator policy decision, and every abandonment rule above is subject to it for that tag. The operator's decision (2026-11-07): retry only when the retry rule above allows it; when it cannot be retried, abandon and use `v5.1.0-ezop.2`.

## Releasing

1. If Upstream has a new tag, rebase `ezop` onto it (see [Following Upstream](#following-upstream)).
2. **Pre-Tag Gates**, before the tag is pushed: in a clean checkout of the landed commit, with the toolchain pinned in `devbox.lock`, run `just test-fast`, `just lint` (leaving no diff), `just build` and `just snapshot`, in that order, stopping at the first failure. Write the sanitized gate-capture file (format in `fork/verification/README.md`) before the push, named for the tag and commit, and keep that original until the tag's terminal record has landed. If it is missing, look for it; gates run after the push are never a substitute. If it cannot be found before the record lands, abandon the version (for `v5.1.0-ezop.1`, under the operator's policy decision above).
3. Push the tag only after `fork-release guard --pre-push <tag> <commit>` passes, using a helper built from a clean checkout at that commit. Push exactly the refspec it prints, `<commit>:refs/tags/<tag>`; never `--tags`, `--follow-tags` or a leading `+`. The first release tag is pushed only after Verified Activation (see [Actions and issues](#actions-and-issues)).
4. The release workflow builds binaries for every platform (Linux, macOS, Windows; amd64 and arm64) together with SHA-256 checksums, and publishes them **only** to this fork's GitHub Releases. It never publishes to Upstream's package channels (winget, npm, Homebrew, AUR, or any other).
5. After publication, `fork-release verify <tag> <gate-capture>` checks the downloaded assets against `checksums.txt` and runs the host binary, then prints the verification record. Land it as `fork/verification/<tag>.md` through the Git workflow above. A tag has at most one record, with status `verified` or `abandoned`; it is written once and never edited, replaced or removed.
6. Hand over to the MCP server only after `fork-release verify --status <tag>` exits zero, using the tag and checksums from the record. In `ezop-proton-calendar-mcp`, follow its Backend release procedure (PRD FR-53): re-record fixtures, run the contract and live tests, update the docs, then update the lock with that tag and those checksums.

`fork/activation-runbook.md` holds the exact commands, the helper build recipe and the recovery steps; this file holds the rules. If they ever disagree, this file wins and the runbook is corrected.

Workflows that belong to the fork use a `fork-` file name and never an Upstream workflow path. `fork-release.yml` is the only workflow that publishes; its shape is pinned by the `release` profile of `scripts/fork-release`, and a change to it is a reviewed topic that changes the pin in the same commit and lands with Actions off.

The weekly OpenAPI drift guard remains required by the MCP PRD (§7) and is tracked by [#2](https://github.com/Ezopek/proton-cli/issues/2). Its mode and trigger are decided separately and depend on the activation option; until it exists, the requirement is unmet.

### Actions and issues

- **Actions remain disabled** until the operator enables them by following `fork/activation-runbook.md`. Merging a PR, including one that adds or changes a workflow, does not enable them, and the rulesets above do not satisfy the activation gates. The operator chose option B on 2026-11-07: the default branch is `ezop`, changed without touching the `main-mirror` ruleset, and `main` stays the protected mirror. The operator may delegate the execution of any runbook step in writing; the decisions and the stops stay the operator's. While Actions are off, the inherited Upstream workflows on `main` answer HTTP 404, which is not `disabled_manually`; the runbook says how they are handled. Activation counts as done only when the runbook's post-enable readback passes (Verified Activation). Fork releases are GitHub Releases only, and no run may touch Upstream's package channels.
- **Issues:** `ezop` is the default branch, so `Closes`, `Fixes` and `Resolves` in a PR merged to `ezop` would auto-close an issue. Do not use them for an issue that must stay open until a release. Reference issues with `Refs #N`. Close an issue manually, only after a verified published fork release actually contains the change, and record that release's tag. Merging a PR is not shipping.
- Triage, labels and the Project follow the standards already in place for this repository.

## Following Upstream

- **Keeping `main` current**, only when explicitly authorized, and only by a compared-commit fast-forward: fetch Upstream `main` once and record the commit `U`; require that `main` can fast-forward to `U`; require that `fork-release guard --profile mirror U` passes with nothing held; compare `.github/` between `main` and `U` and, if anything beyond issue templates changed, do not apply it by default (the runbook's Actions-off steps apply); read Upstream again just before the push and start over if it moved. Then push exactly `U:refs/heads/main`. `main` is never rewound, and nothing is refetched between the check and the push, so the one-line `git fetch && git merge --ff-only && git push` form is not used once Actions are enabled.
- **Upstream release tags** are pushed to the fork one at a time, only after `guard --profile mirror` passes on the commit the tag peels to, and are never force-pushed.
- **When Upstream tags a release,** the operator rebases `ezop` onto it (see [Force-push](#force-push)), and only while no fork release is open (every `v*-ezop.*` tag has a terminal record) and with `fork/verification/` byte-identical before and after. Drop any patch Upstream now ships an equivalent for, record the old-to-new SHA mapping, then release `vX.Y.Z-ezop.1` on the new base.
- **Activation ref sweep.** Before Actions are enabled, first time or again, the operator completes the runbook's run-inventory preflight, including the ref and pull-request sweep, inside a push freeze.
- **Changed-workflow rule.** A topic that adds, changes or removes a workflow file, a local action, `dependabot.yml` or any other `.github/` file except issue templates is pushed and reviewed with Actions off, and its self-tests are advisory. After it lands, activation repeats with a fresh preflight before Actions run again.
- **Rulesets for activation.** Before Actions are enabled, rulesets must limit the creation and update of `v*` tags, and updates of `main`, to the operator. `main-operator-only` and `tags-operator-only` in the table above do that. Whether they bind a workflow's `GITHUB_TOKEN` is not established, so they are defence in depth.
- **Manual gate ledger.** Section 10 of `fork/activation-runbook.md` lists each manual gate with its owner and recovery. These are procedures, not platform enforcement.
- **Push protection:** the `upstream` remote has push disabled locally (`git remote set-url --push upstream DISABLED`). Contributions go Upstream as PRs from this fork's `patch/*` branches.

## Patches and Upstream

Gaps are tracked as issues in this repository. Each patch:

- stays small and focused, in Upstream's style and with Upstream's tests (see Upstream's CONTRIBUTING.md);
- fits Upstream's scope rule of web-client parity, which every calendar gap in the MCP PRD does;
- is offered Upstream as an issue or PR at the same time as it ships here.

The fork does not wait for Upstream. How far it may diverge is the operator's judgment, based on how Upstream responds.

## Safety

- No credentials, session files, account data or recorded personal calendar content go into this repository, its issues or its CI logs.
- Live testing happens in `ezop-proton-calendar-mcp` under its rules: test accounts only, writes only to an "MCP Test" calendar.
