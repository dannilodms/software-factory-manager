# 07: The reminder after 24 hours

**What to build:** Nothing turns staging off by itself, so a scheduled workflow warns the maintainer when staging has been on for more than 24 hours. It reads only the variable holding the time staging was turned on, needs no secret and no credential of the provider, and fails when that time is too old, which is how a scheduled workflow warns. See the spec, section The pipeline.

**Blocked by:** 02.

**Status:** resolved

- [x] A scheduled workflow runs every six hours and on a manual trigger.
- [x] It fails, with a message giving how long staging has been on, when the time variable of `staging` is more than 24 hours old. It passes when the variable is empty or recent.
- [x] It reads no secret, asks for no permission beyond reading, and blocks no pull request.
- [x] actionlint and zizmor pass.
- [x] `CODING_STANDARDS.md` (Scheduled workflows) describes the reminder in place of the sweep.

## Comments

### What was built

- `.github/workflows/staging-reminder.yml`, "Staging reminder": on `schedule` (`23 */6 * * *`) and `workflow_dispatch`, `permissions: {}` for the workflow and its one job, concurrency group `staging-reminder`. The job declares the environment `staging` with `deployment: false` and has one step, which gets `vars.TURNED_ON_AT` through `env:`: empty, it passes with "Staging is off."; 24 hours old or less, it passes giving the hours; older, it fails with an `::error` annotation giving the hours and saying to run `task staging:off`; a value `date` cannot read fails too, without printing it. No checkout, no Task, no action.
- Records: Scheduled workflows in `CODING_STANDARDS.md` (the reminder in place of the sweep); the environment `staging` in the GitHub section of `docs/providers.md`, its summary row and its opening paragraph (the reminder reads it, records no deployment, and may be what creates it); the paragraph on turning staging on and off in `README.md` and `README.pt-BR.md`.

### Decided while building

- **The environment is declared, with `deployment: false`** (rule in Scheduled workflows, `CODING_STANDARDS.md`). A variable of an environment reaches a job only through `vars` in a job that declares it. The job's token cannot read it another way: the permissions a workflow can give its token (actions, artifact-metadata, attestations, checks, code-quality, contents, copilot-requests, deployments, discussions, id-token, issues, packages, pages, pull-requests, security-events, statuses, vulnerability-alerts) have none for environments or variables, so `GET /repos/{owner}/{repo}/environments/staging/variables/TURNED_ON_AT` with it is refused; a token of the maintainer would be a secret; a repository variable would undo the names of ticket 02. What declaring it costs, and the answer:
  - *Deployment records*: by default each run would record a deployment of `staging`, four a day, burying the publishes in the environment's history. `deployment: false` (GitHub, March 2026; actionlint 1.7.12 knows the key and rejects a misspelt one) records none.
  - *The secrets of the environment*: the job could name `DEPLOY_KEY` and `MAILPIT_PASSWORD`. It names no secret, and its token has no permission.
  - *Protection rules*: with `deployment: false` required reviewers and a wait timer still hold the job, and a custom protection rule fails it at once. `staging` has none (ticket 02 creates it so), and the rule now says it keeps none.
  - *A missing environment*: GitHub creates an environment a run refers to when it does not exist, so a run before the first turn-on creates `staging`, empty and with no rule, as `task staging:on` would.
- **The check is in the step**, so the job always runs and passes while staging is off; no job-level `if` depends on when the environment's variables are loaded.
- **More than 24 hours** is more than 86400 seconds; the hours are rounded down. With a run every six hours, the first failure comes between 24 and 30 hours after the turn-on, and one follows every six hours until staging is off. Turning on again keeps the first time (ticket 02), so it does not reset the count.
- **A value that is not a time fails**: passing would silence the reminder. Nothing prints the value (Never printed, in Secrets): the error of `date`, which quotes it, is hidden.
- **The concurrency group is `staging-reminder`, not `staging`.** A concurrency group spans every workflow of the repository, and a newer run waiting in a group replaces the one waiting before it, so a reminder in the publish's group `staging` could replace a pending publish.
- **No Task target**: the value exists only in the pipeline; on the machine `task staging:status` already says since when staging is on. The minute 23 keeps the runs off the top of the hour, when GitHub delays scheduled runs most.

### What was checked

- The step's script, taken from the workflow file by a scratch script and run as GitHub runs a `bash` step (`bash --noprofile --norc -eo pipefail`, GNU `date` as on `ubuntu-24.04`): empty passes with "Staging is off."; 2 and 23 hours old pass giving 2 and 23 hours; 30 and 100 hours old, and `2026-01-01T00:00:00Z` (the form the tool writes), fail with an `::error` giving the hours; `garbage-value` fails without printing it.
- `task workflows:check` with `GH_TOKEN` (zizmor's online audits, as in CI): actionlint, with shellcheck run on the step, and zizmor find nothing. zizmor with `--persona=pedantic` on the new file finds nothing.
- `task versions:check` passes.

### What was not checked

- No run on GitHub: that the job gets `vars.TURNED_ON_AT`, that no deployment is recorded, and that a run creates `staging` when it does not exist. The first scheduled run after the merge shows them.
