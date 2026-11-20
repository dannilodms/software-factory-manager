## Agent skills

### Issue tracker

Issues live as local markdown files under `.scratch/<feature-slug>/`. See `docs/agents/issue-tracker.md`, also when a ticket's work is done or every ticket of a spec is resolved.

### Triage labels

Default vocabulary (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`), recorded as a `Status:` line in each issue file. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.

### Browser tools

Chrome DevTools MCP, for investigating performance, console and network in a real Chrome. See `docs/agents/browser-tools.md`.

## Coding standards

Read `CODING_STANDARDS.md` before writing or reviewing code under `apps/`, `deploy/` or `.github/`. Backend: layout by domain area, dependency rule, use cases, domain modelling, login, setup, users and roles, configuration, database access and migrations, background jobs, files, credentials, e-mail, naming, test seam. Frontend: layout by domain area, where each kind of state lives, the data module, forms, tests. Delivery: response compression, cache headers, security headers and the limit on the login code and the setup code, set once in the snippets of `deploy/delivery.caddyfile`; read it before changing a Caddyfile. Observability: how telemetry is collected, kept and shown, and what must never appear in it; read it before changing the collector, Grafana or what a container logs. Environments: the list of providers in `docs/providers.md`, what development, staging and production each run, the seed, the servers, secrets and backup; read it before changing a Compose file or anything in `deploy/`. Pipeline: how a change reaches `main` and the servers, how a release is made, and the rules a public repository sets; read it before changing a workflow in `.github/` or making a release. Versions: where the version of each tool, library and container image is recorded, and how it is updated; read it before adding or updating a dependency.

## Design system

Read `DESIGN.md` before writing or reviewing interface code under `apps/web`. It holds the rules of the look, the names, roles and values of the style variables, the components and the recipes. A change to any of them updates `DESIGN.md` in the same change.

## Records to keep current

Two files record what lives outside the code. When a task adds, replaces, updates or removes one of the things below, update its file in the same change. The task is done when the file matches what is installed or configured.

- **`docs/machine-tools.yaml`**: a tool on the maintainer's machine (a command-line tool, a runtime, a package manager). Write the version the tool itself reports and where it was installed from. `checked` dates the whole file: move it only after reading every version again. A tool whose version the project pins is recorded in its home instead, listed in Versions in `CODING_STANDARDS.md`.
- **`docs/providers.md`**: an outside service the system depends on, and anything set at one (plan, region, server, bucket, host name, DNS record, cost). Update its section and its row in the summary table; a new provider gets a section with why it was chosen and what it costs. Write addresses and names; a key, token or password stays in the secrets store.

When a suggestion or a decision in the conversation would change either file and the change is still to be made, say so in the report.
