# Official platform reference excerpts

Collected on 2026-10-25 from the primary documentation linked below. This is a source-excerpt packet for subsequent BMAD planning, not an architecture decision, execution receipt, authorization, or proof of GitHub workflow state. Quotations retain the documented wording; implications for this fork must be assessed against repository source and verified settings.

## GitHub Actions events

Source: <https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows>

### `push`

> Runs your workflow when you push a commit or tag, or when you create a repository from a template. This includes workflows that are not merged into the default branch.

The documented event table identifies `GITHUB_SHA` as the tip commit pushed to the ref and `GITHUB_REF` as the updated ref. The documentation also warns that tag events are not created when more than three tags are pushed at once.

### `schedule`

> This event will only trigger a workflow run if the workflow file exists on the default branch.

> Scheduled workflows will only run on the default branch.

> In a public repository, scheduled workflows are automatically disabled when no repository activity has occurred in 60 days.

> The `schedule` event can be delayed during periods of high loads of GitHub Actions workflow runs. High load times include the start of every hour. If the load is sufficiently high enough, some queued jobs may be dropped.

The event table identifies both the latest commit and the ref as the default branch. A configured schedule alone is not proof that an actual weekly run occurred.

### `workflow_dispatch`

> This event will only trigger a workflow run if the workflow file exists on the default branch.

> To enable a workflow to be triggered manually, you need to configure the `workflow_dispatch` event. On the GitHub UI, the "Run workflow" button will be present if the workflow file exists on the default branch. Once a workflow has run at least once, you can dispatch it against any branch or tag via the GitHub API or GitHub CLI.

### `workflow_run`

> This event will only trigger a workflow run if the workflow file exists on the default branch.

> The workflow started by the `workflow_run` event is able to access secrets and write tokens, even if the previous workflow was not.

The documented event table identifies `GITHUB_SHA` as the last commit on the default branch and `GITHUB_REF` as the default branch. The warning names cache poisoning and unintended access to write privileges or secrets when untrusted code is run.

## Disabling individual workflows

Source: <https://docs.github.com/en/actions/how-tos/manage-workflow-runs/disable-and-enable-workflows>

> Disabling a workflow allows you to stop a workflow from being triggered without having to delete the file from the repo. You can easily re-enable the workflow again on GitHub.

> Disabling a workflow doesn't cancel its existing runs, but you can't re-run those runs or their jobs while the workflow is disabled.

> When a public repository is forked, scheduled workflows are disabled by default.

The documented CLI form is `gh workflow disable WORKFLOW`. The selector can be the workflow name, ID or filename. The REST endpoint is `PUT /repos/{owner}/{repo}/actions/workflows/{workflow_id}/disable`; the documentation permits the workflow filename as `workflow_id`.

REST source: <https://docs.github.com/en/rest/actions/workflows#disable-a-workflow>

These statements establish documented controls, not actual disabled state, branch-specific isolation, cancellation of old runs, or persistence under future upstream file renames. Those remain verification obligations for the activation design and runbook.

## GoReleaser release configuration

Source: <https://goreleaser.com/customization/release/> (retrieved page identifies GoReleaser v2.19 and a last-updated date of 2026-08-30).

> GoReleaser can create a GitHub/GitLab/Gitea release with the current tag, upload all the artifacts and generate the changelog based on the new commits since the previous tag.

The `release.github` configuration names the release repository with `owner` and `name`. Its documented default is extracted from the origin remote URL.

For `prerelease: auto`, the documented explanation is:

> If set to auto, will mark the release as not ready for production in case there is an indicator for this in the tag e.g. v1.0.0-rc1

The documented default of `prerelease` is `false`. No live release flag is established by this excerpt.

> You can specify a file containing your custom release notes, and pass it with the `--release-notes=FILE` flag. GoReleaser will then skip its own release notes generation, using the contents of your file instead.
