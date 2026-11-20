# Providers

Every outside service this system depends on: why it was chosen, what it costs, and what is configured there. The decisions behind them are in ADRs [0019](adr/0019-three-environments-with-staging-on-a-server-created-on-demand.md), [0020](adr/0020-postgresql-backed-up-by-pgbackrest-and-production-files-in-backblaze-b2.md) and [0021](adr/0021-a-public-repository-under-agplv3.md).

**No secret is written here.** No key, token, password or passphrase, and no value that works as one. Addresses, host names, DNS records, regions, plan names and bucket names are fine. Where secrets live is in Secrets, in [`CODING_STANDARDS.md`](../CODING_STANDARDS.md).

**Keep it true.** A change made at a provider (a new record, a new bucket, a change of plan) is written here in the same pull request, or right after when the change is made by hand.

Prices were read on the providers' own pages on 2026-11-16, without tax, and Hetzner's from its API on 2026-11-19. The GitHub repository and the Hetzner project of staging were created by hand; the GitHub Environment and the label of staging were made by the first `task staging:on`, and the packages of the images by the first publish to staging, on 2026-11-20. A value shown as _not created_ is filled in when the resource exists.

## Summary

| Provider | Used for | Environments | Approximate cost | State |
| --- | --- | --- | --- | --- |
| [GitHub](#github) | Repository, pipeline, container images, secrets | all | Free | Repository created and protected; the environment `staging`, the label `staging` and the packages of the three images created; the environment `production` and the secret `RESEND_TEST_API_KEY` not created yet |
| [Hetzner Cloud](#hetzner-cloud) | The servers | staging, production | US$ 0.0114 an hour while staging is on; production _not created_ | Project of staging created; its server exists only while staging is on |
| [Backblaze B2](#backblaze-b2) | Production files, database backups | production | Free under 10 GB, about US$ 0.63 a month at 100 GB | Decided, not created |
| [Domain and DNS](#domain-and-dns) | The addresses of the system | staging, production | _not known_ | Registrar not chosen; [a ticket](../.scratch/domain/issues/01-register-the-domain-and-move-staging-off-sslip-io.md) asks the maintainer to register it |
| [sslip.io](#sslipio) | Host names of staging until the domain exists | staging | Free | Provisional, nothing to create |
| [Let's Encrypt](#lets-encrypt) | HTTPS certificates, through Caddy | staging, production | Free | Nothing to create |
| [Resend](#resend) | Sending e-mail | production | _not checked_ | Decided, not created |
| [Login providers](#login-providers) | Login through Google, Microsoft and GitHub | staging, production | Free | Decided, not created |
| [LLM provider](#llm-provider) | Calls to language models | production | By use | Not chosen |
| [Hosted monitor](#hosted-monitor) | Warning when production or a backup stops | production | Free plan intended | Not chosen |

Expected total while the system is small, from the prices below: about US$ 7.10 a month for the production server and its IPv4 address, about US$ 0.27 for each day staging is on, and nothing for Backblaze B2 under 10 GB, plus the domain and what Resend and the LLM provider charge.

## GitHub

**Why.** The repository is public, and a public repository on the free plan has what the pipeline needs: secrets for each environment, a protected `main` and unlimited minutes of GitHub Actions (ADR-0021).

**Cost.** Free.

| Item | Value |
| --- | --- |
| Account or organisation | `dannilodms`, a personal account |
| Repository | [`dannilodms/software-factory-manager`](https://github.com/dannilodms/software-factory-manager), public |
| Licence | AGPLv3 |
| Branch protection on `main` | The `main` ruleset: no direct push, a pull request with a squash merge, no force push, no deletion, and a merge only when the `Checks` check of GitHub Actions has passed |
| Merge | Squash only; the branch is deleted after the merge |
| Environment `staging` | Created by `task staging:on` when it does not exist, with no protection rule and no branch restriction, so that a run of a pull request can use it, the job `Publish to staging` of `ci.yml`, and so that the scheduled `staging-reminder.yml`, which reads `TURNED_ON_AT` there and records no deployment, neither waits nor fails; an environment that exists is not changed. A run of the reminder creates it the same way, empty, when it does not exist yet. While staging is on it holds the secrets `DEPLOY_KEY` (the private deploy key of the server) and `MAILPIT_PASSWORD` (the password of Mailpit), and the variables `SERVER_ADDRESS` (the IPv4 address of the server), `SERVER_HOST_KEY` (its host key, as a line of known hosts) and `TURNED_ON_AT` (when the server was created). `task staging:off` deletes all but `MAILPIT_PASSWORD`. What each one holds is in Secrets, in [`CODING_STANDARDS.md`](../CODING_STANDARDS.md) |
| Environment `production` | Restricted to `main`. _Not created_ |
| Secret scanning with push protection | On |
| Dependabot alerts | On. Its automatic pull requests are off |
| Private vulnerability reporting | On; the way to report is in [`SECURITY.md`](../SECURITY.md) |
| Actions | An action runs only when pinned to a full commit hash. The token of a workflow is read-only unless the workflow asks for more, and cannot approve a pull request |
| Workflows of outside contributors | Run only after approval, for every outside contributor |
| Tag protection | A ruleset on the tags that start with `v`: no update and no deletion (ADR-0023). _Not created_ |
| Releases | Created by the publish workflow, one for each tag, with the notes from `CHANGELOG.md` and no attached file. _None yet_ |
| Container registry (GHCR) | Public images, tagged with the commit hash: the packages `software-factory-manager-api`, `software-factory-manager-caddy` and `software-factory-manager-seed` of `ghcr.io/dannilodms`, pushed by the job `Publish to staging` of `ci.yml` with the token of the job, and linked to the repository by the label `org.opencontainers.image.source` of each image. The server pulls them without logging in, so each must be public. The first publish to staging created the three on 2026-11-20, public from their first push, with nothing done by hand; a new package that came up private would be made public by hand, once, in its settings on GitHub (Danger Zone, Change visibility). The images of a release also carry its number |
| Label that publishes a pull request to staging | `staging`, color `FBCA04`, described as "Publish this pull request to staging once its checks pass". Created, or given that description and color, by `task staging:on`. Adding it to an open pull request publishes that pull request, and each publish takes it off every other open pull request, with the token of the job |
| Commit address | The maintainer's `noreply` address |
| Repository secret `RESEND_TEST_API_KEY` | The key of Resend's test account that the scheduled workflow `resend.yml` gives the tests that call Resend (see Resend). _Not created_: until it exists those tests skip and the run passes |
| Token of `gh` on the maintainer's machine | The token with which `gh`, logged in as the maintainer, opens pull requests, reads the pipeline and turns staging on and off (the value is a secret). Fine-grained, with access to `dannilodms/software-factory-manager` alone and no account permission. Expiration date: none, until the maintainer makes the token again. Its repository permissions: Metadata, read (required). Contents, read: compares branches when a pull request is opened, and reads releases; not write, because agents never merge, and tags and releases come from the workflow. Pull requests, read and write: opens and edits pull requests, puts the label on and takes it off, creates the label of the repository and comments on a pull request; those endpoints accept Issues or Pull requests write (`X-Accepted-GitHub-Permissions: issues=write; pull_requests=write`). Actions, read and write: runs, logs, rerun, cancel and `gh workflow run`; read also covers reading an environment. Secrets, read and write: the repository secret `RESEND_TEST_API_KEY`. Environments, read and write: the secrets and variables of `staging`, and later of `production`; without it GitHub answers 403 to the first secret, and `task staging:on` destroys the server it created. Administration, read and write: creates the environments `staging` and `production`; the maintainer accepted the risk that it also allows deleting the repository and changing its rulesets. Left out: Issues (Pull requests covers it), Workflows (git goes over SSH, and a token counts only for git over HTTPS), Variables (those of an environment come with Environments), Webhooks, and Packages (a fine-grained token cannot reach the GHCR; the pipeline uses `GITHUB_TOKEN`) |

## Hetzner Cloud

**Why.** The lowest price found for a server of this size that is billed by the hour, which is what lets the staging server exist only while it is used. The tool of the environments reaches its API through hcloud-go, from the maintainer's machine: GitHub Actions holds no credential of Hetzner and knows only a server it reaches over SSH (ADR-0019).

**Cost.** Read from Hetzner's API on 2026-11-19, in US dollars, without VAT; the account is billed in dollars. CX23 (2 vCPU, 4 GB, 40 GB): US$ 0.0104 an hour, up to US$ 6.49 a month. An IPv4 address: US$ 0.001 an hour, up to US$ 0.60 a month. Staging costs only while it is on, about US$ 0.27 a day, and nothing once it is off. Production is priced when its server is created.

**To check before ordering.** A new account has its resources limited in its first month. On 2026-11-19 the CX23 was offered in `fsn1`, `nbg1` and `hel1`; moving staging to another of them is a change to `deploy/server.staging.yaml`.

| Item | Value |
| --- | --- |
| Project of staging | `software-factory-manager-staging`, for staging alone. Its API token, which needs read and write permission, is `HETZNER_TOKEN` in the `.env` of the maintainer's main checkout, and nowhere else |
| Region | `fsn1` (Falkenstein, Germany), a location: Hetzner's API no longer names a datacenter |
| Staging server | `staging`, plan CX23, system image `ubuntu-26.04`, labelled `environment=staging`. Created by `task staging:on` and destroyed by `task staging:off`, from the choices in `deploy/server.staging.yaml`, and prepared by `deploy/cloud-init.yaml`. It exists only while staging is on |
| Address of staging | An ordinary IPv4 address, created with the server and deleted with it, and no IPv6. No reserved address: each turn-on gets a new one |
| SSH key of the project | `staging`, the deploy public key of the server, labelled `environment=staging`, registered with the server so that Hetzner e-mails no root password, and removed with it. A new one each turn-on |
| Firewall | None at Hetzner: the firewall is inside the server, ufw, which lets in 22, 80 and 443 (`deploy/cloud-init.yaml`) |
| SSH | Key login as the deploy user; no password, no root. The host key is the one the tool gave the server |
| Metadata service | `169.254.169.254`, which gives anything on the server its user data, the rendered cloud-init file with the private host key. The server reads it; no container reaches it, by a rule of `deploy/cloud-init.yaml` (Servers in [`CODING_STANDARDS.md`](../CODING_STANDARDS.md)) |
| Production server | Plan CX23; project, name and IP address _not created_ |
| Restore-test server | _Decided with production_: a task of the maintainer's machine (ADR-0019) |

## Backblaze B2

**Why.** The cheapest S3-compatible storage with object versioning: 10 GB free for good, the lowest price for each gigabyte, and free egress up to three times what is stored. It is a different company from the one that hosts the servers, so losing that account does not lose the backups (ADR-0020).

**Cost.** US$ 6.95 for each terabyte a month, with the first 10 GB free. About US$ 0.63 a month for 100 GB stored, 20 GB uploaded and 50 GB downloaded. Requests are free.

**Not proven yet.** The AWS SDK for Go v2 with its default checksum, `CopyObject`, which confirming an upload uses (ADR-0017), an upload in parts with signed URLs, and pgBackRest against it. The work that builds the deploy tries all four, before production uses it. If one fails, the storage moves to Scaleway or OVHcloud.

| Item | Value |
| --- | --- |
| Account region | _not chosen_; fixed for the account when it is created |
| S3 endpoint | _not created_ |
| Bucket of production files | _not created_; private, versioning on, old versions removed after 30 days, cross-origin requests from the app's origin only |
| Bucket of database backups | _not created_; private, written by pgBackRest, 30 days kept |
| Application keys | One for each bucket, limited to it (the values are secrets) |

## Domain and DNS

**Why.** The system needs addresses of its own: production needs one, and so does each application that staging and production register with a login provider. Until the domain is registered, the host names of staging are under `sslip.io` (see below). Registering it is [a ticket for the maintainer](../.scratch/domain/issues/01-register-the-domain-and-move-staging-off-sslip-io.md), which also decides how the host names of staging follow a server whose address changes each time staging is turned on.

**Cost.** _Not known_: the yearly fee of the domain.

| Item | Value |
| --- | --- |
| Registrar | _not chosen_ |
| DNS host | _not chosen_ |
| Domain | _not registered_ |

Records to create:

| Host name | Type | Points to | Serves |
| --- | --- | --- | --- |
| _app, production_ | A | The production server | The app, the API and MCP |
| _app, staging_ | A | The staging server, in the way the ticket of the domain decides | The app, the API and MCP of the pull request |
| _files, staging_ | A | The staging server, the same way | Garage, for uploads and downloads |
| _mail, staging_ | A | The staging server, the same way | Mailpit, behind a password |
| _the records Resend asks for_ | TXT, MX, CNAME | Resend | Proof that the domain may send e-mail |

In production the browser reaches the files at Backblaze's own host name, so there is no record for them.

## sslip.io

**Why.** It answers DNS for an IPv4 address written into a host name, so `app.203-0-113-10.sslip.io` points to `203.0.113.10` with no account and no record to set. Staging has host names, and so HTTPS, from its first turn-on, before the domain exists (ADR-0019).

**Cost.** Free.

**Provisional.** It goes once the domain is registered, which [a ticket for the maintainer](../.scratch/domain/issues/01-register-the-domain-and-move-staging-off-sslip-io.md) asks for, and soon:

- `sslip.io` is not on the Public Suffix List, so Let's Encrypt counts every certificate under it against one limit shared with everyone who uses it, and that limit has run out in some weeks (see Let's Encrypt).
- The host names change each time staging is turned on, so no login provider can register them: until the domain exists, staging logs in with a login code only.

| Item | Value |
| --- | --- |
| Host names of staging | `app`, `files` and `mail`, each followed by the address of the server written with dashes and `.sslip.io` (`app.203-0-113-10.sslip.io`). `task staging:status` prints them |
| Configured there | Nothing: the address is in the name |

## Let's Encrypt

**Why.** Caddy gets and renews the HTTPS certificates by itself, at no cost.

**Cost.** Free.

**Limit that matters.** No more than five certificates for the same host names in a week. Each staging server has host names of its own, so none asks again for a certificate an earlier one got, and Caddy keeps its certificates in a volume of the server while the server lives (ADR-0019). The limit that can still stop staging is the one `sslip.io` shares with all its users (see sslip.io).

Nothing is configured there. The certificates are for the host names of staging under `sslip.io`, and, once the domain is registered, for the ones listed in Domain and DNS.

**ZeroSSL.** Caddy asks ZeroSSL when Let's Encrypt refuses a certificate only when it has an address for the account, `ACME_EMAIL` (`deploy/Caddyfile.staging`). Staging gives none yet, so it asks Let's Encrypt alone.

## Resend

**Why.** It sends the e-mail of production and reports a bounce to the API. Development, tests and staging use Mailpit and send nothing real.

**Cost.** _Not checked._

| Item | Value |
| --- | --- |
| Account | _not created_ |
| Sending domain | _not created_; verified by the DNS records above |
| Sender address | _not chosen_ |
| API key of production | One, which the worker sends with (`RESEND_API_KEY`; the value is a secret) |
| Webhook for bounces | `POST /api/resend/bounces` on the production address, for the event `email.bounced` only. Resend signs it with Svix, and the API checks the signature with the signing secret of the webhook (`RESEND_WEBHOOK_SECRET`; the value is a secret). _Not created_ |
| Test API key | A key of its own, apart from the one of production, which only the scheduled workflow `resend.yml` uses, from the repository secret `RESEND_TEST_API_KEY` (see GitHub); its tests send to Resend's test addresses (`delivered@resend.dev`, `bounced@resend.dev`), which deliver nothing. Which Resend account holds it is decided with the deploy. _Not created_ |
| Used by Grafana for alerts | Not tried (ADR-0012) |

## Login providers

**Why.** People log in with an account they already have, and the system stores no password (ADR-0011).

**Cost.** Free.

Each provider needs two registered applications, because staging and production have different addresses. Staging registers its own once the domain gives it a fixed address; until then it logs in with a login code only (ADR-0019). Development and the pipeline use Dex and register nothing. The redirect address each application registers is the origin of the app followed by the callback of the provider, `<APP_ORIGIN>/api/session/login_providers/<key>/callback`, where the key names the provider in the configuration (such as `google`).

| Provider | Application for | Redirect address |
| --- | --- | --- |
| Google | staging | _not created_ |
| Google | production | _not created_ |
| Microsoft | staging | _not created_ |
| Microsoft | production | _not created_ |
| GitHub | staging | _not created_ |
| GitHub | production | _not created_ |

## LLM provider

**Why.** The backend calls models through one interface in the Chat Completions format, so the provider is configuration: a base address, a key and a model (ADR-0004). Only production calls a real provider. Development and the pipeline use the stand-in written in this repository, and staging does once a feature calls an LLM; until then it declares none.

**Cost.** By use, in production only. Nothing automated calls a real provider anywhere else (ADR-0019).

| Item | Value |
| --- | --- |
| Provider | _not chosen_ |
| Base address | _not chosen_ |
| Default model | _not chosen_ |
| Keys | One, for production (the value is a secret) |

## Hosted monitor

**Why.** Production runs no telemetry yet, so something outside the server has to warn when the system stops answering or a backup does not finish (ADR-0012).

**Cost.** A free plan is intended.

| Item | Value |
| --- | --- |
| Service | _not chosen_ |
| HTTP check | `/up` on the production address |
| Backup signal | Sent by each backup when it ends; a missing one warns |
| Warning | By e-mail, to the maintainer |
