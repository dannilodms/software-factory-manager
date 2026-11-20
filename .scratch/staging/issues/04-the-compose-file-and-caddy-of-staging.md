# 04: The Compose file and Caddy of staging

**What to build:** The description of what runs on the staging server. It is a Compose file that extends the base file and runs PostgreSQL, Garage with its cross-origin rule, Mailpit, Caddy, the migration and the seed as steps that run once, and the API and the worker. Caddy serves three host names over HTTPS: the app with the built web app, `/api` and `/up`; the files, forwarded to Garage; and Mailpit behind a password. It keeps the same rules of delivery as development. See the spec, section The staging Compose file.

**Blocked by:** 03.

**Status:** resolved

- [x] The staging Compose file extends the base file and declares only what the spec lists: no Dex, no River panel, no telemetry, no LLM stand-in.
- [x] The host names of the app, the files and Mailpit, and the origin of the app, come from configuration, so that the publish derives them from the server's address.
- [x] Caddy gets its certificates by itself. Its data is in a volume that the publish keeps, while the volumes of PostgreSQL and Garage are the ones the publish recreates.
- [x] Mailpit's host name asks the browser for a password, taken from configuration.
- [x] The rules of delivery keep one home for development and staging: compression, cache headers, security headers, the rate limit on the setup code and the login code, and the masking of addresses in the logs.
- [x] A test through HTTP, as `caddy_test.go` does, starts Caddy with the staging configuration and its own local certificates. It proves the forwarding of the three host names, the password of Mailpit, the rate limit and the headers.
- [x] The checks validate the staging Compose file with `docker compose config`.
- [x] `CODING_STANDARDS.md` (Compose files, Delivery) matches what was built.

## Comments

### From ticket 01

- Docker publishes the port of a container past ufw, the firewall that `deploy/cloud-init.yaml` sets inside the server: it writes rules of its own in iptables. The staging Compose file publishes Caddy's 80 and 443 and nothing else, or what it publishes is open to the internet whatever ufw says. The firewall lets in 443/udp too, for HTTP/3 if Caddy keeps it. The rule is in Ports, in Servers of `CODING_STANDARDS.md`.
- The host names are `app.`, `files.` and `mail.` before the address of the server written with dashes, then `.sslip.io` (`app.203-0-113-10.sslip.io`), as `hostNamesOf` in `apps/api/tools/environment/status.go` writes them and `task staging:status` prints them. The publish derives them the same way, from that function.

### What was built

- **`deploy/compose.staging.yaml`**, project `software-factory-manager-staging`: `postgres`, `garage`, `garage-cors`, `mailpit`, `caddy`, `migrate` and `seed` (run once), `api` and `worker`. The API, the worker and the migration run `ghcr.io/dannilodms/software-factory-manager-api:${COMMIT}`, each with its command; Caddy and the seed run their images by the same `COMMIT`. `migrate` waits for PostgreSQL to be healthy; the seed, the API and the worker wait for `migrate` to end well; Caddy waits for `garage-cors`. Volumes: `postgres-data` and `garage-data` (the publish removes them), `caddy-data` and `caddy-config` (kept). Only Caddy publishes ports: `80` and `443` over TCP, and `443` over UDP for HTTP/3. Each service lists, by name, only the variables its command reads.
- **`deploy/compose.base.yaml`** now holds what a service is wherever it runs: the command, configs, environment, volume and health check of Garage, the script of `garage-cors`, the connection of the seed, and three services of the servers: `api` (health check `[CMD, /api, probe-readiness]`, stop grace period 45 s), `worker` (command `worker`, 45 s) and `migrate` (command `migrate`). `compose.yaml` keeps only what differs in development; `docker compose config` of development is the same as before but for the new mount of Caddy.
- **One home of the rules of delivery**: `deploy/delivery.caddyfile`, with the snippets `logs-without-whole-addresses`, `site-of-the-app` (compression, `/up`, `/api` with `no-store`, the two limits, the 429 and the 502/503/504 in the error format), `the-built-app` (`/srv`, `precompressed br`, the cache headers, `index.html` for a route, 404 for a missing file of `/assets/`) and `security-headers`. `deploy/Caddyfile` (development) and the new `deploy/Caddyfile.staging` import it. Development now compresses too.
- **`deploy/Caddyfile.staging`**: the sites `{$APP_ORIGIN}`, `{$FILE_STORAGE_PUBLIC_URL}` and `{$MAILPIT_PUBLIC_URL}` (the last behind `basic_auth`, user `maintainer`, hash `{$MAILPIT_PASSWORD_HASH}`), and `email "{$ACME_EMAIL}"`. Caddy keeps HTTP/3, which the firewall of ticket 01 lets in over UDP. Upstreams default to `api:8080`, `garage:3900` and `mailpit:8025`, and can be named by the environment.
- **`apps/api/cmd/api/caddy_staging_test.go`**: six tests start the Caddy image with the staging Caddyfile, a fixture of the built app in `/srv`, and stand-ins for the API, Garage and Mailpit, over HTTPS for host names under `localhost`, trusting Caddy's local root. They prove HTTP to HTTPS, `index.html` for `/` and a route with `no-cache`, `immutable` under `/assets/`, a 404 with no cache header for a missing asset, the Brotli copy and gzip, the CSP and the other headers, `/api` with `no-store` and `/up`, the Host header that reaches Garage, 401 without and with a wrong password of Mailpit and the answer with the right one, both limits with their 429 Problem, and the masked address in the logs. The stand-in of `caddy_test.go` became `standIn`, which also records the host and the path.
- **The versions guard** accepts an image this repository publishes, `ghcr.io/dannilodms/software-factory-manager-<name>`, tagged with one variable of Compose, and nothing else with a variable (test cases in `versions_test.go`).
- **`task compose:check`**: `docker compose --file deploy/compose.staging.yaml config --quiet`, with `.env.example` and sample values for `COMMIT` and `MAILPIT_PASSWORD_HASH`. `task check:quick` and the backend job of `ci.yml` run it.
- **`.env.example`**: `ACME_EMAIL`, `MAILPIT_PUBLIC_URL`, `MAILPIT_PASSWORD_HASH` and `COMMIT`; the stale line on the seed's mailbox in staging now says staging keeps the fictitious default.
- **`apps/web/vite.config.ts`**: `build.assetsInlineLimit: 0`, so no font goes inline as `data:`.
- **Records**: `CODING_STANDARDS.md` (Delivery, Compose files, Shutdown, Tests, Privacy, Development, Checks on a pull request, Enforcement of Versions, Deferred, LLM stand-in), ADR-0019 (ZeroSSL needs an address), `docs/providers.md` (Let's Encrypt), `README.md` and `README.pt-BR.md` (`task compose:check`, 45 s, the folder `deploy/`).

### Decided while building

- **Development imports every rule but the built app and the security headers.** Vite serves the app there, with an inline script (React's refresh) and style elements it writes at run time, which the policy refuses. The cache headers of the built app are about files Vite does not serve. Development gains compression. In Delivery.
- **The CSP, closed against the built app** in a local rehearsal (below): `style-src-elem 'self' 'unsafe-inline'`, because input-otp writes an empty `<style>` and fills it through the CSSOM, and Base UI writes one to hide a scrollbar; with only `'self'` the browser refused input-otp's on the screen of the login code. `blob:` in `img-src` and `media-src` for the preview of a chosen image. `Referrer-Policy: same-origin`, because `no-referrer` would make a browser send `Origin: null` with a form POST, which the API's check of the origin refuses. In Security headers.
- **No `data:` in the policy**: the build stopped inlining small files (a JetBrains Mono subset was a `data:` font).
- **The host names are the origins.** Caddy's site addresses are `APP_ORIGIN`, `FILE_STORAGE_PUBLIC_URL` and `MAILPIT_PUBLIC_URL`, which carry `https://`; the first two are already variables of the API, so the publish derives three values from the address, not six.
- **The connections are written by Compose** from the passwords of the roles, as development's seed already did (surprise 8), and so are the addresses of the services of the network (`FILE_STORAGE_URL`, `EMAIL_SMTP_ADDRESS`, `API_LISTEN_ADDRESS`) and `EMAIL_SENDER=smtp`. In Compose files.
- **The Mailpit password is Caddy's `basic_auth`**, not Mailpit's own `MP_UI_AUTH`, so the test of Caddy proves it and Mailpit keeps no configuration. Caddy needs a bcrypt hash, so the variables file holds `MAILPIT_PASSWORD_HASH`, in single quotes.
- **The test uses host names under `localhost`**, for which Caddy issues certificates from its own authority by itself: the staging Caddyfile needs no switch for the test.
- **HTTP/3 stays on**, Caddy's default: Compose publishes 443/udp, which the firewall of ticket 01 lets in.
- **Stop grace period 45 s** for the API and the worker: 20 s of drain or of jobs, 15 s for the pool, 5 s for the spans. README said 30 s and now says 45 s. In Shutdown.
- **The configs of PostgreSQL and Garage are written twice**, in `compose.yaml` and in the staging file, because `extends` copies none and a config of a file would be a bind mount of the checkout that development avoids. Each file says a change goes into both. In Compose files.
- **The image of a server is named by `COMMIT`**, a variable of `.env.example` like the other names only the files of the servers read, so the publish's check covers it.
- **After the code review** (2026-11-20): every variable the base and the staging files require is written `${NAME:?...}`, the health check of Garage and the region in the config of Garage included (and in its twin in `compose.yaml`). The README says what is built: Caddy holds no request while the API restarts; `lb_try_duration` stays under Found, and left for a decision.

### What was checked

- Test-first: the guard's new cases failed before the rule and pass after. The six staging tests pass, and fail when the security headers or the password of Mailpit are taken out of the Caddyfile. Every test of `caddy_test.go` still passes with the Caddyfile of development split into the shared file.
- `caddy adapt`: the development Caddyfile gives the same configuration as before plus `encode`; the staging one gives Let's Encrypt alone without `ACME_EMAIL` and Let's Encrypt then ZeroSSL with one, and the routes `/up`, `/api`, `/assets/*`, then the rest. `caddy fmt` agrees with both files.
- `docker compose config --hash` of development, before and after: the same for every service but Caddy, which gained a mount.
- **A local rehearsal of staging**, in a Compose project of its own (`sfm-staging-rehearsal`) with only Caddy's port moved to `127.0.0.1:18443` by an override: the three images built from this worktree and tagged as the published ones, `up --wait` of the data and Caddy, `run --rm migrate`, `run --rm seed`, `up --wait api worker`, `/up` answering 200. In Chromium (Playwright), the login screen, a login code asked for `seed+admin@fabrica.example`, read through Mailpit behind its password, the login, and the shell, with the three fonts loaded and no violation of the policy but Zod's probe of `eval`. Mailpit answered 401 without the password. Everything was removed after, the local tags included.
- `task versions:check`, `task compose:check` (and its failure without `COMMIT`), `task api:lint` and `vp check` pass.
- `task check:quick` passed every step up to `api:test`, whose `task services` met a Caddy that another worktree was creating at that moment. `task api:test` then failed only where the shared PostgreSQL refused connections to the tests of several worktrees at once ("too many clients"); run again with `-p 4`, every package passed but `platform/session`, where a random cookie happened to hold the digits `37` that `TestTheSessionIsAnOpaqueCookie...` searches for, and which passed run alone. `task e2e` passed, 7 tests, through the Caddy of development with the shared rules.

### Found, and left for a decision

- **Caddy tries ZeroSSL only with an address** (`DefaultIssuers` of Caddy 2.11.9). The spec counts on ZeroSSL when the shared limit of Let's Encrypt on `sslip.io` runs out. `ACME_EMAIL` is read and empty: which address to give, and where it is kept (a value of the GitHub Environment, or fixed), is the maintainer's choice; until then a week with the limit spent leaves staging without a certificate.
- **Zod's probe of `eval`** writes one violation on the console of the browser. Zod then works without `eval`. `z.config({ jitless: true })` in `main.tsx` does not stop it, because the chunk that builds the schemas runs first; Rolldown's `strictExecutionOrder`, or a banner that sets `globalThis.__zod_globalConfig`, would. Accept the line, or take one of those.
- **`lb_try_duration`**, which the README promises for production (Caddy holding requests while the API restarts), is not written: in staging the publish stops the API for longer than any hold, and production has no Compose file yet.
