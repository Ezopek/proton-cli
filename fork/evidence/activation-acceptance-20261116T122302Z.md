# Activation acceptance 20261116T122302Z

An operator decision note under runbook section 5. It was recorded at the UTC time in the file name (task EZO-124, part of EZO-119). It observes nothing and adds no readback.

## Decision

- On 2026-11-16 the operator accepted the historical activation of 2026-11-07 (`ezop` at `fd3d3ad200b6471f2bf2b3b7a790c5ab3a870176`), as recorded in `activation-20261107T035332Z.md`, as the accepted basis for the first bullet of runbook section 6.1 (Verified Activation is done and its evidence is landed), **for the tag `v5.1.0-ezop.3` only**.
- This is an explicit, one-off exception decided by the operator.

## The gaps stay as recorded

- The step 2.8 readbacks that were not taken (the tag list, the open pull request list, the workflow-permission settings and secret counts, and the run-signature comparison) were not performed. This note does not claim them.
- Verified Activation remains "not established in full" in `activation-20261107T035332Z.md`, as a historical fact. That note is not edited.

## What this does not do

- It is not a general change to runbook section 2.8 or section 6.1, and it does not apply to any other tag.
- It does not exempt any other section 6.1 precondition or any current release check: the pre-tag gates and the rest of section 6 apply unchanged.
- It authorizes no tag, release or activation by itself.
