# Ezopek/proton-cli — fork rules

This is the operator's fork of [`roman-16/proton-cli`](https://github.com/roman-16/proton-cli) (Upstream, MIT). It exists to supply the Backend binary for [`Ezopek/ezop-proton-calendar-mcp`](https://github.com/Ezopek/ezop-proton-calendar-mcp), a local MCP server for Proton Calendar. That server's PRD records why the fork exists and what it should fix (PRD §7, addendum §B and §F).

This file is the only place where the fork's own conventions are written down. Everything else in the repository is Upstream's: its README, AGENTS.md, CONTRIBUTING.md and docs apply unchanged unless this file says otherwise.

## Branches

| Branch | Content | Rules |
|---|---|---|
| `main` | Pristine mirror of Upstream `main`, and the repository's default branch | Never author commits on it. Only an explicitly authorized fast-forward from `upstream/main`. |
| `ezop` | The fork's trunk: the chosen Upstream release tag, plus the fork's own commits (this file, the release configuration, the patches) | The only branch fork releases are built from. Rebased onto each next Upstream tag; `upstream/main` is never merged into it. |
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

Deletion and rebase are separate rulesets on purpose: the rebase bypass lets the operator rewrite `ezop` for an Upstream-tag rebase, and that must not also allow deleting the trunk.

Rulesets cannot tell the human owner from automation using the same owner's account. They also do not by themselves enforce that `main` holds only Upstream content, that `ezop` is updated only through PRs, or the whole boundary for enabling Actions in the future.

## Versions and tags

- **Version string:** `X.Y.Z-ezop.N`, set through ldflags. `X.Y.Z` is the Upstream release the branch is based on, and `N` counts fork releases on that base, starting at 1.
- **Release tag:** `v` + the version string, for example `v5.1.0-ezop.1`.
- **The first release, `v5.1.0-ezop.1`,** contains no functional patches. It only exists so the MCP server can lock onto a fork build from day one.
- **`proton version` must report the fork string.** The MCP server's version gate depends on this to tell fork builds from Upstream builds.
- **Fork release tags are immutable, even before publication.** Never delete, move, reuse or re-push an existing `v*-ezop.*` tag.
  - If a release run failed, retry a safe run of the original workflow against the same existing tag and commit.
  - If a release version is abandoned, use a new, explicitly authorized version and tag.
  - A tag pushed before Actions were activated, with no run, needs an explicit recovery decision, not a re-tag.
  - Abandoning `v5.1.0-ezop.1`, the first release, needs its own operator policy decision.

## Releasing

1. Rebase `ezop` onto the Upstream tag if a new one exists. Run `just lint` and `just test-fast`.
2. Tag `vX.Y.Z-ezop.N` on `ezop` and push the tag.
3. The release workflow builds binaries for every platform (Linux, macOS, Windows; amd64 and arm64) together with SHA-256 checksums, and publishes them **only** to this fork's GitHub Releases. It never publishes to Upstream's package channels (winget, npm, Homebrew, AUR, or any other).
4. In `ezop-proton-calendar-mcp`, follow its Backend release procedure (PRD FR-53): re-record fixtures, run the contract and live tests, update the docs, then update the lock with the new tag and checksums.

Weekly OpenAPI drift detection remains required by the MCP PRD (§7). A safe schedule or trigger and its mode are tracked by [#2](https://github.com/Ezopek/proton-cli/issues/2) and remain subject to separate activation decisions.

### Actions and issues

- **Actions remain disabled.** The Git workflow and rulesets above do not enable them and do not satisfy the cleanup and activation gates for the workflows inherited from Upstream, including its publisher jobs. Fork releases are GitHub Releases only.
- **Issues:** `Closes`, `Fixes` and `Resolves` in a PR merged to `ezop` do not auto-close an issue, because `main` is the default branch. Reference issues with `Refs #N`. Close an issue manually, only after a verified published fork release actually contains the change, and record that release's tag. Merging a PR is not shipping.
- Triage, labels and the Project follow the standards already in place for this repository.

## Following Upstream

- **Keeping `main` current**, only when explicitly authorized: `git fetch upstream && git checkout main && git merge --ff-only upstream/main && git push origin main`.
- **When Upstream tags a release:**
  1. The operator rebases `ezop` onto the new tag (see [Force-push](#force-push)).
  2. Drop any patch Upstream now ships an equivalent for.
  3. Release `vX.Y.Z-ezop.1` on the new base.
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
