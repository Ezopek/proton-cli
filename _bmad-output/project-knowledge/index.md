# proton-cli (Ezopek fork) — as-is project knowledge

BMAD `project_knowledge` folder (`_bmad/config.toml` › `modules.bmm.project_knowledge`). Produced in the brownfield as-is phase on 2026-10-25 against `ezop` `dfa2c1dfcd7de0025f85b73b3968f23d05c96ae9`. Every claim cites a repository-relative file and line. The author built, linted and tested nothing. The only gate results cited are the controller's reported baseline on the unchanged tree.

`FORK.md` is the only source of fork policy. These pages are evidence and planning input. Where they paraphrase `FORK.md`, `FORK.md` wins.

| Page | Read it for |
|---|---|
| [architecture-as-is.md](architecture-as-is.md) | source map, run lifecycle, the version path, code that names Upstream |
| [toolchain-build-test-release.md](toolchain-build-test-release.md) | toolchain, `just` recipes and which are safe, test tiers (all unrun), release entry points |
| [workflows-and-release-inventory.md](workflows-and-release-inventory.md) | all five workflows, GoReleaser publishers, secrets, trigger semantics, hazards H1–H4, activation options |
| [boundaries-auth-config-logging.md](boundaries-auth-config-logging.md) | on-disk state, settings resolution, credentials, network identity, outbound GitHub calls |
| [interfaces-and-upstream-preservation.md](interfaces-and-upstream-preservation.md) | contracts the fork exposes; preservation obligations 1–12 (12: the drift guard is required); FORK as sole policy source; issue-footer conflict |
| [risks-and-constraints.md](risks-and-constraints.md) | risk register R1–R14 (R14: the required drift guard is not running), hard constraints |
| [issues-snapshot.md](issues-snapshot.md) | all 13 fork issues; #1 and #2 requirements; #1 → #2 (required, deferred) → #3 sequencing |
| [adoption-assessment.md](adoption-assessment.md) | the delta for #1 and #2; **decision register D1–D8** |
| [project-context-proposal.md](project-context-proposal.md) | `bmad-project-context` adopt run: ledger, controller-supplied interview facts, applied block and self-validation |
| [fork-adoption-brief.md](fork-adoption-brief.md) | public-safe driving input for the coordinator PRD |

Next BMAD phases: [`../NATIVE-BMAD-NEXT.md`](../NATIVE-BMAD-NEXT.md).

Workflows actually used:

- `bmad-help`: route discovery.
- `bmad-project-context` (adopt): steps 1–3 and the step-5 proposal in the as-is pass; then step 4 with controller-supplied facts, the step-5 splice, the step-6 close and the Audit self-validation in the controller-authorized completion pass.

The sequential self-checks are not independent review.
