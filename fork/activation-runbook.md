# Activation Runbook

This is the Operator's procedure for enabling GitHub Actions on the fork, for
pushing and verifying a Release Tag, for recovering from a failed release, and
for keeping `main` and `ezop` safe afterwards. `FORK.md` and the accepted
architecture spine (AD-3, AD-4, AD-7, AD-8, AD-9, AD-12, AD-13, AD-15) win over
anything written here. This file adds no fork policy of its own: where it names
a rule, it cites the document that owns it.

## 0. Authority, owner and rules

- **Owner.** The Operator. The Operator may delegate the execution of any step,
  in writing and for a named operation, to the person or tool doing the work. The
  decisions below and every stop in this file stay the Operator's and bind the
  delegate. The rebase and force-push of `ezop` (section 9.1) stay the Operator's
  own, as `FORK.md` says.
- **Start condition.** This runbook starts only after the #1 work is merged into
  `ezop`. Writing it authorized nothing.
- **It chooses nothing.** It does not take D1, D2, the Q4 risk acceptance or the
  abandonment policy for `v5.1.0-ezop.1` for anyone. Those are the Operator's
  decisions, recorded before the step that needs them (section 1.1), which also
  lists the ones already taken.
- **Every step that changes GitHub or the repository is taken by the Operator
  or the Operator's delegate, at that time, after the checks before it.** That
  covers disabling a workflow path, creating or adjusting a ruleset, removing a
  secret, changing the default branch, enabling or disabling Actions, deleting a
  branch, closing a pull request, pushing a tag or a ref (the topic of an
  evidence or record pull request included), re-running or canceling a run, and
  landing evidence or a record. A workflow never performs one (AD-4); a person
  or tool does so only for the Operator. Each such command in this file sits
  under the label **Manual Operator write**, whoever runs it. Nothing else in the
  file writes to GitHub.
- **The safe state is Actions off, and it is never given up to reach safety.**
  If a check fails, Actions stay off, or are turned off at once, and D1 returns
  to the Operator. Actions are never enabled to get out of a bad state.
- **Stop on surprise.** A command whose behavior on GitHub is not established by
  a source the spine cites is labeled **Unverified until first use**. On its
  first use, any answer that differs from the pass criterion written next to it
  (an error, an empty or differently shaped answer, a count that does not
  reconcile) is a stop. Record the surprise in words in the evidence, never as
  raw output, and do not go on.
- **Nothing in this file is live proof.** The commands were written, not run,
  except where this file records an observation with its date (sections 1.1,
  2.2, 2.4, 2.10 and 11). Section 11 lists what that leaves open.

## 1. Before you start

### 1.1 Decisions that must be recorded before step 2.1

Write the decisions down in a plain note before step 2.1 and keep it until the
activation evidence (section 5) is written; the evidence copies it. Do not start
if any item is missing.

| Decision | Needed when | Where it is recorded |
| --- | --- | --- |
| D1, option A or option B | before the core starts | the note, then the activation evidence (section 5) |
| Under A, an approved FR-14 path to the Drift Guard: owner, trigger and default-branch arrangement, timing, and the viability proof against cited constraints (AD-12: an external-infrastructure or scope decision, A2 or A3; switching to B is a different D1) | before step 2.5 | a sanitized note under `fork/evidence/`, referenced from the activation evidence |
| The `FORK.md` › Following Upstream amendment that carries the AD-13 procedure, approved | step 2.1 | `FORK.md` on `ezop` |
| The FR-12 ruleset items in force and recorded in `FORK.md` | step 2.4 | the `FORK.md` ruleset table |
| The Q4 risk and the abandonment policy for `v5.1.0-ezop.1` (the policy covers any failed first run, not Q4 alone) | before the first Release Tag, section 6.1 | a sanitized note under `fork/evidence/` (the activation evidence or one beside it) |

**Taken so far** (as of 2026-11-07, from the Operator's instruction): D1 is
**option B**, and the Operator delegated the execution of the settings, the
Actions activation and the first release. Abandonment policy for
`v5.1.0-ezop.1`: a failed first run is retried only when the retry rule allows it
(a full re-run after `verify --status` shows `record: none` and `release: None`);
when it cannot be retried, the version is abandoned and `v5.1.0-ezop.2` is used.
The two operator-only rulesets exist (section 2.4). Still to be written down: the
Q4 risk note, and D2 if the Operator wants one before the first release.

### 1.2 Tools and host

- A Linux or macOS shell with `bash`, `git`, `jq` (1.5 or newer) and the GitHub
  CLI `gh`, logged in as the Operator with admin rights on the repository.
- `go` and `just` from the pinned toolchain through `devbox` (`devbox run`, or
  `devbox shell`, inside a clone). Go 1.27.0, GoReleaser 2.17.1 and
  golangci-lint 2.13.1 come from `devbox.lock`; a result from another toolchain
  is not evidence.
- Native Windows is not supported and was not tested. The Operator-side
  commands run a downloaded binary under a POSIX environment.
- The helper reads the repository in the current directory, so it is always run
  from the root of a clean clone (section 1.4).

### 1.3 Shell setup

Run once per session, in `bash`:

```sh
set -o pipefail                       # a failed read must never look like an empty answer
GHV='X-GitHub-Api-Version: 2026-03-10'
WORK=$(mktemp -d)                     # scratch directory outside every clone
mkdir "$WORK/bin"
KEEP="${XDG_STATE_HOME:-$HOME/.local/state}/proton-cli-fork-release"   # survives the session; gate captures only
```

Conventions used by every command below:

- Every GitHub read is `gh api --hostname github.com --method GET -H "$GHV"`
  with the repository written in the path (`repos/Ezopek/proton-cli/...`). The
  `GH_REPO` variable is never used, and `--method GET` is always spelled out
  because `gh api` switches to POST when parameters are added.
- Paginated reads use `--paginate --slurp` and a pipe to `jq`. `--jq` is not
  combined with `--slurp`. `--paginate` with `--slurp` and the `Link` header
  behavior are **Unverified until first use**: the pass criterion for each
  paginated read is a reconciled count, and a mismatch is a stop.
- A push builds its refspec from a variable written as `${VAR:?}`, so an empty
  variable stops the command instead of becoming a delete refspec (`:ref`). That
  guards a typo or a failed read; it does not authorize the push.
- Raw API output goes into `$WORK` only. It is never committed, pasted into an
  issue or pull request, or copied into evidence. Delete `$WORK` when the
  session ends. The one exception is the gate-capture file, which is kept
  outside it (`$KEEP`, section 6.3).
- Times are UTC. `date -u +%Y-%m-%dT%H:%M:%SZ` gives a record time and
  `date -u +%Y%m%dT%H%M%SZ` gives an evidence file stamp.

One small function is used to compare tags between remotes. It prints the commit
a tag peels to, or nothing if the remote positively has no such tag, and it
fails if the read fails (a failed read is never absence):

```sh
peel() {  # usage: peel REMOTE TAG
  local out
  out=$(git ls-remote "$1" "refs/tags/$2" "refs/tags/$2^{}") || return 1
  printf '%s\n' "$out" | tail -n 1 | cut -f 1
}
```

### 1.4 Building the helper (the Story 1.2 recipe)

Every Operator-side command (`guard --profile`, `guard --pre-push`, `verify`,
`verify --status`) refuses a binary that has no embedded build information, was
built from a modified tree, or whose commit is not equal to or an ancestor of
the `origin/ezop` that it fetches at its start. So the helper is built with
`go build`, never `go run` or `go test`, from a clean clone of the tip you have
just fetched, with the output outside the clone (a file left inside the clone is
an untracked file and makes the stamp read as modified). Do not build it from a
linked worktree: how the stamp behaves there is Q6.

```sh
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
CLONE="$WORK/clone-$STAMP"
git clone --branch ezop https://github.com/Ezopek/proton-cli.git "$CLONE"
cd "$CLONE"
git remote add upstream https://github.com/roman-16/proton-cli.git
git remote set-url --push upstream DISABLED
git rev-parse HEAD                    # the commit this helper will be stamped with
git status --porcelain                # must print nothing
devbox run go build -o "$WORK/bin/fork-release-$STAMP" ./scripts/fork-release
FR="$WORK/bin/fork-release-$STAMP"
```

Every later section that says "build the helper again" repeats this block with
a new `STAMP`, so the earlier clone and binary are left alone.

Pass: the build succeeds and `git status --porcelain` printed nothing before it.
Stop: any build error; a non-empty status. The commands that need the helper
built at a particular commit (`guard --pre-push`, section 6.4) build their own
copy in their own clone; this one is "the tip just fetched". If `ezop` moves
while you work, fetch and build again before the next Operator-side command; a
stale helper that is still landed is accepted by the helper, but the recipe is
to build the tip you have just fetched.

### 1.5 Running the helper

- Run it from the root of the clone it was built from: `cd "$CLONE"`.
- A refusal is a line `fork-release: <check>: <reason>` on standard error, and
  nothing on standard output; the `profile` check prints one such line for each
  finding it holds. A bare `fork-release` or an unknown word is
  usage (exit 2). A mistyped shape prints one usage line (exit 2):

```text
fork-release: usage: fork-release guard --profile release|mirror COMMIT | --pre-push TAG COMMIT | --in-run | --recheck
fork-release: usage: fork-release verify --status TAG | verify TAG GATES
```

- If `origin` is not the canonical `Ezopek/proton-cli` remote on every effective
  fetch and push URL, each command prints `fork-release: origin: a fetch URL of
  origin is not the canonical Ezopek/proton-cli remote` (or the push form) and
  exits 1 before any network read.
- `guard --profile release|mirror COMMIT` prints `checked <full sha>`, then
  `release: pass` or `mirror: pass`, or `<profile>: held, N item(s)` with one
  indented line per finding. Exit 0 is a pass, 1 is held or refused.
- `guard --pre-push TAG C` prints exactly `C:refs/tags/TAG` (exit 0), or
  refuses.
- `verify TAG GATES` prints the Verification Record on standard output (exit 0),
  or prints nothing and refuses. It writes no file.
- `verify --status TAG` prints two lines, `record: <token>` and
  `release: <state>`, where the release state is one of `None`,
  `Published-Same`, `Draft`, `Other`, `Unknown`, or `not read`. Exit codes:

| Exit | Token | Meaning |
| --- | --- | --- |
| 0 | `verified` | a valid verified record agrees with the release, the tag and the runs |
| 3 | `none` | no record; the release state is printed beside it |
| 4 | `unknown` | a failed or truncated read, or a run not over; wait and read again |
| 5 | `abandoned` | a valid abandoned record; the version is spent |
| 6 | `invalid` | a malformed record, a non-file at the record path, or a disagreement; the version is spent |
| 1 | | refusal (identity, origin, usage of the checks) |
| 2 | | usage |

### 1.6 Evidence conventions

Evidence (section 5) holds counts, states, paths, versions, tag names, commit
SHAs, run ids only where a disposition needs them, and times. It never holds raw
API output, account metadata, emails, local paths or secret values. Collaborators
appear only as counts. Evidence reaches `ezop` through the `FORK.md` Git
workflow, as its own fork-only topic.

## 2. Activation core

Both options share steps 2.1 to 2.8, in this order. Steps 2.1 to 2.6 run with
Actions off. Pushes are frozen from the start of the preflight (step 2.6) until
Verified Activation (step 2.8). Any failure means Actions stay off (or are
turned off at once) and D1 returns to the Operator.

### 2.1 Step 1: #1 is merged into `ezop` and the `FORK.md` amendment is approved

```sh
cd "$CLONE"
git fetch --no-tags origin '+refs/heads/*:refs/remotes/origin/*'
git rev-parse origin/ezop
git ls-tree -r --name-only origin/ezop -- fork/activation-runbook.md fork/verification/README.md scripts/fork-release/main.go .github/workflows
git log --oneline -n 40 origin/ezop
git rev-parse origin/ezop:FORK.md
git show origin/ezop:FORK.md
```

Pass:

- the listing shows `fork/activation-runbook.md`, `fork/verification/README.md`,
  `scripts/fork-release/main.go`, `.github/workflows/ci.yml` and
  `.github/workflows/fork-release.yml`, and none of `release.yml`,
  `openapi-sync.yml`, `stats.yml`, `web.yml`;
- the log shows the topics of Stories 1.1 to 1.10 of the epic;
- the "Following Upstream" section of `FORK.md` at the tip carries the approved
  amendment: the compared-commit mirror procedure pushed as
  `U:refs/heads/main`, Upstream tag pushes after `guard --profile mirror`, the
  `.github/` comparison with not applying as the default, the rebase rule, the
  activation ref sweep and the changed-workflow rule, the final FR-12 rulesets,
  and the manual gate ledger. Record the blob id printed for `FORK.md`.

Stop: anything missing, or an amendment not yet approved.

### 2.2 Step 2: the four Upstream paths are disabled and read back (Q1 first)

First read the switch. Actions must be off:

```sh
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/actions/permissions' > "$WORK/perm.json" && jq '{enabled, allowed_actions}' "$WORK/perm.json"
```

Pass: `enabled` is `false`. Record `allowed_actions` as read. Stop: `enabled` is
`true` (turn it off at once with the write in step 2.7, using `enabled=false`,
and start again).

**Option B exception (observed 2026-11-07).** Under option B, while Actions are
off, a `GET` of each of the four paths answers HTTP 404, and so does `PUT
…/disable`. That exact answer is the expected one: record it as "not registered"
(never as disabled), skip the disable calls and the test below, and go on; the
inherited paths are decided at section 2.7a. Only a 404 qualifies. An
authentication or authorization error (401, 403), a server error, an empty or
malformed answer, or any workflow state (`active`, `deleted`, …) is still a stop.

**Q1 first (option A).** Q1 is whether a workflow can be disabled, read back and have its
runs listed while Actions are off. No official page states it. Try it on one
path only:

```sh
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/actions/workflows/release.yml' | jq -r '.state'
```

**Manual Operator write.** Unverified until first use (the call, and its
behavior while Actions are off):

```sh
gh api --hostname github.com --method PUT -H "$GHV" 'repos/Ezopek/proton-cli/actions/workflows/release.yml/disable'
```

```sh
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/actions/workflows/release.yml' | jq -r '.state'
```

A clear yes is: the disable call succeeded and the read back says
`disabled_manually`. Any other answer (an error, `active`, `deleted`, another
state, an empty answer) is not a clear yes: Actions stay off, nothing further is
changed, and D1 returns to the Operator. Under option B a path that is absent
from the new default branch may read `deleted` later (section 2.10, step 6); that
is not a clear yes either.

If Q1 was a clear yes, do the other three paths, each as a **Manual Operator
write** (same call, `openapi-sync.yml`, `stats.yml`, `web.yml`), then read all
four back:

```sh
for f in release.yml openapi-sync.yml stats.yml web.yml; do
  printf '%s ' "$f"
  gh api --hostname github.com --method GET -H "$GHV" "repos/Ezopek/proton-cli/actions/workflows/$f" | jq -r '.state' || { echo "STOP: read failed for $f"; break; }
done
```

Pass: all four print `disabled_manually`. Stop: any other state or a failed
read. The workflow list is read in full in section 3.7.

**Observed 2026-11-07, Actions off.** A `GET` of each of the four paths and a
`PUT …/disable` returned HTTP 404, and the workflow list was empty. A 404 is not
a state: it is not `disabled_manually` and is never recorded as disabled. It
means GitHub has no workflow object to read or change while the switch is off, so
the clear yes above is not available here. Nothing more is claimed about Q1.

**Reading rule for option B.** Wherever this runbook says the four states read
`disabled_manually` (2.8, 3.10, 6.11, 8.1, 9), under option B read:

| Path | Pass |
| --- | --- |
| `web.yml` | no run in the window and, if it registers, not left `active` (section 2.7a); not registered is accepted. A run of it is a stop |
| `release.yml`, `openapi-sync.yml`, `stats.yml` | `disabled_manually`, or not registered (404) once Actions are on: their `schedule`, `workflow_run` and dispatch triggers need the file on the default branch (`ezop`), where they do not exist (documented rule). `active` is not a pass: disable it by id |

`web.yml` differs because it also has a `push` trigger that reads `main`'s own
file, so being absent from the default branch does not stop it.

### 2.3 Step 3: the seven-secret readback (names only)

The seven Upstream secrets are `AUR_KEY`, `TAP_WINGET_TOKEN`, `NPM_TOKEN`,
`GPG_PRIVATE_KEY`, `GPG_PRIVATE_KEY_PASSPHRASE`, `WEB_DEPLOY_KEY` and
`STATS_TOKEN`. Repository level:

```sh
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/actions/secrets?per_page=100' > "$WORK/secrets-repo.json"
jq '{total_count: .[0].total_count, enumerated: ([.[].secrets[]] | length)}' "$WORK/secrets-repo.json"
jq --argjson seven '["AUR_KEY","TAP_WINGET_TOKEN","NPM_TOKEN","GPG_PRIVATE_KEY","GPG_PRIVATE_KEY_PASSPHRASE","WEB_DEPLOY_KEY","STATS_TOKEN"]' '[.[].secrets[].name | select(. as $n | $seven | index($n))] | length' "$WORK/secrets-repo.json"
```

Every environment:

```sh
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/environments?per_page=100' > "$WORK/envs.json"
jq '{total_count: .[0].total_count, enumerated: ([.[].environments[]] | length)}' "$WORK/envs.json"
for e in $(jq -r '.[].environments[].name | @uri' "$WORK/envs.json"); do
  gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp "repos/Ezopek/proton-cli/environments/$e/secrets?per_page=100" > "$WORK/secrets-env.json" || { echo "STOP: read failed"; break; }
  jq --argjson seven '["AUR_KEY","TAP_WINGET_TOKEN","NPM_TOKEN","GPG_PRIVATE_KEY","GPG_PRIVATE_KEY_PASSPHRASE","WEB_DEPLOY_KEY","STATS_TOKEN"]' '{total_count: .[0].total_count, enumerated: ([.[].secrets[]] | length), of_the_seven: ([.[].secrets[].name | select(. as $n | $seven | index($n))] | length)}' "$WORK/secrets-env.json"
done
```

Pass: for the repository and for every environment, `total_count` equals
`enumerated` and `of_the_seven` is 0. Record the number of environments and the
number of secrets, as counts. Stop: any of the seven present, a count that does
not reconcile, a failed read. Any other secret name is not one of the seven, but
the release uses only `GITHUB_TOKEN`: record the count, and the Operator records
why it stays or removes it (a **Manual Operator write**) before going on.

### 2.4 Step 4: the settings readback

```sh
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/actions/permissions/workflow' | jq '{default_workflow_permissions, can_approve_pull_request_reviews}'
```

Pass: `default_workflow_permissions` is `read` and
`can_approve_pull_request_reviews` is `false`.

The policy for fork pull requests from outside contributors. **Unverified until
first use**: the exact path and values are not established by a source the spine
cites. Try the path below; if it is rejected, read the same setting in the
repository settings (Actions, General, fork pull request workflows; no command)
and compare it with what GitHub's documentation for that setting says at that
time:

```sh
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/actions/permissions/fork-pr-contributor-approval'
```

Pass: the setting requires approval for fork pull requests from outside
contributors, and its exact value string is recorded. Stop: it cannot be read,
or it does not require approval.

Operator-only write access (collaborators and pending invitations, as counts):

```sh
ME=$(gh api --hostname github.com --method GET -H "$GHV" 'user' | jq -r '.login')
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/collaborators?affiliation=all&per_page=100' > "$WORK/collab.json"
jq --arg me "$ME" '{total: ([.[][]] | length), others_with_write: ([.[][] | select(.login != $me) | select(.permissions.push == true or .permissions.maintain == true or .permissions.admin == true)] | length)}' "$WORK/collab.json"
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/invitations?per_page=100' | jq '[.[][]] | length'
```

Pass: `others_with_write` is 0 and there are no pending invitations. The field
names are **Unverified until first use**. Record counts only, never logins.

The four rulesets recorded in `FORK.md`, read back and compared with its table:

```sh
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/rulesets?per_page=100' > "$WORK/rulesets.json"
jq -r '.[][] | [.id, .name, .target, .enforcement] | @tsv' "$WORK/rulesets.json"
for id in $(jq -r '.[][].id' "$WORK/rulesets.json"); do
  gh api --hostname github.com --method GET -H "$GHV" "repos/Ezopek/proton-cli/rulesets/$id" > "$WORK/ruleset-$id.json" || { echo "STOP: read failed"; break; }
  jq -S '{name, target, enforcement, include: .conditions.ref_name.include, exclude: .conditions.ref_name.exclude, rules: [.rules[].type], bypass: [.bypass_actors[] | {actor_id, actor_type, bypass_mode}]}' "$WORK/ruleset-$id.json"
done
MEID=$(gh api --hostname github.com --method GET -H "$GHV" 'user' | jq -r '.id')
jq -r --argjson me "$MEID" '[.name, ([.bypass_actors[] | select(.actor_type == "User" and .actor_id == $me)] | length), (.bypass_actors | length)] | @tsv' "$WORK"/ruleset-*.json
```

The last command compares each ruleset's bypass actors with the verified account
without printing the identifier: the columns are the ruleset name, the number of
bypass actors that are that account, and the number of bypass actors. Operator-only
rulesets (`ezop-rebase`, `main-operator-only`, `tags-operator-only`) must show 1 and
1; the others must show 0 and 0. A different user, or a second actor, is a stop.

Pass, per the `FORK.md` table: `main-mirror` (branch, `refs/heads/main`: deletion
and non-fast-forward, no bypass), `fork-release-tags` (tag, `refs/tags/v*-ezop.*`:
deletion and update, no bypass), `ezop-deletion` (branch, `refs/heads/ezop`:
deletion, no bypass) and `ezop-rebase` (branch, `refs/heads/ezop`:
non-fast-forward, one user bypass, always), all with `enforcement` `active`.
Rule type names, `include` and `bypass` shapes are **Unverified until first
use**.

**The FR-12 ruleset items are an unmet prerequisite until you have put them in
force and read them back.** PRD FR-12 requires a tag ruleset that lets only the
Operator create, update or delete `v*` tags, and, on `main`, force-push and
deletion blocked and updates restricted to the Operator. The four recorded
rulesets do not meet it (`fork-release-tags` covers update and deletion of
`v*-ezop.*` only, with no creation rule; `main-mirror` has no update
restriction). The existing `main-mirror` and `fork-release-tags` keep no bypass
and stay untouched because AD-13 depends on them; the items are met by separate,
layered rulesets (for example a `v*` tag ruleset with creation, update and
deletion rules and the Operator as its only bypass actor, and a `main` ruleset
with an update rule and the same bypass). The exact form is the Operator's;
creating each ruleset is a **Manual Operator write** (repository settings, or
the ruleset endpoints, **Unverified until first use**); it is recorded in the
`FORK.md` ruleset table, and the readback above shows it.

Pass: the readback shows active rulesets for `refs/tags/v*` and for
`refs/heads/main` in the form recorded in `FORK.md`, matching the FR-12 wording
above, and the four original rulesets still match their rows. Stop: any item
missing or different. Whether any of these rulesets binds a workflow's
`GITHUB_TOKEN`, and whether the layers compose as intended, is part of Q3 and is
not proven by the readback.

**Observed 2026-11-07 (read back from the saved responses).**
`main-operator-only` (id 26741791): branch `refs/heads/main`, rule `update`
(`update_allows_fetch_and_merge: false`), bypass user `Ezopek`, always.
`tags-operator-only` (id 26741823): tag `refs/tags/v*`, rules `creation`, `update`
(same parameter) and `deletion`, bypass user `Ezopek`, always. Both are active and
sit beside the four recorded rulesets, which were not changed (`main-mirror` was
read back unchanged). `fork-release-tags` keeps its no-bypass update and deletion
rules on `v*-ezop.*`, so fork release tags stay immutable even for the operator.
In both new rulesets the single bypass actor is that account: its id equals the
account id in the account's commit address (compared offline, not reproduced here).

### 2.5 Step 5: the option-specific state

Apply section 2.9 (option A) or section 2.10 (option B), whichever D1 recorded.
Record `default_branch` after it:

```sh
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli' | jq -r '.default_branch'
```

Pass: `main` under option A, `ezop` under option B. Stop: any other value,
or a failed read.

### 2.6 Step 6: a fresh preflight

Run the whole Run-Inventory Preflight (section 3) now, inside the push freeze.
Under option B first do the preparation of section 2.7a (probe branch and freeze),
so that the preflight is taken with both in place; the policy is not changed
before it.
A settings change other than the chosen option's own bracketed steps and the planned transitions of section 2.7a voids it.
Stop: any stop in section 3; a push, new run, re-enable or workflow change in the
window.

### 2.7 Step 7: enable Actions, last

Do not start if the preflight is stale or the freeze was broken. Under option B
the enable is the bounded attempt of section 2.7a, not a plain switch. Choose a
start that keeps all of steps 2.7 and 2.8 clear of 00:00 and 01:00 UTC, and check
the clock first:

```sh
date -u +%Y-%m-%dT%H:%M:%SZ
```

**Manual Operator write.** Unverified until first use (the request body and
whether `allowed_actions` is left as it was):

```sh
gh api --hostname github.com --method PUT -H "$GHV" 'repos/Ezopek/proton-cli/actions/permissions' -F enabled=true
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/actions/permissions' | jq '{enabled, allowed_actions}'
```

Pass: `enabled` is `true` and `allowed_actions` is as recorded in step 2.2 (under
option B it is read for the first time right after the enable and then set to
`selected` as section 2.7a says).
Stop: the switch does not read `true`, or `allowed_actions` changed: turn Actions
off (below) and D1 returns to the Operator.

To turn Actions off again (the safe direction, any time a check fails), the same
**Manual Operator write** with `-F enabled=false`, then read back `enabled:
false`.

### 2.7a Option B: the bounded enable

The inherited paths cannot be read or disabled while Actions are off (404, section
2.2), and a workflow file that exists only on a non-default branch is not expected
to register until it has run. So under option B the enable is one bounded step,
and its protection is what can happen during it, not a per-path disable. It is
not a proof: GitHub documents no atomic enable-and-disable, no run does not show
that no event is pending, and an actor with administration rights can change
settings. What stays open is listed at the end of this section, not waived.

While Actions are off GitHub also hides the policy: the permissions read answers
only `enabled` and `sha_pinning_required` (observed 2026-11-07), and the
selected-actions read answers 409. So the original `allowed_actions` is
**unknown** until Actions are on, whether a policy can be written while they are
off is **untested**, and the runbook claims neither. The practical path is a
bounded interval under the existing policy, whose breadth is unknown until the
first readback: with the probe branch, the no-bypass freeze and the fresh
preflight already in place, Actions are enabled once with `{"enabled":true}`
only, the now-visible original mode and list are captured, and the restriction is
set and read back while the freeze is held. The freeze bounds that interval; it
does not prove that nothing runs in it.

Two rules run through every command below. **A response is never the truth:** a
write can apply and still report an error, and a read can fail without meaning
"absent", so every write is reconciled with a successful readback, and a failed
read is a stop, never "nothing there". **Ownership is tracked separately from
the attempt:** a ref or ruleset is deleted only if a readback confirms that this
attempt created it; anything uncertain is left alone and reported. Nothing is
ever reported as restored unless a fresh read shows it, and an original value
that was never captured is never invented.

1. **Reads and helpers.** Installed apps cannot be listed with a user token (`GET
   …/installation` answered 401 and `GET /user/installations` 403 on
   2026-11-07): record them as **unknown**, never as none. List the recent
   repository events and record how many actors are bots. Section 4 is otherwise
   complete; any other missing read is a stop. Record the off-state policy read
   as it is, then define the helpers once per session:

```sh
GH() {  # every call names its method: the CLI silently switches to POST when parameters are added
  case " $* " in *" --method "*) ;; *) echo "GH: --method is required" >&2; return 2 ;; esac
  gh api --hostname github.com -H "$GHV" "$@"
}
REPO=repos/Ezopek/proton-cli
GH --method GET "$REPO/actions/permissions" > "$WORK/perm-off.json"; echo "off-state read: exit $?"
jq -c . "$WORK/perm-off.json"
GH --method GET "$REPO/actions/permissions/selected-actions" > /dev/null; echo "selected-actions read while off: exit $? (expected: an error)"
```

```sh
rref() {  # SHA of a remote branch, nothing if absent; FAILS if the remote cannot be read
  local out; out=$(git ls-remote --heads origin "refs/heads/$1") || return 1
  printf '%s\n' "$out" | cut -f 1
}
polget() { GH --method GET "$REPO/actions/permissions$1"; }
polput() { GH --method PUT "$REPO/actions/permissions$1" --input -; }
WANT='{"github_owned_allowed":false,"verified_allowed":false,"patterns_allowed":["actions/checkout@*","actions/setup-go@*","goreleaser/goreleaser-action@*","jetify-com/devbox-install-action@*","cachix/install-nix-action@*","actions/cache/restore@*","actions/cache/save@*"]}'
pol_verify() {  # usage: pol_verify true|false. 0 only if both reads succeed and show the intended restriction
  local p s
  p=$(polget '') && s=$(polget /selected-actions) || { echo "policy read failed"; return 1; }
  printf '%s' "$p" | jq -e --argjson en "$1" '.enabled == $en and .allowed_actions == "selected"' >/dev/null || { echo "permissions differ"; return 1; }
  printf '%s' "$s" | jq -e --argjson want "$WANT" '.github_owned_allowed == false and .verified_allowed == false and ((.patterns_allowed | sort) == ($want.patterns_allowed | sort))' >/dev/null || { echo "selected-actions differ"; return 1; }
}
pol_orig_ok() {  # 0 only if a fresh read SHOWS Actions off and the captured original mode (and list); hidden fields are not a match
  local p s mode
  mode=$(jq -r '.allowed_actions // empty' "$WORK/perm-orig.json" 2>/dev/null)
  [ -n "$mode" ] || { echo "no original mode was captured"; return 1; }
  p=$(polget '') || { echo "restore check: read failed"; return 1; }
  printf '%s' "$p" | jq -e '.enabled == false' >/dev/null || { echo "restore check: Actions are not off"; return 1; }
  printf '%s' "$p" | jq -e --arg m "$mode" '.allowed_actions == $m' >/dev/null || { echo "restore check: the mode is hidden or differs while off; cannot confirm"; return 1; }
  if [ "$mode" = selected ]; then
    s=$(polget /selected-actions) || { echo "restore check: list read failed"; return 1; }
    printf '%s' "$s" | jq -e --slurpfile o "$WORK/sel-orig.json" '.github_owned_allowed == $o[0].github_owned_allowed and .verified_allowed == $o[0].verified_allowed and ((.patterns_allowed | sort) == ($o[0].patterns_allowed | sort))' >/dev/null || { echo "restore check: list differs"; return 1; }
  fi
}
pol_restore() {  # try to restore ONLY the captured original; the result is CONFIRMED only if a fresh read shows it
  local rc=0 mode
  mode=$(jq -r '.allowed_actions // empty' "$WORK/perm-orig.json" 2>/dev/null)
  if [ -z "$mode" ]; then echo "UNCONFIRMED: no original mode was captured; nothing is restored and none is invented"; return 1; fi
  jq '{enabled: false, allowed_actions} + (if has("sha_pinning_required") then {sha_pinning_required} else {} end)' "$WORK/perm-orig.json" | polput '' >/dev/null || { echo "restore: the permissions write was rejected"; rc=1; }
  if [ "$mode" = selected ]; then
    jq '{github_owned_allowed, verified_allowed, patterns_allowed}' "$WORK/sel-orig.json" | polput /selected-actions >/dev/null || { echo "restore: the list write was rejected"; rc=1; }
  fi
  pol_orig_ok || rc=1
  if [ "$rc" = 0 ]; then echo "RESTORED and read back"; else echo "UNCONFIRMED: the original policy is not confirmed restored (while Actions are off GitHub may hide it; read it at the next enable)"; fi
  return $rc
}
rs_id() {  # id of the ruleset with that name, nothing if absent; FAILS if the list cannot be read
  local out; out=$(GH --method GET --paginate --slurp "$REPO/rulesets?per_page=100") || return 1
  printf '%s' "$out" | jq -r --arg n "$1" '.[][] | select(.name == $n) | .id'
}
rs_gone() {  # 0 = answers 404 (gone), 1 = still exists, 2 = ambiguous answer
  local out; if out=$(GH --method GET "$REPO/rulesets/$1" 2>&1); then return 1; fi
  printf '%s' "$out" | grep -q 'HTTP 404' && return 0; return 2
}
rs_check() { GH --method GET "$REPO/rulesets/$1" | jq -e '.enforcement == "active" and .conditions.ref_name.include == ["~ALL"] and (.bypass_actors | length) == 0' >/dev/null; }
```

2. **Probe branch, freeze, probes.** All three blocks share the variables of the
   first one, so run them in one shell session. Names are stamped and must be
   absent, the probe commit comes from the remote tip `EZ` and not from the local
   `ezop`, and ownership is `none`, `confirmed`, `gone` or `unknown`: it becomes
   `confirmed` only when a successful readback shows the value this attempt
   pushed, whatever the push reported. **Manual Operator write.**

```sh
cd "$CLONE"
STAMPP=$(date -u +%Y%m%dT%H%M%SZ); PROBE="zz-activation-probe-$STAMPP"; NEWP="zz-activation-new-$STAMPP"
PROBE_OWN=none; NEWP_OWN=none; PROBE_ATT=0; NEWP_ATT=0; ERR=0
git fetch --no-tags origin '+refs/heads/*:refs/remotes/origin/*' || ERR=1
EZ=$(git ls-remote origin refs/heads/ezop | cut -f 1) || ERR=1
if [ -n "$EZ" ] && git cat-file -e "$EZ^{commit}"; then P1=$(git rev-parse "$EZ~1") || ERR=1; else ERR=1; fi
git ls-remote --refs origin 'refs/heads/*' 'refs/tags/*' > "$WORK/refs-before-probe.txt" || ERR=1
A=$(rref "$PROBE") || ERR=1; B=$(rref "$NEWP") || ERR=1
if [ "$ERR" = 1 ]; then echo "STOP: a read failed; nothing was pushed"
elif [ -n "$A$B" ]; then echo "STOP: a probe name already exists; use a new stamp and touch nothing"
else
  PROBE_ATT=1
  git push origin "${P1:?}:refs/heads/$PROBE"; rc=$?
  if cur=$(rref "$PROBE"); then
    if [ "$cur" = "$P1" ]; then PROBE_OWN=confirmed; echo "probe branch created and owned (push exit $rc)"
    elif [ -z "$cur" ]; then echo "STOP: the probe branch was not created (push exit $rc)"
    else PROBE_OWN=unknown; echo "STOP: the probe branch exists with an unexpected value"; fi
  else PROBE_OWN=unknown; echo "STOP: the probe branch cannot be confirmed (push exit $rc); leave it"; fi
fi
[ "$PROBE_OWN" = confirmed ]
```

   Then the freeze: two rulesets on `~ALL` with no bypass actor, so that even the
   Operator is rejected until they are deleted. The bodies are the ones accepted
   in a trial on 2026-11-07 (ids 26743988 and 26743990, read back, then deleted;
   **not** active now). Their ids are found by name right after the writes,
   whatever the writes answered, and recorded in `$WORK/freeze-ids` (keep `$WORK`
   until the release); only recorded ids are ever deleted, never a ruleset found
   later by its name.
   **Manual Operator write.**

```sh
FREEZE_OK=0
if ! FB=$(rs_id activation-freeze-branches) || ! FT=$(rs_id activation-freeze-tags); then echo "STOP: cannot read the rulesets"; FB=; FT=
elif [ -n "$FB$FT" ]; then echo "STOP: a freeze ruleset already exists and is not ours; touch nothing"; FB=; FT=
else
  printf '%s' '{"name":"activation-freeze-branches","target":"branch","enforcement":"active","conditions":{"ref_name":{"include":["~ALL"],"exclude":[]}},"rules":[{"type":"creation"},{"type":"update"},{"type":"deletion"},{"type":"non_fast_forward"}],"bypass_actors":[]}' | GH --method POST "$REPO/rulesets" --input - >/dev/null; echo "create branches: exit $?"
  printf '%s' '{"name":"activation-freeze-tags","target":"tag","enforcement":"active","conditions":{"ref_name":{"include":["~ALL"],"exclude":[]}},"rules":[{"type":"creation"},{"type":"update"},{"type":"deletion"}],"bypass_actors":[]}' | GH --method POST "$REPO/rulesets" --input - >/dev/null; echo "create tags: exit $?"
  if FB=$(rs_id activation-freeze-branches) && FT=$(rs_id activation-freeze-tags); then printf '%s\n%s\n' "$FB" "$FT" > "$WORK/freeze-ids"
  else echo "STOP: cannot read the rulesets back; a freeze ruleset may exist: find it by name, check that it is the one just created, and delete it by its id"; FB=; FT=; fi
  if [ -n "$FB" ] && [ -n "$FT" ] && rs_check "$FB" && rs_check "$FT"; then FREEZE_OK=1; echo "FREEZE READY: both active, ~ALL, no bypass actor"; else echo "STOP: the freeze is not as intended"; fi
fi
[ "$FREEZE_OK" = 1 ]
```

   Then the three probes. Each must be rejected **by the repository rules** (the
   answer names GH013 or rule violations). After each one the probe refs are
   read back and ownership is reconciled, because even a failed push may have
   applied; the first problem ends the probes. A probe that is accepted means the
   freeze does not hold.

```sh
PROBES_OK=1
expect_rejected() {  # usage: expect_rejected DESCRIPTION GIT_PUSH_ARGS... ; 0 rejected by the rules, 1 other failure, 2 accepted
  local what=$1 out; shift
  if out=$(git push "$@" 2>&1); then echo "STOP: $what was ACCEPTED"; return 2; fi
  if printf '%s\n' "$out" | grep -q -E 'GH013|rule violations'; then echo "PASS: $what rejected by the repository rules"; return 0; fi
  echo "STOP: $what failed for another reason"; return 1
}
probe_reconcile() {  # read both probe refs, set the ownership words, 0 only if they are unchanged
  local a b
  if ! a=$(rref "$PROBE") || ! b=$(rref "$NEWP"); then PROBE_OWN=unknown; NEWP_OWN=unknown; echo "STOP: cannot read the probe refs"; return 1; fi
  case "$a" in "$P1"|"$EZ") PROBE_OWN=confirmed ;; "") PROBE_OWN=gone ;; *) PROBE_OWN=unknown ;; esac
  case "$b" in "$EZ") NEWP_OWN=confirmed ;; "") NEWP_OWN=none ;; *) NEWP_OWN=unknown ;; esac
  [ "$a" = "$P1" ] && [ -z "$b" ] || { echo "STOP: the probe refs changed (probe=${a:-absent} new=${b:-absent})"; return 1; }
}
if [ "${FREEZE_OK:-0}" != 1 ] || [ "${PROBE_OWN:-none}" != confirmed ]; then echo "STOP: the probes need an owned probe branch and a verified freeze"; PROBES_OK=0; fi
for step in creation update deletion; do
  [ "$PROBES_OK" = 1 ] || break
  case $step in
    creation) NEWP_ATT=1; expect_rejected "branch creation" origin "${EZ:?}:refs/heads/$NEWP" ;;
    update) expect_rejected "branch update" origin "${EZ:?}:refs/heads/$PROBE" ;;
    deletion) expect_rejected "branch deletion" origin --delete "$PROBE" ;;
  esac
  rc=$?
  probe_reconcile || PROBES_OK=0
  [ "$rc" = 0 ] || PROBES_OK=0
done
if [ "$PROBES_OK" = 1 ]; then echo "PROBES PASSED: three rejections by the rules, refs unchanged"; else echo "PROBES FAILED: run the recovery of step 9"; fi
[ "$PROBES_OK" = 1 ]
```

   The rejections show that creation, update and deletion of a branch are
   covered for the Operator. They show nothing about an actor with administration
   rights, who can delete a ruleset.
3. **Preflight inside the freeze.** Now run step 2.6. Its ref snapshot contains the
   probe branch, which is expected; the fresh readback of section 3.10 must find
   the same refs. Any stop in it goes to step 9. Nothing about the policy has been
   changed yet.
4. **One enable, with only `{"enabled":true}`, and the capture.** The write of step
   2.7, only when `FREEZE READY` and `PROBES PASSED` printed and the preflight
   passed. `allowed_actions` is omitted, so the policy in force stays whatever it
   was; then the now-visible original mode (and selected list) is read and saved.
   An unclear enable answer is settled by the readback. If Actions are on and the
   mode cannot be read, or the mode is `selected` and its list cannot be read,
   that is a stop with Actions on: go to step 9 at once. Do not push to `main`.
   **Manual Operator write:**

```sh
ENABLED_OK=0
if [ "${FREEZE_OK:-0}" = 1 ] && [ "${PROBES_OK:-0}" = 1 ]; then
  printf '%s' '{"enabled":true}' | polput '' >/dev/null; echo "enable write: exit $?"
  if ! p=$(polget ''); then echo "STOP: cannot read the policy after the enable; turn Actions off (step 9)"
  elif [ "$(printf '%s' "$p" | jq -r '.enabled')" != true ]; then echo "STOP: Actions are still off (the enable did not apply); nothing to turn off"
  elif ! printf '%s' "$p" | jq -e '.allowed_actions | type == "string"' >/dev/null; then echo "STOP: Actions are on but the mode is not visible; turn Actions off (step 9)"
  else
    printf '%s' "$p" > "$WORK/perm-orig.json"
    if [ "$(jq -r '.allowed_actions' "$WORK/perm-orig.json")" = selected ] && ! { polget /selected-actions > "$WORK/sel-orig.json" && jq -e . "$WORK/sel-orig.json" >/dev/null; }; then echo "STOP: the original mode is selected but its list cannot be read; turn Actions off (step 9)"
    else ENABLED_OK=1; echo "ENABLED; the original policy in force is captured: $(jq -r '.allowed_actions' "$WORK/perm-orig.json")"; fi
  fi
else echo "STOP: the freeze or the probes are not confirmed; do not enable"; fi
[ "$ENABLED_OK" = 1 ]
```

5. **Restrict, while Actions are on and the freeze is held.** Allow only the
   actions the two workflows on `ezop` use (`actions/checkout`,
   `actions/setup-go`, `goreleaser/goreleaser-action`,
   `jetify-com/devbox-install-action`, `cachix/install-nix-action`) and the two
   cache actions that the pinned Devbox action calls (`actions/cache/restore`,
   `actions/cache/save`; named by a source reading of that action, not
   live-tested), each at any version so that a pin bump needs no settings write,
   with **both** allowance flags false. This keeps out the third-party actions
   that `web.yml` needs (`oven-sh/setup-bun`, `peaceiris/actions-gh-pages`);
   whether a workflow that uses a disallowed action fails at startup is
   **untested**. It does not stop a `run:` shell step in a workflow whose actions
   are allowed: the other inherited workflows are held back only by the
   default-branch trigger rule. The write exit codes are information only; the
   readback decides. **Manual Operator write** (**Unverified until first use**;
   the list call requires `selected` to be set by the first):

```sh
POLICY_OK=0
if [ "${ENABLED_OK:-0}" = 1 ]; then
  jq '{enabled: true, allowed_actions: "selected"} + (if has("sha_pinning_required") then {sha_pinning_required} else {} end)' "$WORK/perm-orig.json" | polput '' >/dev/null; echo "permissions write: exit $?"
  printf '%s' "$WANT" | polput /selected-actions >/dev/null; echo "selected-actions write: exit $?"
  if pol_verify true; then POLICY_OK=1; echo "POLICY READY: the restriction is read back with Actions on"; else echo "STOP: the restriction is not established; turn Actions off first (step 9)"; fi
else echo "STOP: Actions are not enabled by this attempt; nothing to configure"; fi
[ "$POLICY_OK" = 1 ]
```

   Pass: `POLICY READY`. Anything else goes to step 9 now, with Actions on and the
   policy in force still the captured original: **Actions are turned off first**,
   independently of any restoration.
6. **Readback at once and again after about two minutes**: `enabled` is `true`,
   `allowed_actions` is `selected` with the seven patterns and both flags false
   (`pol_verify true`); the workflows and runs are listed; refs, tags, pull
   requests and settings are as the section 3.10 snapshot; the six recorded
   rulesets and the two freeze rulesets read back unchanged.

```sh
pol_verify true && echo "policy still as intended"
GH --method GET --paginate --slurp "$REPO/actions/workflows?per_page=100" > "$WORK/wf-after-enable.json"
jq -r '.[].workflows[] | [.id, .path, .state] | @tsv' "$WORK/wf-after-enable.json"
GH --method GET --paginate --slurp "$REPO/actions/runs?per_page=100" > "$WORK/runs-after-enable.json"
jq -c '[.[].workflow_runs[] | [.id, .run_attempt, .status, .conclusion, .updated_at]] | sort' "$WORK/runs-after-enable.json" | diff "$WORK/runs-fresh.sig" - && echo "runs unchanged"
```

7. **Pass** (the readback condition of Verified Activation under option B): no run
   (`runs unchanged`); the workflow list holds `ci.yml`, `fork-release.yml` and
   nothing `active` that is not one of them; step 6 as recorded. `web.yml` need not
   be registered or disabled: its only trigger left is a push to `main`, which the
   freeze holds now and `main-operator-only` holds after it. If it registers
   `active`, disable it by id as hygiene (**Manual Operator write**, `PUT
   …/actions/workflows/{id}/disable`, expected 204). **Keep the freeze and the
   probe branch** and go on to step 2.8, which is part of the same window.
8. **Release, only after step 2.8 has passed.** Delete the two freeze rulesets by
   the exact ids recorded above, then delete only the refs whose ownership is
   `confirmed`, and check that the refs are back to the snapshot of step 2. A
   deletion that is not confirmed gone keeps its id: repeat it for that id
   only, never recreate. **Manual Operator write.**

```sh
RS_OK=1
if [ -z "${FB:-}${FT:-}" ] && [ -s "$WORK/freeze-ids" ]; then FB=$(sed -n 1p "$WORK/freeze-ids"); FT=$(sed -n 2p "$WORK/freeze-ids"); fi
for id in "${FB:-}" "${FT:-}"; do
  [ -n "$id" ] || { echo "no id for a freeze ruleset: nothing to delete for it"; continue; }
  rs_gone "$id"; r=$?
  if [ "$r" = 0 ]; then echo "ruleset $id is already gone"; continue; fi
  GH --method DELETE "$REPO/rulesets/$id" >/dev/null; echo "delete $id: exit $?"
  rs_gone "$id"; r=$?
  if [ "$r" = 0 ]; then echo "ruleset $id confirmed gone"; else echo "STOP: ruleset $id not confirmed gone (state $r); keep this id and repeat for it only"; RS_OK=0; fi
done
[ "$RS_OK" = 1 ] && GH --method GET --paginate --slurp "$REPO/rulesets?per_page=100" | jq -r '.[][] | [.id, .name] | @tsv'
[ "$RS_OK" = 1 ]
```

```sh
CLEAN_OK=1
for b in "$PROBE" "$NEWP"; do
  if [ "$b" = "$PROBE" ]; then own=${PROBE_OWN:-none}; att=${PROBE_ATT:-0}; else own=${NEWP_OWN:-none}; att=${NEWP_ATT:-0}; fi
  if ! cur=$(rref "$b"); then echo "STOP: cannot read $b; delete nothing for it"; CLEAN_OK=0; continue; fi
  if [ -z "$cur" ]; then echo "$b is absent (attempted=$att, ownership $own)"; [ "$own" != unknown ] || CLEAN_OK=0; continue; fi
  if [ "$own" = confirmed ] && { [ "$cur" = "$P1" ] || [ "$cur" = "$EZ" ]; }; then
    git push origin --delete "$b"; echo "delete $b: exit $?"
    if after=$(rref "$b") && [ -z "$after" ]; then echo "$b confirmed gone"; else echo "STOP: $b not confirmed gone"; CLEAN_OK=0; fi
  elif [ "$att" = 0 ]; then echo "$b exists but this attempt never tried to create it; left untouched"
  else echo "STOP: $b is not confirmed as created by this attempt (attempted=$att, ownership $own); left in place"; CLEAN_OK=0; fi
done
git ls-remote --refs origin 'refs/heads/*' 'refs/tags/*' > "$WORK/refs-after-cleanup.txt" || CLEAN_OK=0
if diff "$WORK/refs-before-probe.txt" "$WORK/refs-after-cleanup.txt"; then echo "refs back to the snapshot"; else CLEAN_OK=0; fi
[ "$CLEAN_OK" = 1 ]
```

9. **Recovery: every stop in steps 1 to 8 comes here, in this order.** (a) If
   Actions might be on, turn them off **first and on their own**, then confirm by a
   fresh read; repeat until `ACTIONS ARE OFF` prints, and do nothing else before.
   (b) Separately, if the policy was already changed, try to restore only the captured original with
   `pol_restore`; it prints `RESTORED and read back` only if a fresh read shows
   it, and otherwise `UNCONFIRMED`, which is reported as it is (the policy fields
   are hidden while Actions are off, so the next enable shows what is in force).
   An original that was never captured is not invented, and a stop before step 4 has nothing to restore. (c) List the non-terminal
   runs, cancel the unsafe ones and read each to a terminal state. (d) Delete the
   freeze rulesets and the refs this attempt created with the two blocks of step
   8, which use the recorded ids and the confirmed ownership only; if the freeze
   had already been deleted and Actions are on, create it again first (step 2) and
   note the new ids. (e) Keep the completed-job evidence and inspect refs,
   releases, artifacts and outside effects. Do not delete evidence, re-run an
   inherited workflow, or move or delete a tag, and never delete a ref or ruleset
   this attempt did not create. **Manual Operator write:**

```sh
OFF_OK=0
printf '%s' '{"enabled":false}' | polput '' >/dev/null; echo "disable write: exit $?"
if p=$(polget '') && [ "$(printf '%s' "$p" | jq -r '.enabled')" = false ]; then OFF_OK=1; echo "ACTIONS ARE OFF (read back)"; else echo "STOP: Actions are NOT confirmed off; repeat this block until they are"; fi
[ "$OFF_OK" = 1 ]
```

**Open, not waived.** (0) Between the enable and the end of step 5 the policy in force is the original, whose breadth is unknown until the capture; the freeze bounds that interval and zero known pending events make it short, but no run being visible is not proof that none started. (a) An actor with administration rights, such as an
installed app with that permission, can delete the freeze or change settings in
the window; installed apps stay unknown. (b) A pull request opened by an outside
account creates an event the freeze cannot hold (read-only token, no secrets, the
approval policy applies); the readback would show its run. (c) Whether rulesets
bind a workflow's `GITHUB_TOKEN` (Q3) and how a disallowed action fails are
unverified. (d) After the window, the first mirror push to `main` that touches
`web.yml`'s paths starts a Web run from `main`'s own file: a read-only job with no
deploy key. The positive `disabled_manually` for `web.yml` is not required because
it cannot be obtained without running the workflow, and it would not remove (a)
or (b).

### 2.8 Step 8: the post-enable readback, inside the freeze (Verified Activation)

Run, in this order:

1. The inherited paths again. Option A: the loop in step 2.2, pass all four
   `disabled_manually`. Option B: the listing and the states of section 2.7a, pass
   as in the reading rule of section 2.2.
2. The run list again, written to a new file and compared with the signature that
   section 3.10 saved just before enabling. Pass: `runs unchanged`, which means
   no new run and no run re-run or changed.

```sh
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/actions/runs?per_page=100' > "$WORK/runs-post.json"
jq -c '[.[].workflow_runs[] | [.id, .run_attempt, .status, .conclusion, .updated_at]] | sort' "$WORK/runs-post.json" | diff "$WORK/runs-fresh.sig" - && echo "runs unchanged"
```

3. The branch, tag and open pull request lists again (sections 3.8.1 and 3.8.4).
   Pass: identical to the sweep.
4. The settings once more (`permissions`, `permissions/workflow`, the secrets
   counts). Pass: unchanged.

**Verified Activation is this readback passing**, captured locally. The push
freeze lifts at that moment; under option B the ruleset freeze of section 2.7a is
released as step 8 of that section says, after this readback and not before. Write the activation evidence (section 5) from the
captured values and land it through the `FORK.md` Git workflow. Later pushes
follow `FORK.md`'s reviewed topics, and a topic that changes workflow files
follows section 9.2.

Stop: any deviation. Turn Actions off at once (step 2.7, `enabled=false`), list
and dispose every run that started (section 3.5), and D1 returns to the Operator.
Repeat from step 2.2 only after that decision.

### 2.9 Option A

`main` stays the default branch. `ci.yml` stays enabled: it is read-only
(`contents: read`, no secret) and runs on pull requests and on pushes to `main`.
The four Upstream paths are disabled repository-wide in step 2.2 and read back
as `disabled_manually` while Actions are off. (Observed 2026-11-07: while Actions
were off those reads returned 404, so under A this readback could only be met by
the attempt of section 2.7a. Option A was not chosen.) Before enabling, the
approved FR-14 path to the
Drift Guard must be recorded (section 1.1): its owner, the trigger and
default-branch arrangement, its timing relative to #1 and #2, and the viability
proof. `schedule`, `workflow_dispatch` and `repository_dispatch` all need the
workflow on the default branch, so under A the path is A2 (Operator-approved
external infrastructure) or A3 (an Operator scope decision on S6); this runbook
selects neither.

Pass: the note exists under `fork/evidence/` with the four parts, and
`default_branch` reads `main`. Stop: no approval, or an incomplete one. The
runbook does not judge the viability proof; it only refuses to continue without
it.

### 2.10 Option B

The default branch is set to `ezop` by explicit Operator decision, with a
`FORK.md` amendment. **Chosen and done (observed 2026-11-07):** the default
branch was changed with one `PATCH` while `main-mirror` stayed active with no
bypass, and the provider accepted it. `main` and `ezop` kept their exact SHAs and
`main-mirror` read back unchanged. The first attempt returned HTTP 400 and changed
nothing; the one that succeeded passes a JSON body on standard input (step 3). The
documentation says a branch protected against force pushes cannot have its default
changed unless the actor can bypass the ruleset; that did not apply here, so the
bracketed adjustment of `main-mirror` below was not used. It stays as the fallback
if a future change is refused. All of it happens before the preflight.

**First, the safe `ezop` tree, read back before the default branch changes.**
`ezop` drops the four inherited Upstream workflows (`release.yml`,
`openapi-sync.yml`, `stats.yml` and `web.yml` do not exist on it), so no Upstream
`schedule`, `workflow_run` or `workflow_dispatch` workflow can run once `ezop` is
the default branch, and Upstream's `openapi-sync.yml` does not become the Drift
Guard by default (D2 stays open). The only workflows allowed on `ezop` are
`ci.yml`, `fork-release.yml` and a Drift Guard that D2 has approved (none exists
yet). `web.yml` has a `push`-to-`main` trigger that still reads the file on
`main`, so it is held by the freeze and the allow-list of section 2.7a, and
disabled by id if it ever registers (a 404 is not a disable). Read the tree back:

```sh
cd "$CLONE"
EZ=$(git ls-remote origin refs/heads/ezop | cut -f 1)
git ls-tree -r --name-only "${EZ:?cannot read the ezop tip}" -- .github/workflows
for f in ci.yml fork-release.yml; do
  body=$(git show "${EZ:?}:.github/workflows/$f") || { echo "STOP: cannot read $f"; break; }
  printf '%s\n' "$body" | grep -nE '^[[:space:]]*(schedule|workflow_run|workflow_dispatch|repository_dispatch):' || echo "$f: none of those triggers"
done
"$FR" guard --profile release "$EZ"
```

Pass: the listing shows exactly `.github/workflows/ci.yml` and
`.github/workflows/fork-release.yml` (plus an approved Drift Guard path, if one
exists), the loop prints "none of those triggers" for both files, and `guard
--profile release` prints `checked <EZ>` and `release: pass`. Stop: anything
else, including a `STOP: cannot read` line. The default branch is not changed,
and Actions stay off.

Save the ruleset next, so it can be restored field by field:

```sh
MM=$(jq -r '.[][] | select(.name == "main-mirror") | .id' "$WORK/rulesets.json")
[ -n "$MM" ] && echo "main-mirror id found" || echo "STOP: no main-mirror ruleset in the list read in step 2.4"
gh api --hostname github.com --method GET -H "$GHV" "repos/Ezopek/proton-cli/rulesets/$MM" > "$WORK/main-mirror-before.json"
jq -S '{name, target, enforcement, conditions, rules, bypass_actors}' "$WORK/main-mirror-before.json" > "$WORK/main-mirror-before.norm.json"
```

1. Record the SHA of `main`:

```sh
git ls-remote origin refs/heads/main | cut -f 1 > "$WORK/main-sha-before.txt"; cat "$WORK/main-sha-before.txt"
```

2. **Manual Operator write, only if step 3 is refused.** Adjust `main-mirror` (the smallest change that
   lets the Operator change the default branch; write down which fields you
   changed). **Unverified until first use**: use the repository settings (no
   command) or `gh api --method PUT` on `repos/Ezopek/proton-cli/rulesets/$MM`
   with the body GitHub's documentation for updating a ruleset names.
3. **Manual Operator write.** Change the default branch (the form that succeeded
   on 2026-11-07 passes the body as JSON on standard input):

```sh
printf '{"default_branch":"ezop"}' | gh api --hostname github.com --method PATCH -H "$GHV" 'repos/Ezopek/proton-cli' --input -
```

4. **Manual Operator write, only if step 2 was used.** Restore `main-mirror`,
   field by field, to the `FORK.md` table, then read it back and compare with the
   saved copy. If step 2 was not used, only read it back and compare. Do this
   also when step 2 or step 3 fails or applies only in part: an adjusted
   `main-mirror` is never left as it is. **Unverified until first use**, the
   restore can be a write of the saved fields (or the repository settings):

```sh
jq '{name, target, enforcement, conditions, rules, bypass_actors}' "$WORK/main-mirror-before.json" > "$WORK/main-mirror-restore.json"
gh api --hostname github.com --method PUT -H "$GHV" "repos/Ezopek/proton-cli/rulesets/$MM" --input "$WORK/main-mirror-restore.json"
```

   Then the readback and comparison:

```sh
gh api --hostname github.com --method GET -H "$GHV" "repos/Ezopek/proton-cli/rulesets/$MM" | jq -S '{name, target, enforcement, conditions, rules, bypass_actors}' > "$WORK/main-mirror-after.norm.json"
diff "$WORK/main-mirror-before.norm.json" "$WORK/main-mirror-after.norm.json" && echo "main-mirror restored"
```

5. Require the `main` SHA unchanged:

```sh
git ls-remote origin refs/heads/main | cut -f 1 | diff - "$WORK/main-sha-before.txt" && echo "main unchanged"
```

6. Repeat the readbacks of step 2.3 (secrets) and step 2.4 (settings, including
   the rulesets), and read the inherited paths of step 2.2. While Actions are off
   they return 404: record that as "not registered", which is neither a pass nor
   a disabled state. The pass for those paths is decided at the bounded enable of
   section 2.7a. A state of `deleted`, or any other unexpected state, is recorded
   in words and stops the activation.

Pass: `default_branch` reads `ezop`, `main` is unchanged, `main-mirror` reads
back as saved (and the `diff` printed nothing, if the bracket was used), and the
repeated readbacks pass. If the default branch did not change,
restore as step 4 says and stop. A failed restore keeps Actions off
and is handled as an incident: `main` is checked against the recorded SHA and
D1 returns to the Operator. Stop: any step above that does not pass.

## 3. Run-Inventory Preflight

The preflight is fail closed and uses read-only commands only; there is no helper
code for it. It runs inside the push freeze (section 3.9), with Actions off. A
truncated, stale or unreconciled inventory never passes. Whether runs can be
listed at all while Actions are off is part of Q1; an unreadable listing is a
stop.

Start by recording the start time (UTC). From this moment pushes are frozen.

### 3.1 Refresh the clone

```sh
cd "$CLONE"
git fetch --no-tags origin '+refs/heads/*:refs/remotes/origin/*'
```

### 3.2 The run listing: unfiltered, paginated to the end, count reconciled

No filter parameter is passed, only `per_page=100`, so the 1,000-result search cap
that applies to filtered listings cannot truncate it.

```sh
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/actions/runs?per_page=100' > "$WORK/runs.json"
jq '{total_counts: ([.[].total_count] | unique), enumerated: ([.[].workflow_runs[]] | length), pages: length}' "$WORK/runs.json"
```

Pass: `total_counts` holds exactly one value and it equals `enumerated`. Record
that count. Stop: a mismatch, more than one `total_count`, a failed read.

`runs.json` is this preflight's baseline. A later read of the same listing
(sections 2.8 and 6.11) is written to a new file and never over it.

### 3.3 The closed sets

`status` is one of `queued`, `in_progress`, `completed`, `requested`, `waiting`,
`pending`. `conclusion` is one of `success`, `failure`, `neutral`, `cancelled`,
`skipped`, `timed_out`, `action_required`, `stale`, or null while the run is not
completed. These sets are derived from the documented `status` query values,
because the REST schema documents no `conclusion` list. Each command prints a
count of runs outside the set:

```sh
jq '[.[].workflow_runs[] | select((.status | IN("queued","in_progress","completed","requested","waiting","pending")) | not)] | length' "$WORK/runs.json"
jq '[.[].workflow_runs[] | select(.conclusion != null and ((.conclusion | IN("success","failure","neutral","cancelled","skipped","timed_out","action_required","stale")) | not))] | length' "$WORK/runs.json"
jq '[.[].workflow_runs[] | select(.status == "completed" and .conclusion == null)] | length' "$WORK/runs.json"
```

Pass: all three print 0. Stop: any other number. Read the run, record the value
and its disposition, and do not go on until the set is changed through a reviewed
change to this runbook.

### 3.4 The runs that need a disposition

A run needs one if its status is not `completed`, or it concluded
`action_required`, or it is completed and was created or last updated in the last
30 days (the documentation does not say when the re-run clock starts, so either
timestamp in the window counts):

```sh
jq -r '(now - 30*86400) as $cut | .[].workflow_runs[] | select(.status != "completed" or .conclusion == "action_required" or (.created_at | fromdateiso8601) >= $cut or (.updated_at | fromdateiso8601) >= $cut) | [.id, .workflow_id, .path, .status, (.conclusion // "null"), .event, .run_attempt, .created_at, .updated_at] | @tsv' "$WORK/runs.json" | tee "$WORK/runs-needing-disposition.tsv"
wc -l < "$WORK/runs-needing-disposition.tsv"
```

The `path` field of a run is **Unverified until first use**; `workflow_id` is the
join key to the workflow list in section 3.7. If the count is 0, record 0 and go
on to section 3.6.

### 3.5 The four dispositions and the named owner

Every run in the list from section 3.4 gets a named owner (the Operator, or a
named maintainer) and exactly one disposition:

1. **Leave it disabled and impossible to re-run.** The workflow is one of the
   four disabled Upstream paths, read back as `disabled_manually` in step 2.2.
   GitHub's documentation says that runs of a disabled workflow cannot be
   re-run while it is disabled; whether that holds for completed runs after the
   switch is turned on is part of Q1.
2. **Let a read-only run finish.** Only for a workflow whose permissions are
   read-only (`ci.yml`). Re-read the run until it is completed.
3. **Accept it as re-runnable.** Only for a workflow that stays enabled and whose
   re-run cannot publish outside the fork release boundary: `ci.yml` (read-only),
   or `fork-release.yml` (its own refusal governs a re-run, AD-7: a re-run of a
   tag with any record refuses, a re-run of a release that exists refuses, and a
   re-run of a tag with neither a record nor a release object is the sanctioned
   recovery). The acceptance is recorded **once per workflow**, with the reason,
   not once per run (the acceptance fields are in the evidence template).
4. **Cancel it with a stated reason.**

**Manual Operator write.** Unverified until first use (cancel acts only on a run
that is not over):

```sh
gh api --hostname github.com --method POST -H "$GHV" 'repos/Ezopek/proton-cli/actions/runs/RUN_ID/cancel'
```

Pass: every listed run has an owner and a disposition, and every workflow with
an accepted re-run has its acceptance recorded. Stop: a run with no disposition;
a run of any workflow not in section 3.7's accounted list; an attempt to accept a
re-run for a workflow that fails the rule above.

### 3.6 The tag states of `fork-release.yml`

List every Release Tag on origin and the token `verify --status` prints for it,
together with the end of its re-run window. Do this for every fork tag, not only
those with a run; a run whose `head_sha` matches no listed tag gets a disposition
as in section 3.5.

```sh
git ls-remote --refs origin 'refs/tags/v*-ezop.*' | tee "$WORK/fork-tags.txt"
for t in $(cut -f 2 "$WORK/fork-tags.txt" | sed 's|^refs/tags/||'); do
  echo "== $t"
  "$FR" verify --status "$t"; echo "exit $?"
done
```

```sh
FRID=$(gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/actions/workflows/fork-release.yml' | jq -r '.id')
jq -r --argjson id "$FRID" '.[].workflow_runs[] | select(.workflow_id == $id) | [.id, .head_sha, .run_attempt, .status, (.conclusion // "null"), .created_at, (((.created_at | fromdateiso8601) + 30*86400) | todate)] | @tsv' "$WORK/runs.json"
```

Pass: for each tag, the token (`none`, `verified`, `abandoned` or `invalid`) is
listed beside the end of its re-run window (30 days after the run was created, a
conservative reading). A tag whose token is `unknown` (exit 4) **stops** the
preflight and is retried after the cause is understood: a persistent `unknown` is
never a retry loop, it is investigated (section 7). Before the first release
there is no tag and no run: record "none".

`fork-release.yml` may not be registered yet: under option A it exists only on
`ezop`, and under option B no workflow is registered while Actions are off
(observed 2026-11-07). A not-found answer for the `FRID` read is recorded as such
and is not a stop **only if** the complete run list holds no run of that workflow;
skip the join then, and coverage (section 3.7) rests on the branches. Check:

```sh
jq '[.[].workflow_runs[] | select(.path == ".github/workflows/fork-release.yml")] | length' "$WORK/runs.json"
```

Pass: `0` (the `path` field is **Unverified until first use**). Any other number
makes a not-found answer a stop. An authentication or authorization error, an
ambiguous answer or any other failed read is a stop in every case.

### 3.7 Workflow coverage

The set of workflow files is the union of the `.github/workflows/` files on every
origin branch and the API's workflow list.

```sh
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/actions/workflows?per_page=100' > "$WORK/workflows.json"
jq '{total_count: .[0].total_count, enumerated: ([.[].workflows[]] | length)}' "$WORK/workflows.json"
jq -r '.[].workflows[] | [.id, .path, .state] | @tsv' "$WORK/workflows.json"
git ls-remote --refs origin 'refs/heads/*' > "$WORK/heads.txt"
: > "$WORK/branch-workflow-paths.txt"
while read -r sha ref; do
  git ls-tree -r --name-only "$sha" -- .github/workflows >> "$WORK/branch-workflow-paths.txt" || echo "STOP: cannot read a branch"
done < "$WORK/heads.txt"
{ cat "$WORK/branch-workflow-paths.txt"; jq -r '.[].workflows[].path' "$WORK/workflows.json"; } | sort -u
```

Pass: `total_count` equals `enumerated`, and every path in the union is accounted
for by this list: `.github/workflows/ci.yml`, `.github/workflows/fork-release.yml`,
and the four Upstream paths (`release.yml`, `openapi-sync.yml`, `stats.yml`,
`web.yml` under `.github/workflows/`). A GitHub-managed dynamic workflow in the
API list is accounted for only as section 4 says. Stop: any other path; a
branch that cannot be read.

### 3.8 The ref and pull request sweep

Path coverage does not read content, so every ref is accounted for once, at
activation (and again at every reactivation, section 8.1 and 9.2).

**3.8.1 Branches and tags, from one complete call.** `--refs` leaves out peeled
tag lines and GitHub's `refs/pull/*`, and the call has no pagination:

```sh
git ls-remote --refs origin 'refs/heads/*' 'refs/tags/*' > "$WORK/refs.txt"
cut -f 2 "$WORK/refs.txt" | grep -c '^refs/heads/'
cut -f 2 "$WORK/refs.txt" | grep -c '^refs/tags/'
```

The branches and tags API listings have no `total_count`, so they are only a
cross-check, read to the last page:

```sh
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/branches?per_page=100' | jq '{count: ([.[][]] | length), page_sizes: [.[] | length]}'
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/tags?per_page=100' | jq '{count: ([.[][]] | length), page_sizes: [.[] | length]}'
```

Pass: each API count equals the matching `git ls-remote` count, and the last page
size is under 100. If the last page holds exactly 100, also read the next page by
hand (`&page=N`) and see it empty. Record both counts. Stop: a failed call, a
missing page or a count mismatch.

**3.8.2 The authoritative profile runs.** From a landed helper (section 1.4), on
the exact tips listed in `refs.txt`:

```sh
EZ=$(git ls-remote origin refs/heads/ezop | cut -f 1)
MN=$(git ls-remote origin refs/heads/main | cut -f 1)
awk -v s="$EZ" '$1 == s && $2 == "refs/heads/ezop"' "$WORK/refs.txt" | wc -l
awk -v s="$MN" '$1 == s && $2 == "refs/heads/main"' "$WORK/refs.txt" | wc -l
"$FR" guard --profile release "$EZ"
"$FR" guard --profile mirror "$MN"
devbox run go version -m "$FR" | grep 'vcs\.'     # the commit that ran the checks
```

Pass: both counts above are 1 (the tips are the listed ones); `guard --profile
release` prints `checked <EZ>` and `release: pass`; `guard --profile mirror`
prints `checked <MN>` and `mirror: pass`; each printed SHA equals the listed tip
and nothing is held. Record the helper's `vcs.revision`. Stop: anything held, a
SHA that differs, a refusal. Nothing before landing stands in for this.

**3.8.3 Every other branch is accounted for by its workflow files.** It has no
`.github/workflows/`, or its `.github/workflows/` tree equals that of the `ezop`
tip, the `main` tip or the Upstream Base tag, and then it changes nothing that
could run. Set `BASE` to the Upstream Base tag name (for example `v5.1.0`):

```sh
wft() { git ls-tree "$1" -- .github/workflows | cut -f 1 | awk '{print $3}'; }
T_EZ=$(wft "$EZ"); T_MN=$(wft "$MN"); T_BASE=$(wft "$BASE^{commit}")
n_none=0; n_same=0; n_changed=0
while read -r sha ref; do
  t=$(wft "$sha") || { echo "STOP: cannot read a branch"; break; }
  if [ -z "$t" ]; then n_none=$((n_none+1))
  elif [ "$t" = "$T_EZ" ] || [ "$t" = "$T_MN" ] || [ "$t" = "$T_BASE" ]; then n_same=$((n_same+1))
  else n_changed=$((n_changed+1)); echo "CHANGED: $ref"
  fi
done < "$WORK/heads.txt"
echo "none=$n_none same=$n_same changed=$n_changed"
```

Pass: `changed=0`. Any other branch carries a changed workflow: the Operator
either deletes the branch or records it as holding a changed workflow, which is
never opened as a pull request while Actions are on (section 9.2). A path-level
disable is not known to cover another ref's copy of a file (Q1), so a changed
copy of one of the four disabled Upstream paths is recorded the same way. Whether
a contribution branch is kept stays the Operator's call under `FORK.md`.
Deleting a branch is the Operator's explicit decision, never a tool's:

**Manual Operator write** (only by explicit decision):

```sh
git push origin --delete BRANCH_NAME
```

No policy is relaxed to clear a branch. GitHub's automatic deletion of a merged
topic branch, which `FORK.md` records, is the one exception to "an undeclared
deletion is a violation". Stop: a changed branch that is neither deleted nor
recorded.

**3.8.4 Open pull requests, with their head repositories.**

```sh
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/pulls?state=open&per_page=100' > "$WORK/pulls.json"
jq '{open: ([.[][]] | length), same_repo: ([.[][] | select(.head.repo.full_name == "Ezopek/proton-cli")] | length), outside_or_null_head: ([.[][] | select(.head.repo.full_name != "Ezopek/proton-cli")] | length), malformed: ([.[][] | select((.number | type) != "number" or (.head.sha | type) != "string")] | length), page_sizes: [.[] | length]}' "$WORK/pulls.json"
jq -r '.[][] | [.number, .head.sha, (.head.repo.full_name // "null")] | @tsv' "$WORK/pulls.json"
```

A head repository that is null, missing or malformed (a deleted fork) counts as
an outside head. Pass: `malformed` is 0, the last page size is under 100, and
the counts are recorded. An open pull request from a branch recorded as holding
a changed workflow is closed before Actions are enabled (**Manual Operator
write**, `gh api --method PATCH` on `repos/Ezopek/proton-cli/pulls/NUMBER` with
`-f state=closed`; unverified until first use). A pull request from another
repository runs only after approval (step 2.4), and none is approved during the
freeze; close each or leave it pending by an explicit decision. Stop: a malformed
entry.

**3.8.5 Tags are counted and compared with `upstream`.**

```sh
for t in $(cut -f 2 "$WORK/refs.txt" | grep -E '^refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$' | sed 's|^refs/tags/||'); do
  o=$(peel origin "$t") && u=$(peel upstream "$t") || { echo "STOP: read failed for $t"; break; }
  if [ -n "$u" ] && [ "$o" = "$u" ]; then echo "$t same"; else echo "$t DIFFERS-OR-NOT-ON-UPSTREAM"; fi
done
```

Pass: every Upstream-shaped tag on origin peels to the same commit as on
`upstream`. A `v*-ezop.*` tag that exists before activation is handled by
sections 6 and 7, never by re-tagging. Stop: any tag that differs.

The sweep never assumes that a workflow is disabled. Disabled states come from
step 2.2 (under option B, from section 2.7a), and an unreadable state fails the
preflight. Under option B the expected 404 for the four inherited paths while
Actions are off is a readable answer: it is recorded as "not registered", not as
disabled. An authentication or authorization error, an ambiguous answer or any
other failed read still fails the preflight.

### 3.9 The push freeze and what voids it

Pushes, tag pushes included, are frozen from the start of the preflight until
Verified Activation. Any of these in the window voids the preflight: a push to
any ref (by anyone, including GitHub-side ref creation, section 4), a new run, a
re-enable of Actions, a workflow change, a settings change other than the chosen
option's own bracketed steps. The single enable and the disables listed in
section 2.7a are the planned transitions inside the window: they are part of the
procedure and do not void the preflight; every other re-enable, settings change,
push, run or workflow change does. A tag pushed in the window stays immutable and
is verified or abandoned like any other (section 7). When the preflight is voided,
Actions stay off (or are turned off at once) and the preflight starts again at
section 3.1 with new files.

### 3.10 The fresh readback immediately before enabling

Do this just before step 2.7, and read every list again. Any difference from the
sweep voids the preflight.

```sh
git ls-remote --refs origin 'refs/heads/*' 'refs/tags/*' > "$WORK/refs-fresh.txt"
diff "$WORK/refs.txt" "$WORK/refs-fresh.txt" && echo "refs unchanged"
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/actions/runs?per_page=100' > "$WORK/runs-fresh.json"
for f in runs runs-fresh; do jq -c '[.[].workflow_runs[] | [.id, .run_attempt, .status, .conclusion, .updated_at]] | sort' "$WORK/$f.json" > "$WORK/$f.sig"; done
diff "$WORK/runs.sig" "$WORK/runs-fresh.sig" && echo "runs unchanged"
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/pulls?state=open&per_page=100' | jq -c '[.[][] | [.number, .head.sha, .head.repo.full_name]] | sort' > "$WORK/pulls-fresh.sig"
jq -c '[.[][] | [.number, .head.sha, .head.repo.full_name]] | sort' "$WORK/pulls.json" | diff - "$WORK/pulls-fresh.sig" && echo "pull requests unchanged"
for f in release.yml openapi-sync.yml stats.yml web.yml; do printf '%s ' "$f"; gh api --hostname github.com --method GET -H "$GHV" "repos/Ezopek/proton-cli/actions/workflows/$f" | jq -r '.state'; done
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/actions/permissions' | jq '{enabled, allowed_actions}'
```

The chosen option's actual preconditions must also be in force at this moment, and
a readback that shows otherwise fails the preflight (there is no second, looser
variant). Under option A, the four states above read `disabled_manually` and
`default_branch` reads `main`. Under option B, the four reads above return 404
while Actions are off (observed 2026-11-07): record that as "not registered",
which is neither a pass nor a disabled state, and decide the inherited paths at
section 2.7a. Also under option B, the `ezop` tree is the safe one
(the listing, the trigger greps and `guard --profile release` of section 2.10,
run again against the freshly listed tip) and `default_branch` already reads
`ezop`:

```sh
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli' | jq -r '.default_branch'
```

Pass: the three `unchanged` lines print, under option A all four states read
`disabled_manually` (under option B the four reads are recorded as above),
`enabled` is `false`, and the option's preconditions above hold. Stop: any
difference or failed read. The same lists are read again at step 2.8.

## 4. Readback breadth

The spine defers this breadth to the runbook. Each item is recorded in the
activation evidence as **unused** (the readback shows it absent) or **swept**
(it exists and the ref sweep of section 3.8 covers the refs it creates). Every
endpoint below is **Unverified until first use**. `gh api` exits non-zero for a
404; read the message, because for some items a 404 is the expected answer.

**Deploy keys.** Pass: `writable` is 0. Record the total as a count.

```sh
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/keys?per_page=100' | jq '{total: ([.[][]] | length), writable: ([.[][] | select(.read_only == false)] | length)}'
```

**Installed apps with write access.** No command; read the account's installed
GitHub Apps in the settings and, for each, whether it can reach this repository
with write access. Pass: none with write access. Record the count. As of
2026-11-07 a user token cannot read it: `GET repos/Ezopek/proton-cli/installation`
answered 401 (it needs a GitHub App token) and `GET /user/installations` answered
403. Record the item as **unknown**, never as none, with the controls and the open
points of section 2.7a. The settings page is the way to close it; it is not a
start condition.

**Pages.** Pass: HTTP 404 (not configured) is unused. Any other answer is
recorded and is a stop until the Operator decides.

```sh
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/pages'; echo "exit $?"
```

**GitHub-managed dynamic workflows.** They appear in the API workflow list with
a path outside `.github/workflows/`. Pass: none listed. If any is listed, record
its path, state and the feature behind it (Pages, Dependabot, code scanning);
the Operator disables the feature or records why it stays, and section 3.7 does
not pass until it is accounted for.

```sh
jq -r '.[].workflows[] | select(.path | startswith(".github/workflows/") | not) | [.id, .path, .state] | @tsv' "$WORK/workflows.json"
```

**Code scanning default setup.** Pass: `not-configured` (the value is
unverified; record what is read).

```sh
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/code-scanning/default-setup' | jq -r '.state'
```

**Dependabot secrets store.** Pass: the counts reconcile and none of the seven
names (as in step 2.3) is present. Record the count.

```sh
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/dependabot/secrets?per_page=100' | jq '{total_count: .[0].total_count, enumerated: ([.[].secrets[]] | length), names: [.[].secrets[].name]}'
```

**Actions variables (`vars`), repository and each environment.** Pass: the counts
reconcile, and the Operator reviews each name and records that no workflow reads
it to steer a publisher or channel. Record the counts. Organization-level
secrets and variables do not exist for a personal account; read them once and
record the answer.

```sh
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/actions/variables?per_page=100' | jq '{total_count: .[0].total_count, enumerated: ([.[].variables[]] | length), names: [.[].variables[].name]}'
for e in $(jq -r '.[].environments[].name | @uri' "$WORK/envs.json"); do
  gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp "repos/Ezopek/proton-cli/environments/$e/variables?per_page=100" | jq '{total_count: .[0].total_count, enumerated: ([.[].variables[]] | length)}' || { echo "STOP: read failed"; break; }
done
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/actions/organization-secrets'; echo "exit $?"
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/actions/organization-variables'; echo "exit $?"
```

**Merge queue.** Pass: no `merge_queue` rule type among the rulesets saved in
step 2.4.

```sh
jq -r '.rules[].type' "$WORK"/ruleset-*.json | sort | uniq -c
```

**Dependabot.** Pass: alerts and security updates disabled (unused), or enabled
with the sweep run after any Dependabot activity (swept); record which.
`.github/dependabot.yml` is held by both profiles, and `dependabot/` branches
show in the sweep.

```sh
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/vulnerability-alerts'; echo "exit $?"
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/automated-security-fixes'; echo "exit $?"
```

**Web-UI branch restore and update.** No command: the web UI can recreate a
deleted branch ("Restore branch" on a closed pull request) and move a pull
request branch ("Update branch"). Pass: not used during the freeze (unused), or
used earlier and covered by the sweep (swept). Record which.

## 5. Evidence template

Copy the block below to `fork/evidence/activation-<YYYYMMDDThhmmssZ>.md`, with
the stamp in UTC to the second (`date -u +%Y%m%dT%H%M%SZ`), so that two
activations on one day never collide. Fill it from values captured during the
freeze, land it through the `FORK.md` Git workflow, and review it in that pull
request. No command reads it. Sanitized fields only: counts, states, paths,
versions, tag names, commit SHAs, run ids only where a disposition needs them,
and times. Never raw API output, account metadata, emails, local paths or secret
values.

```markdown
# Activation evidence <YYYYMMDDThhmmssZ>

## Decisions (the Operator's, taken before the core started and copied from the note of section 1.1)
- D1 option: <A | B>
- FR-14 approval note (option A only): <path of the note under fork/evidence/ | not applicable>
- FORK.md Following Upstream amendment approved: <yes>; FORK.md blob at the ezop tip: <40 hex>
- FR-12 ruleset items in force and recorded in FORK.md: <yes>
- Q4 risk and abandonment policy for v5.1.0-ezop.1: <path of the note | recorded later, before the first Release Tag>
- Q1 answer (taken at step 2.2, not before: disable, read back and list runs while Actions were off): <clear yes | one sentence>

## Helper
- ezop tip at the time: <40 hex>; main tip at the time: <40 hex>
- helper commit that ran each profile check (vcs.revision): <40 hex>
- Go version used for the build: <x.y.z>

## Core readbacks, Actions off
- Step 1: ezop contains the #1 tree (fork/activation-runbook.md, scripts/fork-release, fork-release.yml present; the four Upstream workflows absent): <pass | fail>
- Step 2: release.yml <state>; openapi-sync.yml <state>; stats.yml <state>; web.yml <state>
- Step 3: repository secrets total <n>, of the seven <0>; environments <n>, secrets in them <n>, of the seven <0>
- Step 4: default_workflow_permissions <value>; can_approve_pull_request_reviews <value>; fork pull request approval policy <exact value string>; collaborators <n>, others with write <n>, pending invitations <n>
- Step 4 rulesets: main-mirror <enforcement>; fork-release-tags <enforcement>; ezop-deletion <enforcement>; ezop-rebase <enforcement>; each matches FORK.md: <yes>
- Step 4 FR-12 items: v* tag ruleset <enforcement>; main update restriction <enforcement>; each matches FORK.md: <yes>
- Step 5: default branch <main | ezop>; option B only: main SHA before <40 hex>, after <40 hex>, main-mirror restored field by field <yes>, repeated readbacks <pass>

## Preflight (start <time UTC>)
- Run listing: total_count <n>, enumerated <n>, pages <n>
- Closed sets: statuses outside <0>; conclusions outside <0>; completed with null conclusion <0>
- Runs needing a disposition: <n>
  | run id | workflow file | status | conclusion | disposition (1-4) | owner |
  | --- | --- | --- | --- | --- | --- |
- Acceptance, once per workflow:
  | workflow file | state | accepted as re-runnable | reason |
  | --- | --- | --- | --- |
- fork-release.yml tag states (none yet | one row per tag):
  | tag | verify --status token | release state | re-run window ends (UTC) |
  | --- | --- | --- | --- |
- Workflow coverage: API total <n>, enumerated <n>; paths on branches and in the API, all accounted for: <yes>; dynamic workflows <none | path and state>
- Ref sweep: branches <n> (ls-remote) and <n> (API); tags <n> and <n>; ezop tip <40 hex>, guard --profile release <pass>; main tip <40 hex>, guard --profile mirror <pass>; other branches: none-workflows <n>, same-as-ezop/main/base <n>, changed <n> (each deleted or recorded: <how>)
- Open pull requests <n>: same repository <n>, outside or null head <n>; closed <n>; left pending <n>
- Upstream-shaped tags compared with upstream: <n>, all same <yes>
- Push freeze: start <time UTC>, end <time UTC>; voided <no>
- Fresh readback before enabling (time UTC): refs unchanged <yes>; runs unchanged <yes>; pull requests unchanged <yes>; inherited paths <state, or not registered (404), each>

## Readback breadth
- deploy keys <unused | n, none writable>; installed apps with write access <n>; Pages <unused>; dynamic workflows <none>; code scanning default setup <state>; Dependabot secrets <n>; Actions variables <n> repository, <n> environments; merge queue <unused>; Dependabot <unused | swept>; web-UI branch restore and update <unused | swept>

## Enable and Verified Activation
- Enabled at <time UTC> (not 00:00 or 01:00 UTC): enabled <true>; allowed_actions <value, unchanged>
- Post-enable readback inside the freeze (time UTC): inherited paths <state, or not registered, each; web.yml not run>; freeze probes rejected <yes>; allow-list <seven patterns>; run count <n> equals the preflight count; new runs <0>; refs, tags and pull requests unchanged <yes>; settings unchanged <yes>
- Verified Activation: <yes, at time UTC>

## First-use observations
- Commands marked unverified that were used for the first time, and the answer in words: <list | none>
```

Where the other evidence goes, all under `fork/evidence/` with the same UTC
stamp convention, as sanitized fields only, landed through the `FORK.md` Git
workflow:

- the **post-first-release readback** (section 6.11): `post-release-<stamp>.md`;
- the **per-fast-forward S4 observation** (section 8.1), with the helper commit
  that ran `guard --profile mirror`: `mirror-<stamp>.md`;
- an **FR-14 approval note**, and the **Q4 and abandonment policy note**
  (section 1.1), under names the Operator chooses, referenced from the
  activation evidence.

## 6. Releasing

A Release Tag is `vX.Y.Z-ezop.N`, where `X.Y.Z` is the Upstream Base. Every tag
is immutable, even before publication: it is never deleted, moved, reused or
pushed again (`FORK.md`). Each push carries exactly one tag, named by an explicit
refspec. The first release, `v5.1.0-ezop.1`, contains no functional patch.

### 6.1 Preconditions (all of them, or no tag)

- Verified Activation is done and its evidence is landed on `ezop` (step 2.8).
  A tag pushed while Actions are off, or before activation is verified, starts
  no run and is spent: it needs an explicit Operator recovery decision, never a
  re-tag.
- For the first tag, the Q4 risk acceptance and the abandonment policy for
  `v5.1.0-ezop.1` are recorded in a sanitized note under `fork/evidence/`
  (section 1.1). Q4: whether GitHub refuses to create or publish the release
  for a tag whose commit adds files under `.github/workflows/` (the tagged
  commit adds `fork-release.yml` by design). It can only be observed by the tag
  itself, and `guard --pre-push` cannot see it.
- The Operator has explicitly authorized this version `N`.
- Actions are on and the workflow is not disabled:

```sh
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/actions/permissions' | jq '{enabled}'
gh api --hostname github.com --method GET -H "$GHV" 'repos/Ezopek/proton-cli/actions/workflows/fork-release.yml' | jq -r '.state'
```

Pass: `enabled` is `true`, and the workflow state is `active`. Under option A the
file exists only on `ezop`, and whether the API lists it before its first run is
not established: a not-found answer is recorded and is not a stop, but any
`disabled_*` state is. Stop: anything else.

### 6.2 A clean checkout of the landed commit, and the helper built at it

`C` is a commit that has landed on `origin/ezop`, normally the tip you have just
fetched. Gates run on a PR head do not count: a Rebase and merge rewrites it.

```sh
TAG=v5.1.0-ezop.1                     # the version the Operator authorized
GCLONE="$WORK/gates-$(date -u +%Y%m%dT%H%M%SZ)"
git clone --branch ezop https://github.com/Ezopek/proton-cli.git "$GCLONE"
cd "$GCLONE"
git remote add upstream https://github.com/roman-16/proton-cli.git
git remote set-url --push upstream DISABLED
# to tag an earlier landed commit instead of the tip:  git checkout --detach THE_SHA
C=$(git rev-parse HEAD)
git status --porcelain                # must print nothing
FRC="$WORK/bin/fork-release-at-$C"
devbox run go build -o "$FRC" ./scripts/fork-release
```

Build the helper before running the gates, so that nothing the gates leave
behind can make the stamp read as modified. `guard --pre-push` requires the
helper's embedded commit to equal `C`. Pass: the status is empty and the build
succeeds. Stop: otherwise.

### 6.3 The Pre-Tag Gates, in order, stopping at the first failure

Inside `$GCLONE`, with the `devbox.lock` toolchain. Run `just test-fast`, never
`just test`: the full suite signs real accounts in and is not a gate here.

```sh
devbox run just test-fast
devbox run just lint
git status --porcelain                # must print nothing: lint left no diff
devbox run just build
devbox run just snapshot
```

Then read the versions the gates used and the snapshot's version line:

```sh
devbox run go version
devbox run goreleaser --version
ls dist
```

Run the snapshot's raw binary for the host (`dist/proton-cli_<os>_<arch>`; the
exact file name is **Unverified until first use**) with `version`, and note the
one line it prints. Write the gate-capture file, by hand, from what you read. It
has the six keys that `fork/verification/README.md` fixes, and `verify` embeds
them in the record under `attested`:

```sh
mkdir -p -m 700 "$KEEP"
GATES="$KEEP/gates-$TAG-$C.txt"       # the name binds the file to the tag and the commit
printf '%s\n' \
  "gate-commit: $C" \
  "gates: test-fast,lint,build,snapshot" \
  "lint-diff: none" \
  "go-version: GO_VERSION_NUMBER_ONLY" \
  "goreleaser-version: GORELEASER_VERSION_NUMBER_ONLY" \
  "snapshot-version: THE_LINE_THE_SNAPSHOT_BINARY_PRINTED" > "$GATES"
cat "$GATES"; wc -l < "$GATES"
```

The file lives in `$KEEP`, not in `$WORK`: `verify` needs it after the tag is
pushed and the run is over, which may be in another session, and the cleanup of
`$WORK` would remove it. It holds only the commit, the gate names, two version
numbers and one version line: no secret and no raw output. Do not delete it
until the terminal record for `TAG` has landed (section 6.10).

Rules of the file: plain text, one `key: value` per line with one space after
the colon, printable ASCII, LF line ends, no blank line, each key once, at most
64 KiB. `gates` is exactly `test-fast,lint,build,snapshot`, `lint-diff` is
exactly `none`, `gate-commit` equals `C`, and the two versions are dotted
numbers without a leading `v`, equal to what the gates used. Pass: every gate
exited zero, `git status --porcelain` printed nothing after lint, and the file
has six lines. Stop at the first failing gate: no tag is pushed. The fix is a
reviewed topic on `ezop` and a new `C` with new gates; evidence for another
commit never backs a record for `C`.

### 6.4 The base-tag peel comparison with `upstream`

The helper's base check reads origin tags, so a wrong-commit Upstream-shaped tag
could satisfy it. Confirm that the base tag on origin peels to the same commit
as on `upstream`:

```sh
BASE=${TAG%-ezop.*}
peel origin "$BASE"
peel upstream "$BASE"
```

Pass: both print the same non-empty commit. Stop: either prints nothing, they
differ, or a read fails. If the tag is missing on origin, push it first by
section 8.2. Tags are never force-pushed; if a tag already exists on origin at a
different commit, the procedure stops and no tag is moved.

### 6.5 `guard --pre-push`

Run it immediately before the push, from `$GCLONE`, with the helper built at `C`:

```sh
cd "$GCLONE"
REFSPEC=$("$FRC" guard --pre-push "$TAG" "$C"); echo "exit $?"
printf '%s\n' "$REFSPEC"
```

It checks the tag pattern, `origin`, that `C` is the fetched `ezop` tip or an
ancestor, that origin has no `TAG` (positive absence), the helper's embedded
commit equals `C` with a clean build, the base check, the `release` profile and
the toolchain on the tree of `C`, and that the record is Absent and the release
None. It reads no Actions state, so it cannot see Q4, the config-shadow residual
(section 11) or any platform refusal.

Pass: exit 0 and standard output is exactly `<C>:refs/tags/<TAG>`. Stop: any
refusal. Nothing is printed on standard output, nothing was pushed, and nothing
is spent: no tag, release or record exists. The fix is a reviewed topic, a new
`C` and new gates. Whether the same `N` is used again (it was never pushed) or
the next explicitly authorized `N` is the Operator's authorization.

### 6.6 Push the tag

**Manual Operator write.** Push exactly the printed refspec, once, from
`$GCLONE`:

```sh
git push origin "${REFSPEC:?guard --pre-push printed no refspec}"
```

Never `--tags`, never `--follow-tags`, never a leading `+`, never a refspec typed
by hand. Then confirm:

```sh
peel origin "$TAG"                    # must print C
```

Whether the rulesets allow creating the tag is Q3; a rejected push creates
nothing and is never "fixed" by re-tagging. Stop and decide.

### 6.7 Wait for the run

The workflow starts on the tag push. Read the run, and the status, until the run
is over. While a run is in progress `verify --status` reads `unknown` (exit 4),
and that is expected:

```sh
gh api --hostname github.com --method GET -H "$GHV" "repos/Ezopek/proton-cli/actions/workflows/fork-release.yml/runs?event=push&head_sha=$C&per_page=100" | jq '.workflow_runs[] | {id, run_attempt, status, conclusion}'
cd "$CLONE" && "$FR" verify --status "$TAG"; echo "exit $?"
```

The run fields for a tag-push run are not described by the documentation (Q6): an
empty listing is never evidence by itself. If no run appears, look at the
repository's Actions page and do not assume anything. Start no other attempt
while one is running. Pass: the run is `completed` with conclusion `success`.
Any other outcome goes to section 7; do not re-run before reading the status.

### 6.8 `verify TAG GATES`

Fetch again and build the helper from the tip you have just fetched (section
1.4). In a new session, set `TAG` and find the capture from the tag, not from
memory (the tag peels to `C`, and the file name carries both):

```sh
C=$(peel origin "$TAG"); GATES="$KEEP/gates-$TAG-$C.txt"
cat "$GATES"; wc -l < "$GATES"
```

Pass: six lines, with `gate-commit` equal to `C`. It must be the original
capture, written before the tag was pushed (FR-9). If it is not there, stop. Look
first under the same `HOME` and `XDG_STATE_HOME` as the session that ran the
gates, and anywhere else the original was saved: a different `HOME` or
`XDG_STATE_HOME` alone does not mean it was lost. Never write it again from
memory, and never run the gates now and offer them in its place: gates run after
the push are not Pre-Tag Gates, `verify` cannot tell when they ran, and accepting
them would change FR-9, which this runbook has no authority to do. If the
original cannot be recovered, the version is abandoned (section 7.2, reason
`operator-decision`) and the next explicitly authorized `N` gets new gates before
its tag. Otherwise:

```sh
cd "$CLONE"
"$FR" verify "$TAG" "$GATES" > "$WORK/$TAG.md"; echo "exit $?"
cat "$WORK/$TAG.md"
```

`verify` requires the record Absent and the release Published-Same, re-resolves
`TAG` from its own fetch, downloads the published assets (never `dist/`), checks
`checksums.txt` in both directions, runs the host binary and checks `proton
version -o json`, reads the run attempt that the release notes name, and makes
its last reads before printing. The capture is the Operator's own statement: `verify` checks its six values and
that `gate-commit` is the tagged commit, not that the gates were run. It writes
nothing; the shell redirect above
creates the file even when `verify` refuses, so an empty file is a refusal and
is never landed.

Pass: exit 0 and the file is a record starting `schema: 1`, `tag: <TAG>`,
`status: verified`, `time: <UTC>`. Stop: any refusal. A failed read is transient:
nothing is printed, run it again. A defect found in `verify` itself (for example
in the first real run-field reads, Q6) is fixed by an ordinary reviewed topic,
then the command is run again from the new commit; it does not abandon the
version. A red attempt after a release object exists is not repaired by a
re-run: section 7.

### 6.9 Land the record

**Manual Operator write.** In your own topic worktree for this change (`FORK.md`:
each session works in its own linked worktree; do not use the helper clones,
which must stay clean), through the `FORK.md` Git workflow:

```sh
git switch -c chore/record-v5-1-0-ezop-1 origin/ezop     # a <type>/<short-kebab-slug> topic
cp "$WORK/$TAG.md" "fork/verification/$TAG.md"
devbox run just test-fast                                 # the validator checks the record
git add "fork/verification/$TAG.md"
git commit -m "chore: record verified $TAG"                # body: Refs #1, never a closing keyword
```

Open the pull request to `ezop` with `Refs #1`. Integrate by a reviewed exact
fast-forward, or Rebase and merge with the old-to-new SHA mapping recorded
(`FORK.md`). The record names the tagged commit `E`, not its own commit, so a
rebase-merge does not void it. No re-run is started once `verify` has produced
the record or while it is in review. A record is never edited, replaced or
removed.

### 6.10 `verify --status` before Handover

Fetch and build the helper again from the new tip if `ezop` moved, then:

```sh
cd "$CLONE"
"$FR" verify --status "$TAG"; echo "exit $?"
```

Pass: `record: verified`, `release: Published-Same`, exit 0. Only then may the
tag and the record's sha256 values be handed over, and the capture of this tag
(`rm -- "$GATES"`) has served its purpose; after an abandonment, the same once
`verify --status` prints `record: abandoned`. Never earlier. Handover takes the tag and the
checksums from the verified record, never from `latest`, the pre-release flag or
the release's own `checksums.txt`; the consumer's own byte check against the
record is what catches a later edit to a published release, and nothing in this
repository can enforce it. Stop: any other output; no Handover (section 7).

### 6.11 The post-first-release readback (FR-15, S3, S4)

After the first Verified Fork Release, read the run list and the settings again
and keep the values in `fork/evidence/post-release-<stamp>.md` (section 5). Run
the listing of section 3.2 into a new file (the preflight's `runs.json` stays
as it was), then:

```sh
cd "$CLONE"
RUNS="$WORK/runs-$(date -u +%Y%m%dT%H%M%SZ).json"
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/actions/runs?per_page=100' > "$RUNS"
jq -r '[.[].workflow_runs[]] | group_by(.workflow_id) | .[] | "\(.[0].workflow_id) \(.[0].path) runs=\(length)"' "$RUNS"
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/releases?per_page=100' | jq '{count: ([.[][]] | length), tags: [.[][].tag_name], upstream_shaped: ([.[][].tag_name | select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))] | length)}'
git fetch --no-tags origin '+refs/heads/main:refs/remotes/origin/main'
git fetch upstream main
git merge-base --is-ancestor origin/main FETCH_HEAD && echo "main holds only Upstream commits"
```

and repeat the settings readbacks of steps 2.2 to 2.4. Pass: runs exist only for
`ci.yml` and `fork-release.yml` besides those the preflight (`runs.json`) listed; none for the
four Upstream paths; `upstream_shaped` is 0 (no fork `v5.1.0` release exists);
the four paths still read `disabled_manually`; settings and rulesets are
unchanged; `main` is an ancestor of `upstream/main`. Stop: any deviation. Turn
Actions off and decide.

## 7. Recovery

Read the state first, always, with `verify --status` (section 1.5), and decide by
this table. `Run red` means the run finished `failure`, `cancelled` or
`timed_out`.

| Situation | Action |
| --- | --- |
| Run `success`, token `none`, release `Published-Same` | not a failure: continue with `verify` (section 6.8) |
| Run red, token `none`, release `None` (the run failed before any release object existed) | a full re-run may be possible: section 7.1 |
| Token `none` with release `Draft`, `Published-Same` or `Other`, the run red, and you are considering a re-run | do not re-run: abandon (section 7.2). A leftover draft is abandoned with reason `draft` and never repeated |
| `verify` refuses on a mismatch (a checksum, the version, the run, `gate-commit` not `E`) | no record is made; the release stays; abandon (section 7.2) |
| `verify` refuses on a failed read | transient: run it again |
| Token `unknown` | wait until the run is over and read again. A persistent `unknown` is investigated, never a retry loop: cancel a stuck run with a stated reason (Manual Operator write, section 3.5) or deal with a duplicate release by hand, then run the command again |
| Token `abandoned` or `invalid` | terminal: the version is spent; use the next explicitly authorized `N` |
| Token `verified` | done; no recovery |
| `verify --status` exits 1 or 2 (a refusal or usage) | nothing was changed: fix the cause (the build, `origin`, the arguments) and read again |
| Token `none` with release `Unknown` (exit 3: the release lookup failed) | a failed lookup is not proof that a release exists, and it is not a reason to abandon. Stop: no re-run and no Handover. Read `verify --status` again once the cause is understood (the `gh` login, the network); if it persists, look at the repository's releases in the browser. Go on by the row that matches what reads back |
| Any other combination, or a condition of section 7.1 that fails (`release: Unknown` excepted: the row above) | do not re-run: abandon (section 7.2) with the reason that fits, `operator-decision` when none does. A run still in progress is waited for, not abandoned |

### 7.1 The pre-re-run check, the re-run, and non-overlap

A re-run is allowed only for a transient failure that happened before any release
object existed. A failure caused by `E` or its tree repeats on every re-run and
can only be abandoned. All of this must hold:

Set `RUN_ID` to the id of the run printed in section 6.7 (for the original run of
the tag), then:

```sh
cd "$CLONE"
RUN_ID=THE_RUN_ID
OUT=$("$FR" verify --status "$TAG"); RC=$?
printf '%s\n' "$OUT"; echo "exit $RC"
[ "$RC" -eq 3 ] && [ "$OUT" = "$(printf 'record: none\nrelease: None')" ] && echo "PRE-RERUN CHECK PASSED" || echo "STOP: do not re-run"
gh api --hostname github.com --method GET -H "$GHV" "repos/Ezopek/proton-cli/actions/runs/$RUN_ID" | jq '{run_attempt, status, conclusion, created_at, head_sha}'
```

1. Both printed lines read `record: none` and `release: None`. Exit code 3 alone
   is not enough: a leftover draft also exits 3, with `release: Draft`.
2. The run is over (`status` is `completed`), and no other attempt is running.
3. It is within 30 days of the original run (counted from `created_at`, the
   conservative reading, since the documentation does not say when the clock
   starts) and the run has been re-run at most 50 times (`run_attempt` is at
   most 50).
4. The workflow is still enabled.
5. The re-run is a **full** re-run of the original run. A partial re-run, a
   re-run of failed jobs, or a re-run of the `stamp` job alone is not a recovery.

**Manual Operator write.** Unverified until first use:

```sh
gh api --hostname github.com --method POST -H "$GHV" "repos/Ezopek/proton-cli/actions/runs/$RUN_ID/rerun"
```

Then wait for the attempt to end, read `verify --status` again, and continue at
section 6.8. No tag is ever deleted, moved or re-pushed; "retry" means this
full re-run and nothing else.

If a condition above does not hold, do not re-run. If condition 1 fails only
because the release reads `Unknown`, nothing is abandoned: a failed lookup does
not show that a release exists. Read again until the release reads something
else; there is no re-run and no Handover meanwhile. For any other failed
condition the version is abandoned (section 7.2), with `window-passed` when the
30 days or the 50 re-runs is what failed, `failed` for a release object that
exists or a cause that repeats, and `draft` for a leftover draft. A run that is
still in progress (condition 2) is waited for first.

**Maintainer non-overlap procedure (not enforcement).** A maintainer starts no
further attempt of a tag's run until the current attempt is terminal, and none
once `verify` has produced a record to be landed or while it is in review.
`verify --status` reads a run that is not over as `unknown`, so the check above
refuses over a run in progress. There is no lock, queue or service; the
procedure is only as good as the two maintainers who keep it. It does not cover
two attempts creating a draft at once, a leftover draft the job token cannot see
(Q5; the check above is the cover), or a manual write by a holder of the write
token.

### 7.2 Abandonment

**Manual Operator write.** The Operator adds one `abandoned` record for the tag,
`fork/verification/<TAG>.md`, landed exactly as in section 6.9. Its content is
five lines, plain ASCII, LF line ends, a final line feed, no blank line:

```text
schema: 1
tag: v5.1.0-ezop.1
status: abandoned
time: 2026-01-02T03:04:05Z
reason: failed
```

Use the real tag, the UTC time (`date -u +%Y-%m-%dT%H:%M:%SZ`) and exactly one
reason code:

| Reason | Use when |
| --- | --- |
| `refused` | a guard in the run refused the tag or its tree |
| `failed` | the run failed or `verify` cannot succeed, and a release object exists or the cause repeats |
| `draft` | a release was left as a draft; it is abandoned and never repeated (AMD-FR18-1) |
| `window-passed` | the window to re-run (30 days, or 50 re-runs) passed |
| `rebased` | an `ezop` rebase left the release open |
| `operator-decision` | the Operator abandoned the version for another reason |

After landing, `verify --status` prints `record: abandoned` and exits 5. The next
attempt is a new version and tag that the Operator explicitly authorizes: the
next `N`, a new `C`, new Pre-Tag Gates and a new capture file, from section 6.2.
An abandoned release is never deleted, and its version is never reused; it stays
downloadable and looks like any other release. This runbook has no step that
deletes a release or a draft, and no tool does. For `v5.1.0-ezop.1` the
abandonment policy recorded under section 6.1 applies.

Two cases have no run to recover. A tag pushed before activation is verified, or
while Actions are off, has no run: the architecture offers no way to run it, so
the explicit recovery decision is abandonment or an architecture amendment that
the Operator approves, never a re-tag. A malformed record, or `invalid` after a
record landed, spends the version the same way (there is one file per tag).

## 8. Mirror updates and Upstream tags

`main` holds only Upstream commits. It changes only by an explicitly authorized
fast-forward to a commit `U` that has been compared and checked, and it is never
rewound (`main-mirror` blocks any rewind and has no bypass, so every offline
check runs before the push). The literal `git fetch && git merge --ff-only &&
git push` form in `FORK.md` refetches, so it is not used after activation.

### 8.1 The compared-commit fast-forward

Work from a clone whose helper is built from the tip you have just fetched
(section 1.4).

```sh
cd "$CLONE"
git fetch upstream main
U=$(git rev-parse FETCH_HEAD)                          # record U; use it, and nothing refetched
git fetch --no-tags origin '+refs/heads/*:refs/remotes/origin/*'
M=$(git ls-remote origin refs/heads/main | cut -f 1)
git cat-file -e "$M^{commit}" && git merge-base --is-ancestor "$M" "$U" && echo "fast-forward only"
for f in release.yml openapi-sync.yml stats.yml web.yml; do printf '%s ' "$f"; gh api --hostname github.com --method GET -H "$GHV" "repos/Ezopek/proton-cli/actions/workflows/$f" | jq -r '.state'; done
git diff --no-renames --name-status "$M" "$U" -- .github
git diff --no-renames --name-only "$M" "$U" -- .github | grep -v '^\.github/ISSUE_TEMPLATE/'
"$FR" guard --profile mirror "$U"
```

Pass:

- the ancestry check prints `fast-forward only` (anything else: stop, `main`
  is never rewound);
- the four states read `disabled_manually`;
- `guard --profile mirror "$U"` prints `checked <U>` (the full SHA, equal to
  `U`) and `mirror: pass`, with nothing held. If it does not pass, the update is
  not applied and `main` lags behind. A held item is released only through a
  reviewed fork change to the classification or to the action list (section
  9.2);
- the first `git diff` lists what changed under `.github/`, and the second
  prints nothing when only `.github/ISSUE_TEMPLATE/**` changed (or nothing
  changed).

Before the push, read `upstream` once more:

```sh
git ls-remote upstream refs/heads/main | cut -f 1     # must equal U
```

If `upstream/main` has moved since `U` was recorded, the comparison starts over
with a new `U`; a new commit is never pushed on the strength of an old
comparison.

**When nothing changed under `.github/` except `.github/ISSUE_TEMPLATE/**`** (the
mirror profile's Inert class) and the states hold, push `U` by explicit
refspec, as the **Manual Operator write** below.

**When anything else under `.github/` changed** (a workflow file, a local
action, `dependabot.yml`, any other file), do all of these in order:

1. Run `guard --profile mirror "$U"` (above). It must pass with nothing held.
2. Check that no `fork-release.yml` run is in progress, note any open release,
   then turn Actions off and read the state back.

```sh
gh api --hostname github.com --method GET -H "$GHV" --paginate --slurp 'repos/Ezopek/proton-cli/actions/workflows/fork-release.yml/runs?per_page=100' | jq '[.[].workflow_runs[] | select(.status != "completed")] | length'
git ls-remote --refs origin 'refs/tags/v*-ezop.*' | cut -f 2 | sed 's|^refs/tags/||' | while read -r t; do echo "== $t"; "$FR" verify --status "$t"; done
```

   Pass: no run is in progress. A release is open while its token is `none` or
   `unknown`. If one is open, turning Actions off may close the re-run of a tag
   that has no release object (Q1): bring the release to a terminal record
   first, or accept that it may have to be abandoned. Then the **Manual Operator
   write** `-F enabled=false` of step 2.7 and the read back of `enabled: false`.
3. Push `U` (the **Manual Operator write** below).
4. Read back every new path as disabled and every changed disabled path as still
   `disabled_manually`. Whether a newly arrived workflow is registered and can
   be disabled while Actions are off is Q2, and it can only be observed after the
   push. For a path of the four that is new (listed `A` by the first `git diff`),
   if its state is not `disabled_manually`, disable it (**Manual Operator
   write**, the call of step 2.2) and read it back. If Q2 fails, Actions stay
   off, the Operator decides, and `main` is never rewound.
5. Run the full preflight (section 3), including the ref sweep.
6. Enable Actions, last, and read the four states back (steps 2.7 and 2.8). A new
   Verified Activation is needed, with new activation evidence, and no Release
   Tag is pushed before it.

If step 4 or step 5 fails, Actions stay off and the Operator decides. That never
means rewinding `main`.

**Manual Operator write.** The push, by explicit refspec, whether or not
`.github/` changed:

```sh
git push origin "${U:?U is not set}:refs/heads/main"
```

Never `+`, never a refetched commit. The S4 observation, taken right after:

```sh
git ls-remote origin refs/heads/main | cut -f 1       # must equal U
git merge-base --is-ancestor "$M" "$U" && echo "the update was a fast-forward only"
```

Write `fork/evidence/mirror-<stamp>.md` (section 5): `U`, the `main` SHA before
and after, "fast-forward only", the class of the `.github/` change (none, inert,
other), the helper commit that ran `guard --profile mirror` (`devbox run go
version -m "$FR" | grep vcs.revision`), the four states, whether Actions were on,
and the run that the push caused. With Actions on, expect one `ci.yml` run for
the push to `main`; record it with its disposition (a read-only run, accepted),
and record any other run as a deviation.

### 8.2 Upstream tag pushes

Besides Release Tags, the only tags pushed to origin are Upstream release tags
fetched from `upstream`, one per push. Pushing a tag runs the tag-reachable
`push` workflows of that tree, so the profile check comes first. Every rebase
pushes its new Upstream Base tag this way, and before the first Release Tag on
a new base.

```sh
cd "$CLONE"
T=v5.1.1                                               # the Upstream tag, one per push
git fetch upstream tag "$T"
P=$(git rev-parse "$T^{commit}")
peel upstream "$T"                                     # must print P
"$FR" guard --profile mirror "$P"
peel origin "$T"                                       # must print nothing, or P if already there
```

Pass: `peel upstream` prints `P`; `guard --profile mirror` prints `checked <P>`
and `mirror: pass`; origin has no such tag (if it has it at `P`, there is
nothing to push). Stop: anything held, a mismatch, or a tag on origin at another
commit: the tag is not pushed, never force-pushed, and the procedure stops until
the verified procedure of section 8.1 has run.

**Manual Operator write.**

```sh
git push origin "refs/tags/${T:?T is not set}:refs/tags/$T"
peel origin "$T"                                       # must print P
```

Never `--tags` or `--follow-tags`.

## 9. Rebases and changed workflow files

### 9.1 Rebasing `ezop` onto an Upstream tag

Only the Operator rebases `ezop` (`FORK.md`), and no step of this runbook or any
tool force-pushes automatically. No rebase happens while a release is open: a
rebase would leave the old release commit out of `origin/ezop`, so the re-run and
`verify` would refuse, and the only way forward would be to abandon that version.

Before the rebase, read every fork tag:

```sh
cd "$CLONE"
OLD=$(git ls-remote origin refs/heads/ezop | cut -f 1)        # the exact observed old SHA
git ls-remote --refs origin 'refs/tags/v*-ezop.*' | cut -f 2 | sed 's|^refs/tags/||' | while read -r t; do echo "== $t"; "$FR" verify --status "$t"; done
```

Pass: every token is `verified` or `abandoned` (`invalid` is terminal and not
open). Stop: any `none` or `unknown`.

Do the rebase as local work, dropping any patch Upstream now ships and removing
any new Upstream workflow file in a fork commit. Then, with `NEW` the rebased
tip and `OLD_BASE`, `NEW_BASE` the old and new Upstream tags, three checks before
the push:

```sh
devbox run just test-fast                                      # includes the release profile
[ "$(git rev-parse "$OLD:fork/verification")" = "$(git rev-parse "$NEW:fork/verification")" ] && echo "fork/verification is byte-identical"
git range-diff "$OLD_BASE..$OLD" "$NEW_BASE..$NEW"
git diff --no-renames --name-status "$OLD" "$NEW" -- .github
```

- `just test-fast` passes.
- The `fork/verification/` tree id is the same on both sides (a lost record would
  read as Absent).
- The old-to-new SHA mapping is recorded from the `range-diff` pairs (in the pull
  request or the evidence), and the actual source and tree are compared, not
  just the messages.
- The last command lists any `.github/` difference. If `.github/workflows/`
  differs from the old tip's, the rebase takes the steps of section 9.2 with the
  push below as its step 3 (a rebase is not a PR).

**Manual Operator write.** The push names the exact observed old SHA and the
checked new commit; never plain `--force`:

```sh
git push --force-with-lease="ezop:${OLD:?OLD is not set}" origin "${NEW:?NEW is not set}:refs/heads/ezop"
```

Afterward: read `git ls-remote origin refs/heads/ezop` (it must print `NEW`);
build the helper from the new tip (section 1.4); run `guard --profile release
"$NEW"` (the authoritative check on the landed tip); push the new Upstream Base
tag by section 8.2; and read `verify --status` for every earlier tag. A verified
tag still reads `verified` because the status needs no ancestry of its commit.

If a rebase happened with a release open, or `fork/verification/` was not
byte-identical: turn Actions off while any recorded tag is unverified, restore
the exact bytes from the old SHA as a record-only topic, re-read each state, and
abandon (reason `rebased`) any release that was repeated meanwhile or whose
commit is no longer an ancestor.

### 9.2 A topic that changes workflow files after Verified Activation

Only the concrete change that can become live gets this treatment: a topic whose
diff against the `ezop` tip adds, changes or removes a workflow file, a local
action, `dependabot.yml` or any other file under `.github/` except
`.github/ISSUE_TEMPLATE/**` (the Drift Guard, an action SHA bump, an edit to
`ci.yml` or `fork-release.yml`), and a mirror update that does the same
(section 8.1). The safe reading is that such a topic runs its own workflow
definition as soon as its pull request is opened; the documentation does not say
so in terms, and this design does not need it to be false. Check, on the topic
branch based on the current `ezop` tip:

```sh
git fetch --no-tags origin ezop
git diff --no-renames --name-only origin/ezop...HEAD -- .github | grep -v '^\.github/ISSUE_TEMPLATE/'
```

If that prints nothing, the topic needs no admission, no check and no approval
beyond the reviewed topic that `FORK.md` already requires. There is no recurring
per-branch check. Otherwise:

1. **Before turning Actions off:** no run of `fork-release.yml` is in progress,
   and any open release is noted (the commands of section 8.1 step 2). If a release
   is open, turning Actions off may close the re-run of a tag that has no release
   object (Q1): bring the release to a terminal record first, or accept that it
   may have to be abandoned.
2. **Turn Actions off and read the state back** (the **Manual Operator write** of
   step 2.7 with `enabled=false`). The topic is not pushed, and no pull request is
   opened, before this.
3. **Push and review as an ordinary `FORK.md` pull request.** Nothing runs. Run the
   candidate self-tests locally, in a shell with no Proton or GitHub credentials
   loaded:

```sh
unset GH_TOKEN GITHUB_TOKEN
for v in $(env | cut -d= -f1 | grep '^PROTON_CLI_TEST_'); do unset "$v"; done
devbox run just test-fast
devbox run just lint
```

   These are checks of the candidate by the candidate: **untrusted and advisory**.
   Label them so in the pull request. They are never a release qualification and
   do not replace the review, which reads the diff, the lists and the pinned
   shape. `guard --profile` is not used on an unlanded candidate. For a topic
   that adds or bumps an action in the helper's action list, also confirm before
   merging that the SHA is the peeled commit of a release tag in the action's own
   repository (`peel https://github.com/OWNER/REPO vX.Y.Z` prints it). The topic
   lands by the `FORK.md` path, preferably an exact fast-forward.
4. **After landing, activation repeats from step 2.2**, with the preflight fresh
   (section 3), and a new Verified Activation is needed before Actions run again.
   The authoritative check is `guard --profile release` on the exact landed
   `ezop` tip from a landed helper (section 3.8.2). A tree that fails it keeps
   Actions off, and the fix is a forward topic through the same steps, never a
   force-push. No Release Tag is pushed while Actions are off.

A topic that edits the helper source or the profile lists is judged by itself:
review at landing is the only control, because the lists and the pinned shape are
helper constants and the check does not re-judge a change to the helper. The
in-run guard at a tag repeats the profile with the helper at `E`. A defect that
slips through is fixed by a forward topic, and a release it let through is
abandoned.

## 10. Manual gates and stop table

Every gate below is run by the Operator, is a procedure and not platform
enforcement, and has one owner: the Operator. Skipping or doing a gate wrong is a
violation, never an accepted state. Nothing, human or tool, has authority to
delete, move, reuse or re-push a `v*-ezop.*` tag.

| Gate | Section | If skipped | What notices | Recovery |
| --- | --- | --- | --- | --- |
| Pre-Tag Gates on the landed `C`, clean checkout | 6.3 | the gates did not run on the landed `C`, or ran on a PR head a rebase later rewrote | `verify` refuses a missing or malformed capture, one for another commit, or one with a wrong value, after publication. It cannot tell whether the attested gates were run: the capture is the Operator's statement | a refused record: tag and release stay; abandon and use the next `N` once the gates pass on a new commit. A skipped gate that left a valid-looking capture is noticed by no tool |
| `guard --pre-push` before a tag push | 6.5 | the tag is pushed without the local check. The in-run guard repeats the tree checks but cannot see whether this command was run: a tree that fails them publishes nothing, the run ends red and the version is spent; a tree that passes publishes normally | the red run, for a tree that fails the checks only; nothing notices a skipped command on a passing tree | the tag is immutable; abandon and authorize the next `N` |
| Core steps 2.2 to 2.5: disables, secrets, settings including FR-12, option state | 2.2 to 2.5 | Actions enabled with a path not disabled, a secret present, or an FR-12 item unmet | the activation evidence lacks the readback lines | Actions off at once, list and dispose every run that started, repeat from step 2.2 |
| Preflight, ref sweep and push freeze | 3 | Actions run with no fresh passing preflight, or a push lands in the window | the fresh readback voids it; the run list shows unexpected runs | Actions off, dispose the runs, repeat from step 2.2; a tag pushed in the window stays immutable and is verified or abandoned |
| Q4 risk and abandonment policy recorded before the first tag | 6.1 | the decision lines are absent | review of the evidence pull request | a refused or failed first release is recovered only by the Operator's abandonment policy |
| First Release Tag only after Verified Activation | 6.1 | a tag is pushed while Actions are off: no run, the tag is spent | nothing in the helper | an explicit Operator recovery decision; never a re-tag |
| A topic that changes workflow files: Actions off first | 9.2 | its own workflow definition runs in its pull request with Actions on | an unexpected run in the run list; review | Actions off, list and dispose the runs, check them against the secrets readback, requalify |
| Review of a topic that edits the helper or the profile lists | 9.2 | a defective helper change lands and is judged by itself | nothing automatic; review at landing only | a forward topic; a release the defect let through is abandoned |
| `verify --status` before any re-run, and no re-run once a record is produced or in review | 7.1 | a re-run over a release object that exists: a second draft, or replaced assets under the baseline settings | `verify --status`: `none` with `release: Draft`, `unknown`, or `invalid` | a release that exists is abandoned and the next `N` is used; a draft is never deleted by tooling |
| `verify --status` exit zero before Handover | 6.10 | there is no record, or it disagrees with the release | `verify --status` | no Handover; abandonment and the next `N`, or a re-run only for a tag with no release object |
| Rebase: `fork/verification/` byte-identical, and no rebase while a release is open | 9.1 | a lost record reads Absent, or the old release commit is no longer an ancestor | only the Operator's own comparison with the old SHA | Actions off while any recorded tag is unverified, restore the bytes from the old SHA, re-read each state, abandon what was repeated |
| Compared-commit mirror procedure, and the profile check before an Upstream tag push | 8 | `main` is fast-forwarded to an unchecked or refetched commit, or an Upstream tag is pushed unchecked | the next mirror comparison and the S4 observation; an unexpected run | Actions off, the sweep and a full requalification; `main` is never rewound and the tag stays |
| Base tag on origin peels to the same commit as on `upstream`, before every Release Tag | 6.4 | the base check reads origin tags, so a wrong-commit tag could satisfy it | nothing in the run | the version is abandoned; no tag is moved |
| Option B `main-mirror` bracket | 2.10 | `main-mirror` is left adjusted | the repeat readbacks of step 6 | Actions stay off and the Operator handles it as an incident; `main` is checked against the recorded SHA |

The Manual Operator writes in this file, so that none is missed: disabling a
workflow path (2.2, 8.1), the Actions switch (2.7, 8.1, 9.2), removing a secret
(2.3), creating a ruleset (2.4), adjusting a ruleset and changing the default
branch (2.10), canceling a run (3.5), deleting a branch (3.8.3), closing a pull
request (3.8.4), pushing a tag or a ref (6.6, 8.1, 8.2, 9.1), re-running a run
(7.1), and landing evidence or a record, its topic push and pull request
included (2.8, 6.9, 6.11, 7.2, 8.1).

## 11. Limits and open questions

This runbook cannot guarantee any of the following, and nothing in it claims a
risk solved that is not.

- **Every command was written, not run.** The real GitHub behavior of every
  release step is unverified before the first run: the first real observation of
  the run lookups, `verify` and `verify --status` is the bootstrap tag itself,
  and no rehearsal or test tag is permitted. A wiring failure that remains is
  recovered as section 7 allows. None of the documentation this runbook cites is
  live proof, and a documented control is not proof of the fork's actual state.
- **Q1: disabling and reading back while Actions are off.** No official page says
  whether a workflow can be disabled, have its state read, or have its runs
  listed while the repository switch is off. Also open: what the readback shows
  after Actions are switched off and on again and after a default-branch change,
  whether option B shows the four paths as `deleted`, and whether a completed run
  of a disabled workflow can be re-run. Observed 2026-11-07, Actions off: reads and
  a disable of the four inherited paths returned HTTP 404, which is not a state;
  whether they register once Actions are on is read at section 2.7a.
  Consequence: any answer that is not a clear yes keeps Actions off, and under
  option B the paths are decided at section 2.7a.
- **Q2: a newly arrived workflow.** Whether it is registered and can be disabled
  while Actions are off is only observable after the `main` fast-forward, which
  cannot be undone. The offline checks run first; if Q2 fails, Actions stay off
  and `main` is never rewound.
- **Q3: what the rulesets enforce.** Not shown: whether they bind a workflow's
  `GITHUB_TOKEN`, whether `v*` tag creation is limited to the Operator, PR-only
  updates of `ezop`, and human versus automation identity (rulesets cannot tell
  the owner from automation on the same account). The FR-12 items among these are
  a prerequisite, not a waiver; the rest are defense in depth.
- **Q4: release creation for a tag whose commit changes workflow files.** GitHub's
  page says the token must be authorized to modify workflows and that the
  `GITHUB_TOKEN` of Actions cannot be, and it does not say which commit is
  evaluated when the tag already exists. It cannot be answered offline, and no
  workaround is adopted (no new credential, no `target_commitish`, no rehearsal,
  no re-tag). A refusal leaves a draft or nothing and is recovered only by
  abandonment; for `v5.1.0-ezop.1` that is the Operator's recorded policy.
- **Q5: draft visibility to the job token.** The platform page says only users
  with push access see draft listings. If the job token cannot, a leftover draft
  reads as None inside the run and a repeat would create a second release object;
  the in-run Draft refusal is best effort, and the Operator's own pre-re-run check
  (section 7.1) is the cover.
- **Q6: run fields and build identity.** The documentation does not describe
  `head_sha`, `run_attempt` or `status` of a tag-push run, whether a re-run can
  overlap an attempt in progress, whether the contents lookup accepts a commit
  SHA, or how the build stamp behaves in a linked worktree. Every doubt refuses.
  A defect is fixed by an ordinary reviewed topic and the command is run again.
- **Native Windows is unsupported and untested.** The Operator-side commands run
  the downloaded binary under a POSIX environment; use Linux or macOS.
- **The Operator's own `gh` login is relied on.** Every read and every Operator
  write uses it; a stale or under-privileged login is a stop, and no check here
  proves that a given change was made by a human.
- **The config-shadow residual.** GoReleaser reads `.goreleaser.yaml` only if none
  of `.config/goreleaser.yml`, `.config/goreleaser.yaml` and `.goreleaser.yml`
  exists, and the workflow passes no explicit `--config`. A tree with one of those
  files would be released with a configuration the `release` profile never
  checked. The required `just test-fast` gate catches it in the development tree,
  and the guard does not check it in a tagged tree. An explicit `--config` is
  deferred, not decided.
- **The non-overlap rule is a maintainer procedure.** It is not enforced. Two
  attempts that overlap, a repeat over a draft the job token cannot see, and a
  manual write by a holder of the write token are not prevented. A manual write
  that leaves the recorded identity unchanged, such as an asset-only edit, is not
  detected in this repository; it is the Handover consumer's byte check.
- **Derived values.** The closed `status` and `conclusion` sets of section 3.3
  are derived from the documented `status` query values, because the REST schema
  documents no `conclusion` list. The exact path and values of the fork pull
  request approval policy, and the endpoints of section 4, are unverified. A
  helper that is stale but landed is accepted by the helper itself; the recipe is
  to build the tip you have just fetched.
- **`FORK.md` wording.** The Releasing, Following Upstream and Conventions rules
  this runbook relies on are in `FORK.md` (merged 2026-11-07). If they disagree
  with this runbook, `FORK.md` wins and the runbook is corrected.
- **Toolchain currency.** The accepted pin (Go 1.27.0, GoReleaser 2.17.1) is kept;
  later Go point releases exist, so a binary built from it carries the 1.27.0
  standard library. Moving the pin is its own reviewed change, not a gate here.
