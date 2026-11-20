# Staging, turned on from the maintainer's machine

Status: ready-for-agent

The decisions below were taken in the grilling of 2026-11-19 and stand on ADR-0019 (revised that day), ADR-0011 (login), ADR-0017 (files), ADR-0021 (public repository) and the sections Environments, Pipeline, Delivery and Versions of `CODING_STANDARDS.md`. The terms are in `GLOSSARY.md`: staging, publish, seed, login code, local login provider. Do not reopen them.

## Problem Statement

The maintainer can try a change only on their own machine. Nothing of the deploy exists yet: there is no image of the API, the web app is never built for a server, there is no Compose file for a server, no cloud-init file and no publish. So a pull request cannot be seen working on a real server, with HTTPS, with its e-mail caught by Mailpit and with the seed's data, before it is merged, and it cannot be shown from another device. An agent that opens a pull request has nothing to offer for trying it either.

The first plan had the pipeline create and destroy the staging server. The maintainer does not want GitHub Actions to hold a credential of the provider or to depend on one provider: the pipeline should know only a server it reaches over SSH.

## Solution

From the maintainer's machine, one command turns staging on: a tool of this repository creates a server at the provider with the cloud-init file, waits until it is ready, and gives GitHub only what reaches that server, its address, its host key and an SSH key, plus the password of Mailpit. Another command turns it off, destroying the server and taking those values back out of GitHub. A third one tells whether staging is on, since when, at which address and with which pull request.

A pull request with the `staging` label is published to staging once its checks pass. The pipeline builds the images of the commit, pushes them to the GitHub container registry and runs the publish command of the same tool, which logs in to the server over SSH, starts the data again, applies the migrations, runs the seed and starts the API and the worker. A comment on the pull request gives the address of the app and of Mailpit. The maintainer opens the app, asks for a login code for a user of the seed, reads it in Mailpit and logs in.

An agent that is about to open a pull request asks the maintainer whether it goes to staging. When it does, the agent turns staging on first and opens the pull request, with the label, only once staging is on. Nothing turns staging off by itself: the agent asks once the pull request is merged or closed, and a scheduled check warns the maintainer when staging has been on for more than 24 hours.

The provider is an adapter. Hetzner Cloud is the only one today. Moving to another machine needs the repository, Go, Task, `gh` and the provider's token; moving to another provider needs another adapter and new choices in the committed file of the environment. The pipeline does not change in either case.

Until the domain is registered, the host names of staging are under `sslip.io`, derived from the address of each server. That is provisional and recorded as such.

## User Stories

### Turning staging on and off

1. As the maintainer, I want to turn staging on with one command from my machine, so that I have a server to try a pull request on.
2. As the maintainer, I want turning staging on to wait until the server is ready to receive a publish, so that the command finishing means staging can be used.
3. As the maintainer, I want turning staging on when it is already on to change nothing and tell me so, so that running it twice never creates a second server or a second bill.
4. As the maintainer, I want a server that fails to come up to be destroyed, and what was written to GitHub removed, before the command reports the failure, so that a failed attempt costs nothing afterwards.
5. As the maintainer, I want to turn staging off with one command, so that it stops costing once I am done.
6. As the maintainer, I want turning staging off when it is already off to succeed and say so, so that I can run it without checking first.
7. As the maintainer, I want to ask whether staging is on, since when, at which address and with which pull request, so that I know what I am paying for and what it shows.
8. As the maintainer, I want the commands to print no secret, so that their output can be pasted or logged.
9. As the maintainer, I want the token of the provider read from my `.env`, also when the command runs from a worktree, so that an agent working in one can turn staging on.
10. As the maintainer, I want the token never sent to GitHub, so that the pipeline cannot reach the provider.
11. As the maintainer, I want the choices of the server (provider, region, size, system image) in a committed file, so that the same staging comes up from any machine.
12. As a future maintainer on another provider, I want to change only that file and add an adapter, so that the pipeline, the cloud-init file and the publish stay as they are.
13. As a future maintainer on another machine, I want the commands to run on Windows, Linux and macOS with no tool beyond Go, Task and `gh`, so that moving machines is cloning the repository and filling `.env`.

### The server

14. As the maintainer, I want every server prepared by the one cloud-init file, with Docker, the deploy user, a firewall inside the server and automatic security updates, so that staging rehearses rebuilding production.
15. As the maintainer, I want only ports 22, 80 and 443 open, so that nothing else of the server is reachable.
16. As the maintainer, I want SSH to accept only the deploy user's key, with no password and no root, so that only the holder of the key gets in.
17. As the maintainer, I want a new deploy key each time staging is turned on, so that a key from a destroyed server opens nothing.
18. As the maintainer, I want the host key of the server known before the first connection, so that neither the pipeline nor I trust whatever answers at that address.
19. As the maintainer, I want a copy of the deploy key kept on my machine while staging is on, outside git, so that an agent or I can log in to the server to investigate.
20. As the maintainer, I want the provider to send me no root password by e-mail, so that no other way into the server exists.
21. As the maintainer, I want the server's address to go away with the server, so that nothing is paid for while staging is off.

### Publishing a pull request

22. As the maintainer, I want a pull request with the `staging` label published to staging once its checks pass, so that code that fails its tests never reaches staging.
23. As the maintainer, I want every later push to that pull request published again, so that staging follows the branch.
24. As the maintainer, I want adding the label to an open pull request to publish it, so that I can decide after the pull request exists.
25. As the maintainer, I want publishing one pull request to take the label off the one staging showed before, with a comment there, so that a push to the old one does not publish it back over the new one.
26. As the maintainer, I want a pull request with the label to pass its checks when staging is off, with a notice that nothing was published, so that a forgotten server is never the price of a green check.
27. As the maintainer, I want a pull request from a fork never published, so that code nobody reviewed never gets the secrets of staging.
28. As the maintainer, I want two publishes never to run at the same time, so that one does not tear down what the other is starting.
29. As the maintainer, I want every publish to start the database and the file storage again and run the seed, so that staging always opens on the seed's data and nothing of an earlier pull request is left.
30. As the maintainer, I want the publish to stop before it changes anything on the server when a variable of `.env.example` has no value decided for staging, so that a new variable never half-breaks staging.
31. As the maintainer, I want the secrets of the app drawn anew on each publish and written only to the server, so that GitHub keeps nothing that lasts beyond the data it protects.
32. As the maintainer, I want the certificates of the server kept from one publish to the next, so that publishing often does not exhaust the limit of the certificate authority.
33. As the maintainer, I want the pull request to get a comment with the address of the app and of Mailpit, so that I open it without looking anything up.
34. As the maintainer, I want the publish to print no secret, no variables file and no configuration of a container, so that the public log of the run reveals nothing.

### Using staging

35. As the maintainer, I want the app served over HTTPS at a host name of its own, so that the session cookie is protected as in production.
36. As the maintainer, I want the file storage at a host name of its own, so that uploads and images work the way they will in production.
37. As the maintainer, I want Mailpit at a host name of its own behind a password, so that the login codes are read by me and not by whoever finds the address.
38. As the maintainer, I want the password of Mailpit chosen once and kept in my `.env`, so that I can look it up after GitHub stops giving it back.
39. As the maintainer, I want to log in as any user of the seed with a login code read from Mailpit, so that I try the pull request as each role.
40. As the maintainer, I want no local login provider in staging, so that nobody who finds the address logs in as a user of the seed.
41. As the maintainer, I want the rules of delivery (compression, cache headers, security headers, the rate limit on the login code and the setup code, the masking of addresses in the logs) to be the same in staging as in development, so that staging shows how they behave on a server.
42. As someone the maintainer shows staging to, I want to open it from any network, IPv4 only included, so that I can see the pull request.

### The agent

43. As the maintainer, I want an agent that is about to open a pull request to ask me, in the same question that asks to open it, whether it goes to staging, so that every pull request makes that choice.
44. As the maintainer, I want the agent, when I say it goes to staging, to turn staging on before pushing and to open the pull request with the label only if that worked, so that a labelled pull request always has a staging to go to.
45. As the maintainer, I want the agent to report the failure and stop before the push when staging cannot be turned on, so that nothing half-done is left on GitHub.
46. As the maintainer, I want the agent never to turn staging off by itself, and to ask whether to do it when I tell it the pull request was merged or closed, so that I decide when staging ends.
47. As the maintainer, I want the agent to follow the publish and give me the links once it is done, so that I know when to look.

### Not forgetting it

48. As the maintainer, I want a scheduled check that fails, and so warns me, when staging has been on for more than 24 hours, so that a forgotten server does not cost for weeks.
49. As the maintainer, I want that check to need no credential of the provider, so that the pipeline still cannot reach it.

### Records

50. As the maintainer, I want `docs/providers.md` to say what exists at the provider and in GitHub, with the prices read from the provider, so that I know what I pay for.
51. As the maintainer, I want `sslip.io` recorded as provisional, with the reason to leave it soon, and a ticket that asks me to register the domain, so that it does not stay for good.
52. As the maintainer, I want the `README.md` to say how to turn staging on and off and what it needs on the machine, so that I can do it without an agent.

## Implementation Decisions

### The tool

- **A Go command of its own**, in the Go module of the API, among the tools of the repository. It is not built into any image. It has four commands: turn on, turn off, status and publish, each taking the name of the environment; only staging exists. Task runs the first three as `staging:on`, `staging:off` and `staging:status`. The pipeline runs publish.
- **Choices of an environment**: a committed file for each environment, read by the tool: the provider, the region, the size, the system image and the name of the server. Staging: Hetzner, `fsn1`, `cx23` and the newest Ubuntu LTS that Docker supports officially, checked when the file is written.
- **The provider is an interface** with three operations: create a server from a specification, find one by name, destroy one by name. The specification holds the name, size, region, system image, cloud-init content, deploy public key and labels. A server found or created gives its name, IPv4 address and creation time. The Hetzner adapter uses `hcloud-go`. It registers the deploy public key as a key of the project, so that Hetzner sends no root password, and removes it when the server is destroyed. It asks for an ordinary IPv4 address that is deleted with the server. It labels what it creates with the environment, and finds things by name and label. It creates no firewall at Hetzner: the firewall is inside the server.
- **GitHub is an interface** too: make sure the environment `staging` and the label `staging` exist, set and delete a secret or a variable of the environment, and find the open pull request with the label. The real adapter calls `gh`, logged in on the machine as the maintainer. A secret value goes in through standard input, never as an argument.
- **Readiness is an interface**: wait until the server answers SSH with the expected host key and cloud-init reports that it is done, with a deadline of about ten minutes.
- **Turning on**:
  1. Find the server by name. When it exists, check that it answers, write its values to GitHub again, and report.
  2. When it does not exist, generate a deploy key pair and a host key pair, render the cloud-init file with both public keys and the private host key, create the server, wait for readiness, keep a copy of the deploy private key in a git-ignored folder of the main checkout, and write the values to GitHub.
  3. On a failure after the server was created, destroy it, delete what was written to GitHub and the local copy, and exit with an error.
- **Turning off**: destroy the server and its key at the provider, delete the address, the host key, the deploy key and the time staging was turned on from GitHub, and remove the local copy. Off when already off succeeds.
- **Status**: on or off, the address and the host names, since when, and the pull request with the label.
- **Token**: `HETZNER_TOKEN`, which Task reads from `.env`. When the tool runs in a worktree it reads the `.env` of the main checkout. The name is not in `.env.example`, because the publish requires a staging value for every name listed there and GitHub never gets this one. Another provider's adapter reads a variable of its own.
- **Password of Mailpit**: generated by the tool the first time staging is turned on, kept in the `.env` of the main checkout under a name of its own that is not in `.env.example`, and written to GitHub every time staging is turned on.
- **What the `staging` environment of GitHub holds**: as secrets, the deploy private key and the password of Mailpit; as variables, the server's address, its host key as a line of known hosts, and the time staging was turned on. Turning off removes all but the password of Mailpit.

### The server

- **cloud-init**: Docker from Docker's own repository, at the version the file names; the deploy user, in Docker's group, with the deploy public key; the given host key; SSH by key only, no password, no root; a firewall that lets in 22, 80 and 443 only; automatic security updates; and a clear signal that it has finished.
- **Host names**: `app`, `files` and `mail`, each under the address of the server written in `sslip.io`'s form. The origin of the app is `https://` plus the app's host name.

### Images

- Three images for each commit, tagged with its hash and public in the GitHub container registry: the API, whose binary carries its commands (serve, migrate, worker); Caddy with the built web app; and the seed, which exists.
- The web app is built by the pipeline with the same command the checks use, and its output is copied into the Caddy image, so that Node keeps its one home in `apps/web/package.json`.
- The checks of a pull request build the image of the API and the Caddy image with the web app and publish nothing, as they already do for the seed.

### The staging Compose file

- It extends the base file and declares PostgreSQL, Garage and the step that sets its cross-origin rule, Mailpit, Caddy, the migration and the seed as steps that run once, and the API and the worker.
- It declares no Dex, no River panel, no telemetry and no LLM stand-in. No feature calls an LLM yet; the stand-in comes with the first one.
- The volumes of PostgreSQL and Garage are recreated on every publish. Caddy's data volume, which holds the certificates, is kept while the server lives.
- Caddy serves the three host names over HTTPS, with certificates it gets by itself. The app's host name serves the built web app and forwards `/api` and `/up` to the API. The files host name forwards to Garage. The mail host name forwards to Mailpit behind a password the browser asks for.
- The rules of delivery keep one home and apply in development and staging alike: compression, cache headers, security headers, the rate limit and the masking of addresses in the logs.

### The publish

- **Its inputs**, given by the pipeline: the commit, the pull request, the address, the host key, the deploy key and the password of Mailpit.
- **The variables file.** Every name of `.env.example` has a staging value, and each value is one of four kinds: fixed, derived from the address, drawn at random on each publish, or deliberately empty (the local login provider, Resend, the test-only names). A name of `.env.example` that has no decision stops the publish before it changes anything on the server. The file is written readable by the deploy user alone.
- **Its steps, in order**:
  1. Connect, trusting only the given host key.
  2. Copy the Compose files and Caddy's configuration.
  3. Write the variables file.
  4. Pull the images of the commit.
  5. Stop the API and the worker.
  6. Remove the volumes of PostgreSQL and Garage.
  7. Start PostgreSQL, Garage and its rule, Mailpit and Caddy.
  8. Run the migration, then the seed.
  9. Start the API and the worker.
  10. Wait until `/up` answers on the server.
- **Output**: it prints no value, no variables file and no configuration of a container.

### The pipeline

- **The job**: a job of `ci.yml` that runs after `Checks`, for a pull request from this repository that has the `staging` label, in the GitHub Environment `staging`. It is not one of the jobs `Checks` needs, so the required check does not change.
- **Its steps**: push the three images, run the publish, take the label off any other open pull request that has it with a comment there, and comment the links on the published one.
- **When staging is off** (the address variable is empty), it skips with a notice and passes.
- **Events**: the workflow also runs when a label is added, so that labelling an open pull request publishes it; the checks run again then.
- **Concurrency**: one group for staging, and a running publish is never cancelled.
- **Permissions**: the job asks for writing packages and pull requests, and nothing else beyond reading the contents.
- **The reminder**: a scheduled workflow, every six hours, fails when the time staging was turned on is more than 24 hours old. It reads only that variable and holds no secret.

### The agent

- **A global rule of Claude on this machine**, written for this repository, once the command that turns staging on exists. Before a pull request, the agent asks one question with three answers: open it with staging, open it without staging, or do not open it. It recommends without staging for a pull request that changes only documents.
- **With staging**: the agent turns staging on from the main checkout, then pushes and opens the pull request with the label. When turning on fails, it reports and stops before the push.
- **After the pull request is open**: the agent follows the publish and gives the links. It never turns staging off by itself, and asks whether to when the maintainer says the pull request was merged or closed.

### Records

- `CODING_STANDARDS.md`, in the sections:
  - Environments: Servers, Secrets, Seed and Compose files;
  - Pipeline: the checks build the images, the reminder replaces the sweep, Publishing;
  - Versions: where the system image of a server, the provider's library and Docker on the server keep their versions;
  - Delivery, if the Caddy configuration changes shape.
- `docs/providers.md`:
  - the Hetzner project `software-factory-manager-staging` and its region;
  - the prices read from Hetzner's API on 2026-11-19, in US dollars and without VAT: CX23 at US$ 0.0104 an hour, up to US$ 6.49 a month, and IPv4 at US$ 0.001 an hour, up to US$ 0.60 a month;
  - no reserved address, no firewall at the provider, and the deploy key of each server;
  - the GitHub Environment `staging`, its label, and the names of its secrets and variables;
  - `sslip.io` as the provisional address, with why it should go soon.
- `README.md`: how to turn staging on and off, and what the machine needs (`gh` logged in, `HETZNER_TOKEN` in `.env`).
- `docs/machine-tools.yaml` does not change: nothing new is installed on the machine.
- A ticket `ready-for-human`, in a folder of its own under `.scratch/`, so that it outlives this effort: register the domain and move staging off `sslip.io`.

## Testing Decisions

- **The seam is the commands of the tool.** Turn on, turn off, status and publish are tested with in-memory implementations of the provider, of GitHub, of readiness and of the remote server, the last one recording the files written and the commands run in order. The tests check outcomes:
  - what exists at the fake provider;
  - what the fake GitHub holds;
  - the order of the remote steps;
  - that turning on twice creates one server;
  - that a failure destroys what was created and deletes what was written;
  - that a name of `.env.example` with no decision stops the publish before any remote step;
  - that no secret value appears in what the commands print.
- **The Hetzner adapter** is tested against a stand-in of Hetzner's API that answers as its documentation says, as the `resend` adapter's tests do with Resend. No test calls Hetzner itself; the first real turn-on does, by hand.
- **The `gh` adapter** is tested against a stand-in executable that records its arguments and its standard input, which proves that a secret never travels as an argument.
- **The SSH adapter** is tested against an SSH server the test starts in-process: it runs commands and writes files on the expected host key, and refuses a server whose key differs.
- **The cloud-init file** is validated by the checks with cloud-init's own schema validator, run from an image pinned by version, when that costs little; the first real turn-on proves it either way.
- **The images** are built by the checks of every pull request, as the seed's is.
- **The acceptance test** is real: turn staging on, publish this effort's own pull request, log in with a login code read from Mailpit, turn staging off. It is done by the agent with the maintainer and recorded in the ticket that closes the effort.
- **Prior art**:
  - the in-memory implementations of `applicationtest`;
  - the stand-in of Resend's API;
  - the tests of `cmd/seed`, which prove a command's outcome;
  - `caddy_test.go`, which starts the Caddy image;
  - the tools of `apps/api/tools/guard`, with their tests beside them.

## Out of Scope

- The production server, its publish, its backups and the monthly restore test, which ADR-0019 now says becomes a task of the machine, decided with production.
- The real login providers in staging, which need the domain.
- Registering the domain: a ticket for the maintainer.
- Removing old images from the GitHub container registry.
- The LLM stand-in, which comes with the first LLM feature.
- Telemetry and the River panel in staging.
- The manual workflow that publishes an earlier commit, and releases.

## Further Notes

- Hetzner's API answered on 2026-11-19 with the token in `.env`. The project was empty, and the CX23 was offered in `fsn1`, `nbg1` and `hel1`. The account is billed in US dollars. Hetzner has removed datacenters from its API: a server, an address and a volume name a location.
- `sslip.io` is not on the Public Suffix List, so Let's Encrypt counts all of its certificates against one shared limit, which has run out in some weeks. Caddy then tries ZeroSSL. A new server has new host names, so no certificate is ever asked for twice.
- The token was pasted into the conversation of the grilling. Once staging works, the maintainer revokes it in the Hetzner Console, makes a new one and replaces it in `.env`.
- The runners of GitHub Actions have no IPv6, and the GitHub container registry does not answer over it. That is why the server has an IPv4 address.
