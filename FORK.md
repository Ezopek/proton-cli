# Ezopek/proton-cli — fork rules

This is the operator's fork of [`roman-16/proton-cli`](https://github.com/roman-16/proton-cli) (Upstream, MIT). It exists to supply the Backend binary for [`Ezopek/ezop-proton-calendar-mcp`](https://github.com/Ezopek/ezop-proton-calendar-mcp), a local MCP server for Proton Calendar. That server's PRD records why the fork exists and what it should fix (PRD §7, addendum §B and §F).

This file is the only place where the fork's own conventions are written down. Everything else in the repository is Upstream's: its README, AGENTS.md, CONTRIBUTING.md and docs apply unchanged unless this file says otherwise.

## Branches

| Branch | Content | Rules |
|---|---|---|
| `main` | Mirror of Upstream `main` | Fast-forward only, from `upstream/main`. Never commit to it directly. |
| `ezop` | The latest Upstream release tag, plus the fork's own commits (this file, the release configuration, the patches) | The only branch fork releases are built from. Rebased onto each new Upstream tag. |
| `patch/<topic>` | One focused change, cut from the Upstream tag `ezop` is based on | Merged into `ezop` and offered Upstream as an issue or PR. |

Keep the Go module path `github.com/roman-16/proton-cli` unchanged, so rebases and Upstream PRs stay clean.

## Versions and tags

- **Version string:** `X.Y.Z-ezop.N`, set through ldflags. `X.Y.Z` is the Upstream release the branch is based on, and `N` counts fork releases on that base, starting at 1.
- **Release tag:** `v` + the version string, for example `v5.1.0-ezop.1`.
- **The first release, `v5.1.0-ezop.1`,** contains no functional patches. It only exists so the MCP server can lock onto a fork build from day one.
- **`proton version` must report the fork string.** The MCP server's version gate depends on this to tell fork builds from Upstream builds.

## Releasing

1. Rebase `ezop` onto the Upstream tag if a new one exists. Run `just lint` and `just test-fast`.
2. Tag `vX.Y.Z-ezop.N` on `ezop` and push the tag.
3. The release workflow builds binaries for every platform (Linux, macOS, Windows; amd64 and arm64) together with SHA-256 checksums, and publishes them **only** to this fork's GitHub Releases. It never publishes to Upstream's package channels (winget, npm, Homebrew, AUR, or any other).
4. In `ezop-proton-calendar-mcp`, follow its Backend release procedure (PRD FR-53): re-record fixtures, run the contract and live tests, update the docs, then update the lock with the new tag and checksums.

The weekly OpenAPI drift guard inherited from Upstream stays enabled on this fork. The MCP PRD requires it (§7).

## Following Upstream

- **Keeping `main` current:** `git fetch upstream && git checkout main && git merge --ff-only upstream/main && git push origin main`.
- **When Upstream tags a release:**
  1. Rebase `ezop` onto the new tag.
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
