---
status: accepted
---

# Three environments, with staging on a server created on demand

The system runs in three environments, each with its own Compose file: development on the maintainer's machine, staging on a server that exists only while a pull request is being tried, and production on one server that is always on. Both servers are virtual machines prepared by the same cloud-init file. The maintainer's machine creates and destroys them, through a tool of this repository, and GitHub Actions only publishes to them over SSH: the pipeline never talks to the provider. Hetzner Cloud hosts them today, and the lowest price decided the provider and the region.

Revised on 2026-11-19, before any server existed: the servers moved from the pipeline to the maintainer's machine, staging became turned on and off by hand, and its address became provisional.

- **Development runs the tools on the machine.** Go, Vite+ and Task run on Windows, so an agent runs a test without going through Docker. The development Compose file declares every service the code talks to.
- **Staging shows one pull request.** A pull request with the `staging` label is published to staging once its checks pass, and every later push to it is published again. There is one staging environment: publishing another pull request replaces what was there and takes the label off the one before.
- **Staging data is the seed.** Every publish to staging recreates the database and the file storage, empties Mailpit and runs the seed. Nothing from production is ever copied there.
- **Staging is turned on and off by hand.** Turning it on creates its server and turning it off destroys it, both from the maintainer's machine. An agent that opens a pull request for staging asks first, turns staging on, and opens the pull request only once staging is on. Nothing destroys the server by itself: a scheduled check warns when staging has been on for more than 24 hours.
- **Staging is public, with the real login.** A person logs in through Google, Microsoft or GitHub, or with a login code. The e-mail goes to Mailpit, which has a host name of its own behind a password asked by the browser. The seed creates its users with addresses derived from one mailbox taken from configuration, so the maintainer can log in as any of them.
- **Production receives `main`.** A merge into `main` is published to production once the pipeline passes. Nothing else publishes there.
- **One cloud-init file prepares every server**: Docker, the deploy user, the firewall inside the server and automatic security updates. The tool hands it to the provider when it creates the server. Each staging server is a rehearsal of rebuilding production from nothing.
- **The provider is an adapter.** The tool reaches the provider through one interface: create a server, find it, destroy it. Changing provider means writing another adapter and changing the choices of each environment (region, size, system image), which are a committed file.
- **Staging's address is provisional.** Until the domain is registered, the host names of staging are under `sslip.io`, derived from the IP address of the server, which changes every time staging is turned on. They are to be replaced by the domain soon.
- **Publishing is one command.** GitHub Actions builds the images, tags them with the commit, pushes them to the GitHub container registry, and runs the publish command of the same tool, which logs in to the server over SSH, writes the variables file, pulls the images and restarts the Compose services. The Compose files stay the description of the deploy.
- **Secrets live in GitHub Environments**, `staging` and `production`. A value can be written and never read back. The secrets of `production` can be used only by a run of `main`. The publish writes the variables file on the server, after checking that every name in `.env.example` has a value. Staging's data starts again on every publish, so the publish draws its secrets anew each time; the `staging` environment keeps only what reaches the server and Mailpit, written by the tool when staging is turned on.
- **The seed is its own command, in its own image.** It creates data by calling use cases, so seeded data obeys the domain rules. The production Compose file does not declare it, the same way it does not declare the local login provider (ADR-0011).
- **A stand-in answers for the LLM provider outside production.** It is a small server written in this repository that answers in the Chat Completions format (ADR-0004) with fixed answers, streamed ones included, and lists models for the health check. Development and the pipeline use it, and staging does once a feature calls an LLM: until then the staging Compose file declares none. The production Compose file does not declare it.

What each environment runs:

| Service | Development | Staging | Production |
| --- | --- | --- | --- |
| API, worker, `migrate` | run by Task, on the machine | the image of the pull request | the image of `main` |
| Caddy, with the web app | container, the web app served by Vite | container, the web app built into its image | container, the web app built into its image |
| PostgreSQL | container | container, recreated on every publish | container, backed up (ADR-0020) |
| File storage | Garage | Garage, recreated on every publish | Backblaze B2 (ADR-0020) |
| E-mail | Mailpit | Mailpit, behind a password, emptied on every publish | Resend |
| Login | Dex, or a login code read from Mailpit | the three providers, or a login code read from Mailpit | the three providers, or a login code sent by Resend |
| LLM provider | the stand-in | the stand-in, once a feature calls an LLM | the real provider |
| Telemetry | a Compose profile, off by default | none | none yet (ADR-0012) |
| Seed | yes | on every publish | never |

## Considered Options

- **Staging as a gate before production.** `main` would go to staging by itself and to production by a manual promotion. The maintainer wants a merge into `main` to reach production.
- **One environment for each pull request.** More isolation, with a wildcard DNS record, a cleanup routine and more memory to pay for.
- **Staging on the maintainer's machine only.** Nothing remote besides production, and nothing to show from another device.
- **A second server that is always on.** Simpler than creating one each time, for a monthly fee paid while nothing is being looked at.
- **The pipeline creating and destroying the staging server.** The first version of this decision: the label created the server, removing it or closing the pull request destroyed it, and a scheduled sweep destroyed one older than 24 hours. Nothing was left on by mistake, but the token of the provider lived in GitHub and the pipeline was tied to one provider. The maintainer wants the pipeline to know only a server it reaches over SSH.
- **Staging published when a pull request is approved.** GitHub does not let the author approve their own pull request, and the agents open them with the maintainer's account, so the approval never comes.
- **Staging on the production server.** No cost at all, but a branch that nobody has reviewed would run beside the data and the secrets of the clients.
- **Staging closed to the internet, on a private network.** Nothing to protect, but it needs one more account and it shuts out anyone the maintainer wants to show a branch to.
- **Staging behind a browser password on the whole site.** The password travels in the same header as the access token of an agent, so the API and MCP would stop working for agents.
- **The local login provider in staging.** A simple way in for automated tests, but anyone who found the address could log in as any seeded user.
- **Resend in staging.** The login code would reach a real mailbox, with a second key to keep and real e-mail leaving a test environment.
- **A reserved IP address for staging, with DNS records set once by hand.** The first version of this decision, with Caddy's certificate data kept in a bucket between one server and the next. It needs the domain, which is not registered, and the address is paid for while staging is off. With a new address on each server, the host names are new too, and no certificate is asked for twice.
- **Staging reached by its IP address, over HTTP.** No host name to depend on, but the session cookie and the Mailpit password would cross the internet unencrypted.
- **A server reached only over IPv6.** The address is free, but the runners of GitHub Actions have no IPv6 to log in with, the GitHub container registry does not answer over it, and a visitor on a network without IPv6 could not open staging.
- **The provider's command-line tool in a shell script.** Shorter to write, but it does not run the same on every system, needs one more tool installed on the machine and cannot be tested against a fake provider.
- **The publish as a shell script run on the server.** Simpler to read, but checked only by a real publish. In the tool, a fake server proves the order of the steps, that a missing name stops it before anything restarts, and that no secret is printed.
- **The provider's token only in GitHub, or typed on each use.** An agent turns staging on by itself before it opens a pull request, so the token has to be on the machine.
- **A real LLM provider outside production, with a spending limit on each key.** LLM features could be tried before a merge. The maintainer chose to spend nothing outside production.
- **Scheduled tests of the LLM adapter against a real provider.** A change at the provider would show before production met it, for a second key and a little spent every day.
- **A small local model as the stand-in.** Real answers at no cost per call, but it takes gigabytes of memory, its answers change from one run to the next, and it does not fit the staging server.
- **A server in São Paulo or in the United States.** Lower latency for people in Brazil, at a higher price.
- **Kamal.** Deploys with no interruption through a proxy of its own, which competes with Caddy, and the Compose files would no longer describe production.
- **Coolify or Dokploy.** A panel on the server that pulls and publishes. It is one more system to operate, and part of the configuration would live in the panel.
- **The server pulling new images by itself.** No SSH key in GitHub, but nothing orders the dump, the migration and the restart.
- **SOPS and age in the repository.** A change of secret is a commit, but the private key on the maintainer's machine opens the production secrets to any agent running there.
- **A variables file placed on the server by hand.** Nothing in GitHub but the SSH key. Every new variable becomes a manual step that, when forgotten, stops the API at the start.
- **A hosted secret manager.** A panel and a history of changes, with one more provider.
- **OpenTofu or Terraform.** Servers, firewall and DNS declared in code, with a state file to keep and one more tool. The tool of this repository keeps no state: it finds each server by its name and labels at the provider.
- **Production set up by hand.** Rebuilding it would depend on a written procedure being up to date.
- **A Dev Container, or every service of development in Compose.** The same Linux as the pipeline. The first means setting up the agent's own configuration again inside the container. The second makes an agent run every test through `docker compose exec`.
- **The seed as a command of the API binary, as SQL files, or as an anonymised copy of production.** The first puts the command in production. The second goes around the domain rules. The third needs an anonymisation routine and carries client data out of production.
- **DNS updated by the pipeline.** No reserved address to pay for, but the DNS would have to be at a provider with an API, with one more secret.
- **SSH through a private network, or a key that can only start the publish script.** Either would limit what a leaked key can do. The first is one more account, the second a more complex script.

## Consequences

- The servers are in Europe. Every request from Brazil crosses the ocean, which was accepted for the price.
- A staging server left on keeps costing, about US$ 0.27 a day with its IPv4 address, and nothing destroys it by itself, because the pipeline cannot reach the provider. The scheduled check only warns.
- The token of the provider's staging project is in the `.env` of the maintainer's machine, where any agent running there can read it. It reaches only the staging project, which holds no client data. Production gets a project and a token of its own.
- Port 22 is open on both servers, with key login only, no password and no root. Whoever holds the deploy key controls the server. Staging gets a new deploy key every time it is turned on.
- A secret cannot be read back from GitHub. The two that cannot be made again, the key that encrypts credential values (ADR-0018) and the passphrase of the backups (ADR-0020), are also kept in the maintainer's password manager.
- A new configuration variable needs its value set in production before the merge. When one is missing, the publish stops before it restarts anything. Staging draws its values on each publish.
- `sslip.io` is not on the Public Suffix List, so Let's Encrypt counts every certificate under it against one limit shared with everyone who uses it, and that limit has run out in some weeks. Caddy then tries ZeroSSL, but only when it has an address for the account at the certificate authorities, which staging does not give yet (`ACME_EMAIL`). This is one more reason to register the domain.
- Staging needs its own application registered with each of the three login providers, and a fixed address to register, which only the domain gives. Until then, the login in staging is by login code only.
- An automated test cannot get past a real login provider, so nothing automated logs in to staging. The end-to-end tests run in the pipeline, against the development Compose file.
- An LLM feature meets a real provider only in production. Nothing automated calls one anywhere else, so the tests of the LLM adapter run against the stand-in, and a change at the provider shows first in production. The one exception is the maintainer pointing the development configuration at a real provider, by hand, to work on a prompt.
- The three Compose files extend one base file, so the version of each container image keeps a single home. Tried with development, which extends the base for PostgreSQL, Garage, the step that sets the rule of the bucket, and Caddy. It holds, with two things the base cannot hold: `extends` copies a service and nothing else, so the volumes and the configs a service names are declared in the file that extends it; and an image built in this repository, Caddy's, has its versions in its Dockerfile, and the file that builds it names the result with `build` and `image` together. The staging Compose file builds nothing: it runs the images the pipeline pushed, named by the commit.
- The monthly restore test of ADR-0020 was a workflow that created a server. With servers created only from the maintainer's machine, it has to become a task run there; this is decided with production.
- Going back to an earlier version is a manual workflow that publishes an earlier commit of `main`. Going back in the data is a restore (ADR-0020).
- Earlier documents call this environment staging. The label on a pull request and the GitHub Environment use the same name.
