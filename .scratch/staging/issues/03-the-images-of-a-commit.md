# 03: The images of a commit

**What to build:** A server runs images, not tools on the machine. Every commit gets an image of the API, whose binary carries its commands (serve, migrate, worker), and an image of Caddy with the built web app inside. The checks of every pull request build both and publish nothing, as they already do for the seed's image. See the spec, section Images.

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] An image of the API runs the binary with each of its commands: serve, `migrate`, `worker`. `issue-setup-code` stays reachable for the installer.
- [x] The web app is built with the same command the checks use, and its output goes into an image of Caddy that keeps the rate-limit plugin. Node keeps its one home in `apps/web/package.json`: no Dockerfile names a Node version.
- [x] Task targets build each image locally and publish nothing, as `seed:image` does.
- [x] The backend job of the pipeline (or the job that builds the web app) builds both images on every pull request, so that a change that breaks either fails before anything is published.
- [x] `task versions:check` accepts every new pinned version, and every new image tag is exact.
- [x] The development Compose file and `task dev` behave as before: development still serves the web app through Vite.
- [x] `CODING_STANDARDS.md` (Checks on a pull request, Publishing, Versions) names the images and where their versions live.

## Comments

### What was built

- **The image of the API**: `apps/api/cmd/api/Dockerfile`, in the shape of the seed's, with the Go module as its context: the static binary of `cmd/api` on distroless `static-debian12:nonroot`, pinned by the digest the seed uses, with `ENTRYPOINT ["/api"]`. A Compose service or a run picks the command: none serves, `migrate`, `worker`, and `issue-setup-code` for the installer (`docker compose run --rm api issue-setup-code`).
- **A health check without a shell**: a fifth command of the binary, `probe-readiness` (`cmd/api/probe_readiness.go`), asks `GET /readyz` at `API_LISTEN_ADDRESS`, with `127.0.0.1` in place of a host that names every interface, and exits 0 only on a 200. It prints nothing when the API is ready and one line when it is not. Ticket 04 writes `healthcheck: test: [CMD, /api, probe-readiness]`.
- **The one build of the app**: `task web:build` (`vp build` into `apps/web/dist`). A Vite plugin of the repository, `apps/web/brotli-copies.ts`, writes a `.br` copy beside each JavaScript, CSS, HTML, SVG and JSON file once the build has written them.
- **The Caddy image with the app**: `deploy/caddy/Dockerfile` has two stages. `development`, which `compose.yaml` now names as its `target`, is the image as it was. `servers` is the same Caddy with `COPY . /srv`, built with the output of the build as its context, so no Dockerfile names Node.js.
- **Task targets that publish nothing**: `task api:image` builds `software-factory-manager-api:servers`. `task caddy:image` runs `task web:build` and then builds `software-factory-manager-caddy:servers`. `task seed:image` is unchanged (`software-factory-manager-seed:development`). `task check:quick`, and so `task check`, builds all three.
- **The pipeline**: the backend job builds the image of the API after the one of the seed (`task api:image`). The app job, which has the Node.js of the app, builds the app and the Caddy image after `task web:check` (`task caddy:image`).
- **Records**: `CODING_STANDARDS.md` (Commands, Plain lines in Telemetry, Readiness in Health, Plugin in Compression, Deferred, The images are built in Checks on a pull request, Images in Publishing, a row of Versions), `README.md` and `README.pt-BR.md` (the commands table, `task check`, Build, the line on readiness).

### Decided while building

- **The health check is a command of the binary.** The image keeps no shell and no HTTP client. A base image with busybox, or leaving the wait to Caddy and the publish, were the other ways (surprise 5). A command that runs as the process would is testable whole, as Commands asks. The rule is in Health, Commands and Telemetry of `CODING_STANDARDS.md`.
- **The web app goes in as the build context, not as a named build context.** `docker build --target servers --file deploy/caddy/Dockerfile apps/web/dist`. Every `FROM` and `COPY --from` stays an image or an earlier stage, so `task versions:check` needed no new rule (surprise 3). Checked: the stage `development` builds the same image ID as the Dockerfile did before the change, so `task services` does not create Caddy again.
- **Local names say where an image runs.** `:servers` for the API and for Caddy with the app, which only the servers run. `:development` for the Caddy of development and for the seed, which `compose.yaml` declares. The publish (ticket 05) tags each one again as `ghcr.io/dannilodms/software-factory-manager-{api,caddy,seed}:<commit>`, the names Publishing in `CODING_STANDARDS.md` now gives.
- **`caddy:image` builds the app every time**, so an image never holds an earlier build. `web:check` does not build it, or the app job would build it twice. The checks still use the one command, through `caddy:image`.
- **The Brotli plugin is the repository's own** (surprise 2, Compression in `CODING_STANDARDS.md`). It is about twenty lines over `node:zlib`, at the highest quality, in `closeBundle`, and needs no dependency: the types of Node come through the types of Vite+. No plugin of the ecosystem had been tried with Vite+ (Rolldown). Every text file gets its copy, as Compression says, even a few bytes where the copy is not smaller.
- **`task check:quick` builds the three images**, the seed's included, which it did not build before. That keeps true the rule that `task check` runs every check of a pull request.
- **The Go images keep the compiler's cache between builds**, with a cache mount in the Dockerfiles of the API and of the seed. On this machine a rebuild after a change compiled in 6.5 s, not 89 s. Nothing of the cache goes into the image, and the pipeline starts with an empty cache.
- **After the code review** (2026-11-20): `task seed:image` builds `software-factory-manager-seed:servers` with `docker build`, as the other two are built, so the publish pushes the three by one name (`software-factory-manager-<image>:servers`); the `seed` service of `compose.yaml` keeps its own name of development. The size limit of the JavaScript is written back as deferred, in the frontend rules, with its numbers and its tool still the maintainer's. "Install Task" stays copied in each job: zizmor refuses a composite action that writes `GITHUB_PATH` (`github-env`, high).

### What was checked

- Test-first, at the seam of the command (`run`, as the process would): `probe_readiness_test.go` covers a ready API, a pending migration (exit 1, `answered 503`), no API listening, a listen address of every interface (`0.0.0.0`, empty, `[::]`) and a missing `API_LISTEN_ADDRESS`. The config test gained the command's required variable.
- Both images were built with their Task targets. The API image ran in a scratch Compose project of its own, on the network of development, with `healthcheck: [CMD, /api, probe-readiness]`, and Docker reported it healthy. Each command was reachable in the image.
- The Caddy image of the servers: `/srv` holds `index.html` and `assets/` with their `.br` copies, `caddy list-modules` lists `http.handlers.rate_limit`, and the version is 2.11.9. Every one of the eight copies decompresses to its file.
- `task versions:check`, `task workflows:check` (offline audits), `vp check` (the new file is type-checked) and the lint of the API all pass.
- `task check:quick` ran once at the end, with the three images. Every step passed but `api:test`, where two tests outside this change, `TestTheWorkerStopsAtOnceWhileAMigrationIsPending` and `TestTheSeedFailsWhileAMigrationIsPending`, failed in their cleanup: `DROP DATABASE ... WITH (FORCE)` ran out of time while the whole suite loaded PostgreSQL. `go test ./cmd/...` passed when run again. `task e2e` passed (7 tests), with the Caddy of development built from the stage `development`, so development still serves the app through Vite.
- For real, on 2026-11-20, in run 44662023954 of pull request #10:
  - the job Backend passed "Build the image of the seed" and "Build the image of the API";
  - the job App passed "Build the web app, and the Caddy image of the servers with it inside";
  - the job "Publish to staging" then built the three images again with the same Task targets, pushed them, and the server ran them (ticket 05).

### Found, and left for a decision

- **The size limit of the JavaScript** (Enforcement in the frontend rules: measured on the Brotli copies, "numbers set when the deploy is built") is not set: the numbers and the tool are for the maintainer, or for ticket 04. The copies of today: the entry `index-*.js` 122 KB and `form-fields-*.js` 134 KB in Brotli (444 KB and 501 KB minified; Vite warns above 500 KB), the CSS 16 KB.
