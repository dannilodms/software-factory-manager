# Code review of feat/staging (origin/main...c8e565c), 2026-11-20

Two independent reviews. Fix everything under "To fix". Items under "Not code" are real runs the orchestrator does; do not touch them.

## Standards

### Hard (documented standards), most severe first

1. **Configuration, "Secrets do not print"** (`CODING_STANDARDS.md` ~:162; the tool's own exceptions at ~:163 and ~:165 put it under that section). Private keys travel as plain `[]byte`/`string`, and become `config.Secret` only at `SetSecret` and `renderCloudInit`:
   - `keys.go`: `serverKeys.deployPrivate`, `hostPrivate`, `loginKeys.deployPrivate`;
   - `values.go`: `serverValues.deployKey`;
   - `localcopy.go`: `save(…, deployKey []byte, …)`;
   - `cloudinit.go`: `cloudInitValues.HostPrivateKey`;
   - `environment.variable` also hands the token back as a `string`.
2. **Tests** (~:330) says "each real adapter has tests against the real service", but `provider/hetzner` and `github/gh` are tested against stand-ins only. The decision "No test calls Hetzner" exists only in the spec (`.scratch/staging/spec.md`) and ticket 01, so it must be written into Tests.
3. `README.md:485` and `README.pt-BR.md:485` were edited on this branch but still say Caddy holds requests for 30 s through `lb_try_duration`, which no Caddyfile sets. Ticket 04 left it "for the maintainer". Make the READMEs say what is built, and keep the open point where it is.
4. `AGENTS.md` still says delivery is "all set in the `Caddyfile`", against the new Delivery intro (~:545: the snippets of `deploy/delivery.caddyfile`).
5. **Frontend Enforcement** (~:530, ~:533) puts the JS size limit off until "when the deploy is built". This branch builds it, and the Deferred bullet "The deploy." (~:393) no longer lists it. Write it back as deferred (ticket 03 left the numbers for the maintainer), or reword Enforcement.
6. Minor:
   - `cmd/api/caddy_staging_test.go:126,145` use `t.Fatalf`, against the testify rule (~:332);
   - `gh/gh_test.go:53` calls `os.Getenv`, against "No other code reads the environment" (~:159);
   - `compose.base.yaml:145-147` writes a bare `${FILE_STORAGE_BUCKET}` and others, against Values (~:661).

### Security (judgement)

- **Windows access list.** `dotenv.go` `appendTo` says the file it creates is "readable by its owner alone" because of `0o600`. On Windows, Go's file modes set no access list, as `owner_windows.go` states. Call `restrictToOwner`.
- **The host key in Hetzner's metadata.** The private host key sits in Hetzner's user data, which any container on the server can read from 169.254.169.254, since ufw lets out all outgoing traffic. Consider a DOCKER-USER drop rule in `cloud-init.yaml`, kept across reboots and in place before any container starts. The orchestrator checks it on the next real server: from a container, `wget -T 3 http://169.254.169.254/hetzner/v1/userdata` must fail.

### Smells (judgement)

- **Duplicated Code:**
  - `failedTo` is in both `on.go` and `gh/gh.go`;
  - there are two `.env` line parsers, `dotenv.go` `assignedIn` and `variables.go` `withNameOf`;
  - the retry-until-deadline loop is written twice, `sshclient/readiness.go` `Wait`/`again`/`unlessCut` and `up.go` `WaitUntilUp`/`pause`/`unlessCutShort`;
  - `ci.yml` has a fifth copy of "Install Task".
- **Data Clumps:**
  - address, host key and deploy key travel together in `serverValues`, `loginKeys`, `publication` and `localCopy.save(address, deployKey, hostKey)`;
  - `sshclient.Target{Address: net.JoinHostPort(…, sshPort), …, User: deployUser, …}` is built separately in `publish.go` and `on.go`.
- **Mysterious Name:**
  - `servers, err := environment.provider()` names a provider "servers";
  - `hub` names the GitHub port;
  - the staging seed is pushed from the local tag `seed:development`.
- **Middle Man:** `variables.go` has `derived(from) value { return from }`.
- **Divergent Change:** `status.go` holds `hostNamesOf` and `report`, which publish, on and variables use; `values.go` holds the general `inOrder`.

## Spec

### To fix

- **The variables check does not reach the containers** (spec: "A name of `.env.example` that has no decision stops the publish before it changes anything on the server"; story 30, "so that a new variable never half-breaks staging").
  - `variablesFile` (`variables.go:140`) checks only that `variables.go` decides every name.
  - Compose reads `staging/.env` only for interpolation, and each service of `deploy/compose.staging.yaml` gets only the names its `environment:` lists (ticket 04).
  - So a new required API variable, decided in `variables.go` but missing from `api.environment`, passes the check. The publish then fails at step 9, after step 6 removed the data.
  - No test ties the names the binary reads to the Compose lists. `TestEveryVariableTheComposeFilesOfStagingRequireHasAValueOnEveryPublish` covers only the other direction.
- **Mailpit outlives a publish** (story 29: "start the database and the file storage again … so that … nothing of an earlier pull request is left"). `rm --stop` (`publish.go:194`) removes only postgres and garage, and `up` (`publish.go:199`) leaves the same Mailpit container running, with the earlier pull request's login codes.

### Not code (the orchestrator's)

- The acceptance run of ticket 08, the notice while staging is off (ticket 05), and the reminder's first run on `main` (ticket 07).

### Accepted as justified

- `probe-readiness` (no shell in a distroless image for a health check) and the publish reading the label again (story 25).
