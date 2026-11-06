# Verification Records

This directory holds the terminal, sanitized record of each fork release tag
(`vX.Y.Z-ezop.N`). A tag has at most one record, `<tag>.md`, and the record is
the only marker that the tag's version is settled: either the release was
verified, or the version was abandoned.

The field list below is owned by the release helper in `scripts/fork-release/`.
A `just test-fast` test compares this file with the helper's key set in both
directions, so a field cannot be added, renamed or dropped on one side alone.

## Rules

- A record is written once, by the Operator, through the pull request workflow
  in `FORK.md`. It is never edited, replaced or removed, and a rebase of `ezop`
  leaves this directory byte-identical.
- A record counts only once it is on `ezop`. A tag whose record exists is spent;
  the next release uses the next `N`.
- The file name is the tag plus `.md`, and the `tag` field repeats it. Every
  file in this directory other than this README must be a regular file with
  such a name that holds one valid record naming that tag. A validator in
  `just test-fast` checks every record before it can land.
- A record is plain text: printable ASCII, LF line ends, one `key: value` per
  line, no blank line, no comment, and a final line feed. A key appears once.
  Every value is one line without leading or trailing space. Only `exceptions`
  may be empty, written as `exceptions:` with nothing after the colon.
- A record holds sanitized facts only: versions, hashes, counts, states and
  times. It never holds raw API output, credentials, account data, local paths
  or personal content.
- The first four keys are `schema`, `tag`, `status` and `time`, in that order.
  `status: abandoned` adds exactly one more key, `reason`. `status: verified`
  adds the remaining fields in the order they are listed below, with the
  `asset.<name>` lines between `commit` and `exceptions`, sorted bytewise by
  name.
- `time` is UTC, `YYYY-MM-DDThh:mm:ssZ`, and a real instant. A hash named
  "blob" is a 40-character lowercase hex Git object id. A sha256 is 64
  lowercase hex characters.
- A record file larger than 64 KiB (65,536 bytes) is a finding. The validator
  checks the size after it reads the file; real records are far smaller.
- The validator judges shape and the invariants stated here. Whether a verified
  record is complete (for example, that all six binaries are present) is judged
  when a release is verified, not when a record is checked into this directory.

## Fields

Every record:

- `schema` - the record format version, always `1`.
- `tag` - the Release Tag; equal to the file name without `.md`.
- `status` - `verified` or `abandoned`.
- `time` - when the record was made, in UTC.
- `reason` - abandoned records only: one code from the list under Reasons.

Verified records only:

- `commit` - the commit the tag names, 40 lowercase hex characters.
- `exceptions` - comma-separated names, sorted bytewise, of assets that were
  downloaded but are not listed in the release checksums. Every name must also
  appear among the `asset.<name>` lines. Empty when there is none.
- `host-platform` - the platform of the host that ran the downloaded binary.
- `proton-version` - the version the downloaded binary reported; equal to the
  tag without its leading `v`.
- `goreleaser-config-blob` - the Git blob id of `.goreleaser.yaml` at `commit`.
- `devbox-lock-blob` - the Git blob id of `devbox.lock` at `commit`.
- `go-version` - the Go version of the toolchain, as a dotted number.
- `goreleaser-version` - the GoReleaser version, as a dotted number.
- `run-id` - the id of the release workflow run, a positive integer.
- `run-attempt` - the attempt of that run, a positive integer.
- `run-conclusion` - the conclusion of that run; must be `success`.
- `attested.gate-commit` - the commit the pre-tag gates ran on; must equal
  `commit`.
- `attested.gates` - the gates in the order they ran; exactly
  `test-fast,lint,build,snapshot`.
- `attested.lint-diff` - whether lint left a diff; exactly `none`.
- `attested.go-version` - the Go version the gates used, as a dotted number.
- `attested.goreleaser-version` - the GoReleaser version the gates used, as a
  dotted number.
- `attested.snapshot-version` - the version line the snapshot build reported.

The `attested.*` fields are the Operator's attestation, copied from the
gate-capture file, which uses the same names without the `attested.` prefix.

## Pattern keys

- `asset.<name>` - one line per downloaded asset: the asset's file name in
  place of `<name>`, and the sha256 of the file as downloaded from the
  published release. A name starts with a letter or digit, continues with
  letters, digits, `.`, `_` or `-`, and is at most 100 characters long. Names
  are unique and sorted bytewise.
- `attested.<key>` - stands for the `attested.*` fields listed above; no other
  `attested.` key exists.

## Reasons

The closed list of values for `reason`:

- `refused` - the release run was refused by a guard.
- `failed` - the release run failed.
- `draft` - the release was left as a draft.
- `window-passed` - the window to repeat the run passed.
- `rebased` - an `ezop` rebase left the release open.
- `operator-decision` - the Operator abandoned the version.
