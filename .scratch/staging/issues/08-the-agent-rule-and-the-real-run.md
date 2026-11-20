# 08: The agent's rule and the real run

**What to build:** An agent on this machine gives every pull request the choice of staging. In the same question that asks to open a pull request, it offers to open it with staging, without staging, or not at all. With staging, it turns staging on from the main checkout before pushing, and opens the pull request with the label only if that worked. Then it follows the publish and gives the links. It never turns staging off by itself, and asks whether to once the maintainer says the pull request was merged or closed. The effort ends with the whole path tried for real. See the spec, sections The agent and Records.

**Blocked by:** 05, 06, 07.

**Status:** ready-for-agent

- [x] A global rule of Claude on this machine, written for this repository, says what the description above says. It recommends "without staging" for a pull request that changes only documents.
- [x] The rule says that a failure to turn staging on is reported and stops the agent before the push.
- [x] The `README.md` says how to turn staging on and off, what the machine needs (`gh` logged in, `HETZNER_TOKEN` in `.env`) and how to read the password of Mailpit.
- [x] `CODING_STANDARDS.md`, `docs/providers.md` and ADR-0019 match what was built. `docs/providers.md` marks `sslip.io` as provisional and points to the ticket of the domain.
- [x] The permissions of the `gh` token have one home: a row "Token of `gh` on the maintainer's machine" in the table of the GitHub section of `docs/providers.md`. The row says the token is fine-grained, with access to `dannilodms/software-factory-manager` alone and no account permission. It gives each repository permission below with its level, and the expiration date, "none" until the maintainer makes the token again. It never gives the value. Both READMEs point to that row instead of listing permissions: the paragraph of `task staging:on` in Deploy, which lists three since be4939d, and, if it fits, the row of the GitHub CLI in the prerequisites. The list, as the maintainer chose it on 2026-11-19 (relayed by another session of this repository):
  - Metadata: read (required).
  - Contents: read. Compares branches when a pull request is opened, and reads releases. Not write: agents never merge, and tags and releases come from the workflow.
  - Pull requests: read and write. Open and edit pull requests, put the label on and take it off, create the label of the repository and comment on a pull request. Those endpoints accept Issues or Pull requests write (`X-Accepted-GitHub-Permissions: issues=write; pull_requests=write`).
  - Actions: read and write. Runs, logs, rerun, cancel and `gh workflow run`; read also covers reading an environment.
  - Secrets: read and write. The repository secret `RESEND_TEST_API_KEY`.
  - Environments: read and write. The secrets and variables of `staging`, and later of `production`.
  - Administration: read and write. Creating the environments `staging` and `production`. The maintainer accepted the risk that it also allows deleting the repository and changing its rulesets.
  - Left out: Issues (Pull requests covers it), Workflows (git goes over SSH, and a token counts only for git over HTTPS), Variables (those of an environment come with Environments), Webhooks, and Packages (a fine-grained token cannot reach the GHCR; the pipeline uses `GITHUB_TOKEN`).
- [ ] The real run, done with the maintainer and recorded here:
  1. the rule's question is asked;
  2. staging is turned on;
  3. this effort's pull request is opened with the label and published;
  4. a login code is read in Mailpit and the shell opens;
  5. staging is turned off;
  6. nothing is left at the provider, and only the password of Mailpit is left in GitHub.

## Comments

### What was built

- **The rule**, a global rule of Claude, `software-factory-manager-staging.md` in `C:\Users\danil\.claude\rules\`, outside the repository, in the form of the rules there. It names this repository by its path and loads at every launch. Before a push and a pull request, the agent asks one question, through AskUserQuestion and in Portuguese, with three answers and the recommended one first: open it and publish it to staging, open it without staging, or do not open it. With staging, it runs `task staging:on` in the main checkout or a worktree, in the background, since it takes up to ten minutes. When that exits with an error, it reports what the command printed and stops before the push. Otherwise it pushes, opens the pull request with `gh pr create --label staging`, follows the newer run of `ci.yml` with `gh run watch` until `Publish to staging` ends, and gives the links of the comment the job writes, naming `STAGING_MAILPIT_PASSWORD` rather than giving its value. Without staging, it opens the pull request without the label. It runs `task staging:off` only after asking, and asks when the maintainer says the pull request was merged or closed. Commits stay under the rule that forbids them unasked.
- **`docs/providers.md`**:
  - the row "Token of `gh` on the maintainer's machine" in the GitHub section, with the permissions the maintainer chose, the 403 that a missing Environments gives, and no value;
  - a section `sslip.io` and its summary row: provisional, why it goes soon, the host names, and the ticket of the domain, which the row of Domain and DNS points to as well;
  - Domain and DNS without the reserved address; Let's Encrypt with the certificates kept in a volume while the server lives; Backblaze B2 without the bucket of staging certificates;
  - the three packages of the GHCR created public by the first publish, and the state of GitHub;
  - the expected total in US dollars;
  - staging's login providers waiting for the domain, and the LLM stand-in in staging only once a feature calls an LLM.
- **`README.md` and `README.pt-BR.md`**: the overview and the index entry of ADR-0019 describe a server turned on and off by hand; the row of the GitHub CLI in the prerequisites and the paragraph of `task staging:on` in Deploy point to the row of the token instead of listing three permissions; the line on `sslip.io` points to its section.
- **`CODING_STANDARDS.md`**: the joints of the deploy that staging proved, in Deferred; "Staging is offered" in Flow; the packages that came up public, in Images of Publishing; the home of the token's permissions, in On the maintainer's machine.
- **ADR-0019**: the LLM stand-in in staging only once a feature calls an LLM, in the decision and in the table; the staging Compose file builds nothing and runs the images of the commit.
- The two items ticket 01 left for later are closed: the row of `gh` by ticket 02, the rest here.

### Decided while building

- **"With staging" is recommended for every pull request that does not change only documents.** The spec names only the recommendation for documents, and the question puts one answer first.
- **Documents are what the job `changes` of `ci.yml` judges as documents**, so that the recommendation follows the same line as the skipped builds. The rule points to that job instead of copying its list.
- **The label goes in `gh pr create`**, not after it: the concurrency of `ci.yml` counts on it to publish the pull request once (ticket 06).
- **The password of Mailpit is given by its name**, as the comment of the publish gives it (Never printed, in Secrets).
- **Turning off is recommended unless another open pull request has the label**, which `task staging:status` tells: staging then shows that one.
- **The rule has no `paths`**: it governs a push and a pull request, not a kind of file, so it loads at launch, and its first sentence limits it to this repository.

### What was checked

- Items 9, 10 and 13 of the notes of this effort, against the current files: tickets 01 to 07 had fixed `.env.example`, the output of the seed and every line they named in `CODING_STANDARDS.md`. After this change, outside the considered options of ADR-0019, no file gives staging a reserved address, a sweep or a bucket of certificates, and none gives a price in euros.
- Every item under "Found, and left for a decision" in tickets 03 to 06 is still true: no size limit of the JavaScript; `ACME_EMAIL` empty (`variables.go`, `.env.example`); nothing set against Zod's probe of `eval`; no `lb_try_duration` in `deploy/`, while the README still promises it; `environment: staging` on the job of every labelled run; `labeled` among the triggers; the label read again, with no time compared.
- The rule against `writing-claude-rules.md`: frontmatter, English with Portuguese only in the example question, a Reason, numbered steps, a link to `no-unsolicited-commits.md`, under 100 lines.

### Found, and left for a decision

- **The restore test.** Backup and Scheduled workflows in `CODING_STANDARDS.md`, and Restore in both READMEs, still say that a monthly workflow creates a server. ADR-0019 says the pipeline creates no server and the restore test becomes a task of the machine, decided with production, which this spec leaves out. Correct the text now, or with production.
- **A rule is only context.** Nothing stops an agent that skips it from opening a pull request without the question. A hook before `gh pr create` could remind it every time.
