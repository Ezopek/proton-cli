# Architecture as-is — source map

Verified against `ezop` at `dfa2c1dfcd7de0025f85b73b3968f23d05c96ae9` (Upstream `v5.1.0` tag commit `03f5c254dcd9` plus one fork commit adding `FORK.md`). Upstream's own layout table is `CONTRIBUTING.md` › Project layout; this page does not repeat it. It adds what fork adoption needs: how a run flows, where the version comes from, and which code knows the Upstream repository by name.

Everything here was read from source. Nothing was built or executed.

## Shape

One Go module, `github.com/roman-16/proton-cli` (`go.mod:1`, `go 1.27.0` at `go.mod:3`). It builds one CGO-free binary, `proton`, plus a Windows-only launcher, `proton-cli`.

```text
cmd/proton/main.go ──► internal/cli.Execute()             (cmd/proton/main.go:5-7)
cmd/proton-cli/main.go ──► exec "proton" next to itself      (cmd/proton-cli/main.go:24-41; Windows zip only)

internal/cli (cobra tree, root.go)
  ├─ PersistentPreRunE: resolveSettings → app.New(...) → kit.Judge   (root.go:158-205)
  ├─ apps:    mail drive calendar contacts pass                       (root.go:222)
  ├─ account: account api index                                       (root.go:223)
  └─ self:    changelog report update uninstall version completion skill (root.go:224-225)
        │
internal/cli/kit   declared language: verbs, placeholders, flags, Run/Mutate/Create/List/Show
internal/app       one invocation: settings, UI, API client, session, credentials, run log
internal/service/* per-product logic (account calendar contacts drive mail pass)
internal/proton    HTTP client, auth/SRP, sessions, human verification, dry-run refusal
internal/account/* key hierarchy, session files, local key, forks
internal/crypto, internal/ical, internal/vcard, ...  formats and crypto helpers
internal/ui        all stream output, golden-tested
internal/redact, internal/runlog, internal/skip   logging, redaction, skipped-item accounting
internal/selfmanage  install-channel detection, update/uninstall, Upstream release lookups
```

## Run lifecycle (observed in source)

1. `Execute` sets up signal handling, rewrites leading-dash IDs (`preprocessArgs`), probes for unknown subcommands on a throwaway tree, then executes a fresh tree (`internal/cli/root.go:241-301`).
2. `PersistentPreRunE` skips completion requests and `help`, resolves settings from flag › env › file (`internal/config/config.go`), and builds the `App` (`internal/app/app.go:110-174`). The app opens the per-day run log, builds the UI, sets `slog.Default`, builds the Proton client, and loads the profile's session file.
3. The command body runs through `kit.Run`. Mutations go through `kit.Mutate`/`kit.Create`, which is what makes `--dry-run` structural (Upstream `AGENTS.md` › interface rules).
4. `finish` records the outcome. `announce` may print a once-a-day update notice on a TTY (`root.go:356-364`, `internal/cli/self/notice.go:40-64`).
5. A panic is caught, its stack goes to the run log, and the process exits with `ExitBug` (`root.go:273-277,321-332`).

## Version path

| Fact | Source |
|---|---|
| `var version = "dev"`, overridden by `-ldflags -X` | `internal/cli/root.go:38-39` |
| The same value feeds cobra `--version`, `proton version`, `report`, `update`, the notice and `skill` | `root.go:102,190,224-225,362` |
| `proton version` prints Version, Go, Platform and the VCS Revision (12 chars) | `internal/cli/self/self.go:19-64` |
| GoReleaser stamps `{{.Version}}`, which it derives from the tag without the `v`. Tag `v5.1.0-ezop.1` therefore yields `5.1.0-ezop.1` with no config change | `.goreleaser.yaml:29-30` |
| The Windows `proton-cli` launcher is built with `-s -w` only and carries no version. It just runs `proton` | `.goreleaser.yaml:45-56` |
| `just build` is `go build -o proton ./cmd/proton` with no ldflags, so it reports `dev` | `justfile:1-3` |
| Nix stamps the git short revision, not a semver | `flake.nix:16,31-35` |
| The fork version reaches Proton only in the User-Agent, `proton-cli/<version>`. `x-pm-appversion` stays `"Other"` | `internal/app/app.go:115,759-766`; `internal/proton/client.go:27,834` |

Consequence for issue #1: an `X.Y.Z-ezop.N` version string needs no ldflags edit as long as the release is built by GoReleaser from a tag named `vX.Y.Z-ezop.N`. A `just build` or `go install` binary reports `dev`, so the version gate can only be shown on a GoReleaser (snapshot or release) artifact.

## Code that names the Upstream repository

Every one of these is a functional surface. Changing any of them is a functional patch, which `FORK.md` excludes from `v5.1.0-ezop.1`.

| Location | What it points at |
|---|---|
| `internal/selfmanage/update.go:26-39` | `owner = "roman-16"`; the releases URL; the CHANGELOG on Upstream `main` |
| `internal/selfmanage/update.go:65-101` | `LatestVersion` follows Upstream `/releases/latest`; `IsNewer` uses semver |
| `internal/cli/self/update.go:56,74-78` | Release-notes URL and the install hint use Upstream |
| `internal/cli/self/changelog.go:17` | Upstream releases page |
| `internal/cli/self/report.go:30` | Upstream bug-report issue form |
| `internal/cli/self/selfmanage.go:114-116` | Upstream npm package name in hints |
| `scripts/install.sh:34,53,55`, `scripts/install.ps1` | `REPO="roman-16/proton-cli"` |
| `scripts/stats/main.go:40,46,50` | Upstream repo and npm scope counters |

## Directory pointers for adoption work

- Release surface: `.goreleaser.yaml`, `.github/workflows/release.yml`, `scripts/changelog/`, `scripts/gen-completions.sh`, `scripts/publish-npm.mjs`. See `workflows-and-release-inventory.md`.
- Drift guard (issue #2): `.github/workflows/openapi-sync.yml`, `scripts/openapi-guard/`, `scripts/openapi-generator/`, `tests/api-coverage.golden`, `openapi.yaml`.
- First functional patch (issue #3, out of scope here): `internal/ical/vevent.go`, `internal/service/calendar/events.go`.
- Test tiers: `tests/AGENTS.md`, `tests/live/`, `tests/offline/`, `tests/rules/`, `tests/install/`.
