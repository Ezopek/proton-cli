# Auth, profile, config and logging boundaries (source facts only)

Read from source only. No session file, config file, log or account was opened, and no credential was read. Where this page names a file on disk, it is the path the code would use, not one that was inspected.

## On-disk state

Everything lives under `config.Dir()` = `os.UserConfigDir()/proton-cli` (`internal/config/config.go:40-48`), which is `~/.config/proton-cli` on Linux.

| Item | Path under `config.Dir()` | Code | Notes |
|---|---|---|---|
| Settings | `config.yaml`, or `$PROTON_CONFIG` / `--config` | `config.go:34-38,55-79` | global plus `per-profile` sections. `no-update-check` is a setting |
| Sessions | `sessions/<profile>.json` | `internal/account/session/session.go:96-112` | UID, access and refresh tokens, user ID, email, `enc_key_blob` (the key password sealed with a server-held client key), app version, base URL (`session.go:25-39`) |
| ID cache | `idcache/<profile>.json` | `internal/app/app.go:297` | |
| Run log | `logs/<day>.jsonl`, 16 days kept, dir `0700`, file `0600` | `internal/runlog/runlog.go:45-79` | always written at debug unless `--no-log` / `PROTON_NO_LOG` |
| Update check | `update-check.json` | `internal/selfmanage/notice.go:46-55` | not profile-scoped |

## Resolution rules (Upstream design, preserved)

- An ordinary setting goes flag › env › file › default. The confirmation policy goes strictest-wins (`config.go:1-13`; `CONTRIBUTING.md` › Design rules).
- Profile comes from `--profile`/`-p`, then `PROTON_PROFILE`, then the file's `profile` (`config.go:235`).
- Environment variables read by `config`: `PROTON_CONFIG`, `PROTON_PROFILE`, `PROTON_LOG_LEVEL`, `PROTON_CONFIRM`, `PROTON_NO_LOG`, `PROTON_NO_INPUT`, `PROTON_NO_UPDATE_CHECK`, `NO_COLOR`, `FORCE_COLOR`, plus the zone variable (`config.go:38,265,290,292,387,391`).
- The app also reads `PROTON_API_URL` (hidden `--api-url`) and `PROTON_VERIFIED` (`internal/app/app.go:113-114`).

## Credentials

- `internal/app/credentials.go` is the only code that asks a person for a secret (`credentials.go:66-80`). Secrets come from `--*-password-file PATH`, where `-` means stdin, and never from argv (`CONTRIBUTING.md` › Design rules).
- `--no-input` / `PROTON_NO_INPUT` turns a missing credential into an error, so an unattended caller such as the MCP server never hangs (`root.go:132`).

## Network identity

| Header or field | Value | Ref |
|---|---|---|
| Base URL | `https://mail.proton.me/api` unless overridden | `internal/proton/client.go:26` |
| `x-pm-appversion` | `"Other"` | `client.go:27,834` |
| User-Agent | `proton-cli/<version>`; a fork build sends `proton-cli/5.1.0-ezop.1` | `internal/app/app.go:759-766` |

Fork note: the release changes the User-Agent string and nothing else Proton sees. Whether Proton treats that string differently is unknown. It can only be observed in an authorized live smoke test, which belongs to the MCP repository's suite on `calendar-dev`/`MCP Test` and is not part of this phase.

## Logging and redaction obligations (Upstream; apply to any fork patch)

- `internal/redact` decides every attribute's policy, and each attribute name is declared in `redact.Fields`. The conformance test checks this in both directions (Upstream `AGENTS.md` › A failure has to leave a trace).
- A user-supplied name reaches an error only through `errs.Naming`.
- `skip.Record` is required wherever information would otherwise be dropped. The conformance test rejects a silent `if err != nil { continue }` under `internal/service` or `internal/account`.
- A fork release-only change touches no Go code, so none of these rules is exercised by issue #1. They bind issue #3 onward.

## Outbound, non-Proton calls a fork binary makes

| Call | When | Target | Ref |
|---|---|---|---|
| Update notice | at most once a day, standalone install, stderr is a TTY, not quiet, not `no-update-check` | `HEAD https://github.com/roman-16/proton-cli/releases/latest` | `internal/cli/self/notice.go:40-64`; `selfmanage/update.go:65-86` |
| `proton update` | explicit | Upstream `releases/download/v…/checksums.txt` and binary | `selfmanage/update.go:121-146` |
| `proton changelog` | explicit | Upstream `main` `CHANGELOG.md` | `selfmanage/update.go:39-44` |

Fork consequence, derived and not executed: `IsNewer("5.1.0", "5.1.0-ezop.1")` is true, because a semver pre-release sorts below its release (`selfmanage/update.go:91-101`). A standalone fork binary on a TTY would therefore be told that `5.1.0` is available, and `proton update` would replace it with the **Upstream** binary, which fails the MCP version gate. The MCP server runs `proton` without a TTY, which suppresses the notice (`notice.go:41`). It should also set `PROTON_NO_UPDATE_CHECK` and never call `update`. Fixing this in code is a functional patch and is deferred past `v5.1.0-ezop.1` (risk R3).
