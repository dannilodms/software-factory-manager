# 06: The label follows one pull request

**What to build:** Staging shows one pull request at a time. Adding the `staging` label to a pull request that is already open publishes it. Publishing a pull request takes the label off any other open pull request that has it, with a comment there saying which pull request staging now shows, so that a push to the old one never publishes it back. See the spec, section The pipeline.

**Blocked by:** 05.

**Status:** resolved

- [x] The workflow also runs when a label is added to a pull request. Adding `staging` to an open pull request runs its checks again and publishes it.
- [x] After a publish succeeds, the label comes off every other open pull request that has it, and each of them gets a comment naming the pull request staging now shows.
- [x] A push to a pull request whose label was taken off publishes nothing.
- [x] Adding another label does not publish a pull request without `staging`.
- [x] Tried for real with two pull requests, or recorded under "What was not checked" with the reason.

## Comments

### From ticket 05

- The publish ends in `announce` (`apps/api/tools/environment/publish.go`), which comments on the published pull request through the GitHub interface once `/up` answered. Taking the label off the other open pull requests, with a comment there, fits right after it: `LabelledPullRequests` exists, and the interface needs a way to remove a label (`gh pr edit <n> --remove-label staging`, or `DELETE /repos/{owner}/{repo}/issues/{n}/labels/staging` through `gh api`), with its fake in `githubtest`. The job already has `pull-requests: write`.
- The job `staging` of `ci.yml` runs only when `github.event.pull_request.labels` has `staging`, which on a `labeled` event includes the label just added. Its first step skips the rest when staging is off.
- The workflow's `concurrency` no longer cancels the run before when the pull request has the label. With `labeled` among the triggers, adding any label to a pull request without `staging` would still cancel its checks in flight, and adding `staging` starts a run that waits for the one in flight.
- Until `labeled` is a trigger, a labelled pull request is published by a push to it, or by closing and reopening it.

### What was built

- **The trigger.** `ci.yml` runs on `pull_request` of the types `opened`, `synchronize`, `reopened` and `labeled`. Adding any label runs every check again.
- **The concurrency groups.** `ci-<ref>` for the runs of a pull request without the label, which cancel the run before them; `ci-<ref>-staging` for the runs with it, which wait for each other, except that the run of adding `staging` cancels the one in flight there; `ci-<ref>-another-label-<run id>` for a run started by adding another label, a group of its own, so that it cancels no checks in flight and takes the place of no publish waiting.
- **The job `staging`** also needs the event not to be the adding of another label, so that adding one to a pull request that has `staging` publishes it again for nothing. Its notice when staging is off says to push again or add the label again.
- **The publish reads the label again first** (`stillLabelled` in `publish.go`): a pull request that is closed, or no longer has the label, when its publish starts is not published; the command says so and passes.
- **The label comes off the others.** Once `/up` answers, `announce` prints the addresses, takes the label off every other open pull request that has it, each with the comment "Staging shows pull request #N now, so the `staging` label came off this pull request, and a push to it publishes nothing. Add the label again to publish it.", then comments the addresses on the published one. It tries each of these, and fails afterwards with every failure on one line.
- **The GitHub interface** gained `RemoveLabel`: in the `gh` adapter, `gh api --method DELETE repos/{owner}/{repo}/issues/<n>/labels/<label>`; in `githubtest`, with `LabelsOf`, `Close`, `FailCommentingOn` and `FailRemovingALabelFrom`. `LabelledPullRequests` now reads `repos/{owner}/{repo}/pulls?state=open` page after page and keeps the pull requests with the label, regardless of case.
- **Records**: Publishing (Staging, The publish of staging, Never cancelled) in `CODING_STANDARDS.md`; Deploy in `README.md` and `README.pt-BR.md`; the label's row in `docs/providers.md`; the usage of the tool.

### Decided while building

- **The label is read again when the publish starts.** The run of a push made before the label came off, by hand or by another publish, may reach its publish after, since the checks of two pull requests run side by side and the publishes wait in turn. Without the check it would publish its pull request back over the new one and take the label off it. The cost is a race the other way: a label added to a pull request while another one's run is under way comes off when that run publishes, and its own run then publishes nothing; the comment there says to add the label again. In The publish of staging.
- **The list of open pull requests is GitHub's own, not its search.** `gh pr list --label` asks the search (gh's `shouldUseSearch` is true when a label is given), which can still list a label taken off a moment ago, exactly when the next publish, which waited for the one before, reads it. The REST list of pull requests needs only reading pull requests, and its `--jq` writes one object a line (tried read-only against the repository).
- **The label comes off through the REST API of issues**, which GitHub's table of the permissions of GitHub Apps lists under writing pull requests. `gh pr edit --remove-label` goes through GraphQL, whose permission that table does not give. The comments stay on `gh pr comment`, as ticket 05 wrote them, which its real run proved with `pull-requests: write`. The permission lives in one place, the `permissions` of the job, whose comment names `issues: write` as the change should GitHub refuse the label.
- **Order and failures**: the label comes off the others before the comment on the published pull request, in the spec's order, and each is tried, so that one refusal of GitHub neither leaves the others undone nor hides what else it refuses; the job then fails.
- **Adding another label runs every check again**, rather than skipping them: a job skipped by its condition counts as passing for a required check, and a `Checks` skipped on the same commit could cover a failure of the run before.
- **Adding `staging` publishes once.** The agent opens a pull request with `gh pr create --label staging`. gh adds the label with a second call just after it creates the pull request, and the real run of ticket 05 showed the run of `opened` already carrying the label; `labeled` follows. Had the second run waited for the first, the pull request would have been published twice, the seed's data starting again some twenty minutes into the trial. So the run of adding `staging` cancels the run with the label in flight in its group, and goes beside the run without it, which never publishes. The one publish this can cut short is one whose label was taken off by hand and added again while it published, and the new run publishes again. In Never cancelled.
- **The gap ticket 05 left** (removing the label by hand and pushing while that pull request publishes cancels the publish) is closed: the push runs in the group without the label, and the run with it goes on; if its publish has not started, it publishes nothing.
- **The images are built before the label is read again**: a run whose pull request lost the label meanwhile still pushes the images of its commit, a few minutes for nothing, rather than reading the label in a step of the workflow that no test covers.

### What was checked

- Test-first, at the seam of the publish command with the fakes: the label taken off every other open pull request that has it and no other label, a comment on each and none on a pull request without it, the published one keeping its label; what it prints; a refusal to take a label off and a refusal to comment, both tried, together on one line, with the addresses still commented; a pull request that lost the label, or was closed, not published, with nothing done on the server and no label taken off; a list that cannot be read stopping the publish before it logs in; the comment of ticket 05 failing alone. At the seam of the `gh` adapter, against the stand-in: the call that takes a label off and its failure, and the list of open pull requests (with the label in another case, none, and an answer that is not JSON).
- `go test -count=1 ./tools/environment/...`, `golangci-lint run ./tools/environment/...`, `go run ./tools/guard code`, `go vet` with `GOOS=linux`, `task versions:check`, and `task workflows:check` with `GH_TOKEN`, so with zizmor's online audits, on the final `ci.yml`: no finding.
- `task check:quick` once: every step passed but `api:test`, where `TestTheProbeOfReadinessFailsWhileAMigrationIsPending` and `TestTheWorkerStopsAtOnceWhileAMigrationIsPending` failed in their cleanup (`DROP DATABASE … WITH (FORCE)` out of time) under the load of the whole suite, as in tickets 03 and 05, in code this ticket does not touch; run again alone, both passed. The concurrency of `ci.yml` was changed after that run, and checked again with `task workflows:check`.
- The behaviour of the workflow itself, its trigger, its groups and the condition of the job, needs GitHub: the steps below try it.
- For real, on 2026-11-20, by the orchestrator, with staging on, #10 and a throwaway #11 (`try/staging-label`, based on `feat/staging`, closed unmerged afterwards, branch deleted), following the steps below:
  1. **A push re-publishes #10.** The push of 92bf151 (this ticket merged) ran 44665621551, whose publish succeeded on #10. It had waited behind run 44664171333, whose publish had started before it.
  2. **Opening #11 publishes it once.** `gh pr create --label staging` opened #11. The run of `opened` (44666542623) was cancelled, and the run of `labeled` (44666543455) ran. The checks of a change to documents only passed with the builds skipped, and the publish succeeded. It printed "Staging shows pull request #11", "Took the staging label off pull request #10, with a comment there" and "Commented the addresses on pull request #11". #10 was left with no label and the comment "Staging shows pull request #11 now…". `pull-requests: write` was enough to take the label off: no `issues: write`.
  3. **Another label publishes nothing and cancels nothing.** Adding `documentation` to #11, which still had `staging`, ran 44667137818: `Checks` succeeded, "Publish to staging" was skipped, and no other run was cancelled.
  4. **Adding `staging` publishes again and moves the label.** Adding `staging` to #10, 28 seconds later, ran 44667179671 beside step 3's run. Its checks and its publish succeeded. #11 lost `staging`, with the comment "Staging shows pull request #10 now…", and #10 got the comment with the addresses.
  5. **Another label on a pull request without `staging`.** Taking `documentation` off #11 and adding it again ran 44668263305: success, publish skipped.
  6. **A push to a pull request whose label was taken off.** An empty commit pushed to #11 ran 44668393817: success, publish skipped. #10 kept `staging` throughout.

### Found, and left for a decision

- **Adding any label runs every check again**, some fifteen minutes of runners, free for a public repository. If labels other than `staging` come into use, a cheaper way needs those runs to report no `Checks` at all.
- **A label added while another pull request's run is under way can be taken off by it**, as above. Comparing when each label was added would settle it, through the events of each pull request, at the price of more calls and a permission more.

### How to try it for real (the last criterion)

With this ticket on `feat/staging`, pushed, staging on and pull request #10 open with `staging` (B is the second pull request; `gh run list --workflow ci.yml --branch <branch>` shows the runs):

1. The push publishes #10 again.
2. `git switch -c try/staging-label feat/staging`, change one line of a document, push, and `gh pr create --base feat/staging --label staging --title "Try the staging label" --body "Throwaway."`. The run of `opened` is cancelled by the run of `labeled`, and B is published once. `gh pr view 10 --json labels,comments` shows #10 without the label, with the comment naming B. If the job fails with "Resource not accessible by integration" on the label, add `issues: write` to the job's permissions and record it.
3. `gh pr edit <B> --add-label documentation`: a run of its own runs the checks, cancels nothing, and skips "Publish to staging".
4. `gh pr edit 10 --add-label staging`: #10's checks run again and it is published; B loses the label, with a comment naming #10.
5. `gh pr edit <B> --remove-label documentation`, then `--add-label documentation` again: B, without `staging`, publishes nothing.
6. `git commit --allow-empty -m "Try a push"` on B's branch and push: its run skips "Publish to staging". Then `gh pr close <B> --delete-branch`.
