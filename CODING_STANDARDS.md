# Coding standards

How code in this repository is written. The reasons and the rejected alternatives are in ADRs [0007](docs/adr/0007-backend-organized-by-domain-area.md) (structure), [0008](docs/adr/0008-use-cases-served-by-several-inbound-adapters.md) (inbound adapters), [0009](docs/adr/0009-rest-api-conventions.md) (API conventions), [0010](docs/adr/0010-sequential-identifiers-exposed-through-sqids.md) (identifiers), [0011](docs/adr/0011-login-through-external-providers-with-a-server-side-session.md) (login), [0012](docs/adr/0012-opentelemetry-for-observability-and-a-separate-audit-log.md) (observability), [0013](docs/adr/0013-frontend-data-in-collections-and-cursor-queries.md) (frontend state and data), [0014](docs/adr/0014-frontend-organized-by-domain-area-and-tested-through-screens.md) (frontend structure and tests), [0015](docs/adr/0015-hand-written-sql-checked-by-sqlc-and-forward-only-migrations.md) (database access and migrations), [0016](docs/adr/0016-background-jobs-in-postgresql-run-by-a-worker-command.md) (background jobs), [0017](docs/adr/0017-files-in-garage-reached-directly-by-the-browser.md) (files), [0018](docs/adr/0018-credential-values-encrypted-by-the-api.md) (credential values), [0019](docs/adr/0019-three-environments-with-staging-on-a-server-created-on-demand.md) (environments and publishing), [0020](docs/adr/0020-postgresql-backed-up-by-pgbackrest-and-production-files-in-backblaze-b2.md) (backup and production files), [0021](docs/adr/0021-a-public-repository-under-agplv3.md) (public repository) and [0022](docs/adr/0022-a-record-with-a-lifecycle-is-a-state-machine-in-the-domain.md) (statuses and transitions).

## Backend (`apps/api`)

### Layout

```
apps/api/internal/
├── <domain area>/
│   ├── domain/         # aggregates, value objects, domain errors
│   ├── application/    # use cases, and the interfaces they need
│   └── adapters/
│       ├── rest/       # inbound: Huma operations, including SSE
│       ├── mcp/        # inbound: MCP tools
│       ├── river/      # inbound: job workers; outbound: saving a job
│       ├── postgres/   # outbound
│       ├── s3/         # outbound
│       ├── smtp/       # outbound
│       └── resend/     # outbound: sending; inbound: bounce notices
├── kernel/             # value objects used by more than one domain area
└── platform/           # shared technical infrastructure
```

A **domain area** is one part of the business with its own aggregates. It is the deep module of the backend: its public surface is its use cases, and everything else is internal to it.

Beside `internal`, `apps/api/cmd/api` holds the commands of the binary (see Commands), `apps/api/migrations` the migrations (see Database), `apps/api/tools` the tools written in this repository, which no image holds: the checks of the build (see Enforcement) and the tool that turns an environment on and off (see Servers in Environments), and `apps/api/openapi.yaml` the OpenAPI spec generated from the code (see Enforcement). `platform` is a set of packages, one per concern: `clock` (the clock of the machine), `config`, `database`, `health`, `identifier` (the identifier of a record as it leaves the backend, see API conventions), `jobs` (River: the queues, the saving of a job and the runner of the worker, see Jobs), `lifecycle` (the signals, the shutdown notice and the server), `restapi` (what every REST operation stands on: Huma, the error format, strict input, the limits of a body and the operations that need a logged-in user), `session` (the session of a browser: scs on PostgreSQL, and the refusal of a request from another site that carries its cookie, see Login) and `telemetry` (the logger, the tracer and the telemetry of a request).

An **inbound adapter** calls use cases on behalf of an outside caller: REST for the React app and for the agents of ADR-0004, MCP for coding agents, a job worker for work that runs in the background. An **outbound adapter** implements an interface that `application` declares: Postgres, the S3 storage, an LLM provider. `adapters` has one package per technology, one level deep, and an area creates only the ones it uses.

### Precedence

When two rules collide, the higher one wins:

1. The dependency rule.
2. A small public surface for the domain area.
3. The modelling and control-flow rules.

### Dependency rule

Dependencies point inward: `adapters` → `application` → `domain`.

| Package | Imports |
| --- | --- |
| `domain` | the standard library, `kernel`, its own area's `domain` |
| `application` | its own `domain`, `kernel`, interfaces from `platform`, the `application` of other areas |
| `adapters` | its own `application` and `domain`, `kernel`, `platform`, third-party libraries; no other adapter package |
| `kernel` | the standard library |
| `platform` | third-party libraries; no domain area |

`application` reaches infrastructure only through an interface: one it declares itself, or one `platform` exports (the LLM provider interface of ADR-0004). The area that uses an interface declares it; an adapter implements it.

`task api:lint` checks the table on every import under `internal` (see Enforcement). It reads the place of a package from its path, so a new area needs no change to the check. A package under `internal` that is outside the Layout fails it too.

### Between domain areas

- One area uses another by calling its use cases, and holds the other area's aggregates by ID.
- A domain type moves to `kernel` when a second area needs it, and stays in its area until then. `kernel` holds value objects only, with no rule that belongs to a single area: the supported language and the country, which the setup put there for the areas that come next, the e-mail address, which moved there when the login of `access` needed the rule `consultancy` keeps its users by, and whose one rule (`kernel.IsEmailAddress`) the recipient of an e-mail and the addresses of the configuration are checked by too, the key in snake_case (`kernel.Key`), which the key of a login provider, the way in and the code of an answer of a service that a log line repeats follow, and the way in of a login (`kernel.WayIn`), which `access` logs in with and `audit` records, the rules of a required field left empty and of a text longer than its field, the error that lists the fields of an input that break a rule (see Errors), and the actor and the scopes, which the use cases of every area receive (see Inbound adapters). The telephone, the website, the currency, the time zone and the limited texts belong to `consultancy` alone, in its `domain`.

### Use cases

- Every operation an inbound adapter exposes is one use case in `application`. A Huma handler or an MCP tool parses its input, calls one use case and maps the result.
- A use case that only passes the call through is correct. So is the mapping between API types and domain types. Both are deliberate, not a middle man or duplication.
- A use case's inputs and outputs carry no type from Huma, the MCP SDK or any other adapter library.
- **Shape.** A use case is a type in `application`, named after what it does. The command that wires it builds it once, with the interfaces it needs, and an inbound adapter runs it with `Execute`. Those interfaces are declared in the same package.
- **Clock.** A use case takes the current time from a clock it receives when it is built, never from the machine, so a test decides what time it is. The command passes the clock of the machine, `clock.System` in `platform`.
- **Random values.** A use case that makes up a value, such as a setup code or the key of an object, reads it from a random source it receives when it is built (an `io.Reader`), as it receives its clock. The command passes `crypto/rand.Reader`, and a test passes a source it controls, so the value it expects is written in the test.
- **Writes.** One use case is one transaction, opened and closed through an interface. It loads the aggregate from its repository, which locks the row, calls the aggregate, and saves it whole. It may save several aggregates of its own area. An effect on another area runs after the commit and before the response, inside the request; one that must not be lost is a job instead. Two exceptions are declared. The audit log: an entry is written in the transaction of the operation it records, so an operation never succeeds without its record. The confirmation of the images of the setup: concluding the setup and registering an administrator confirm the images they were given before their transaction, because the setup checks everything it was given before it saves anything, so a refused image saves nothing and the transaction that locks the setup code never waits on the storage. A confirmation left by a setup that fails afterwards does no harm: a file confirmed again keeps its first confirmation. Inside the files area, the transaction that checks an upload copies the object of a file it confirms to the file's new key before the file names it, and the object the upload was sent to, refused or copied, is removed after the commit of that transaction. An effect on an external service whose result the caller waits to see, such as a streamed LLM answer or an on-demand translation, also runs after the commit, inside the request. Any other effect on an external service is a job. A job is saved inside the transaction (see Jobs).
- **Reads.** A query use case calls a read-only interface that returns data already shaped for the caller. It builds no aggregate.

### Inbound adapters

- **REST is complete for the screens.** Every use case that a person reaches from a screen has a REST operation. A use case that only a job or a command of the binary runs has none. Streaming to the browser is a Server-Sent Events operation in `adapters/rest`, added with the first use case that needs it.
- **A REST operation.** The `adapters/rest` package of an area declares the types of its bodies and adds each operation with `restapi.Register`: the path, the handler, and the broken rules the operation answers with under the status of each. The command that serves registers every area in one function, `mountREST` in `apps/api/cmd/api`, where each use case is wired, and the spec is generated from that function. So an operation that is served is in the spec.
- **MCP is a chosen subset.** A use case becomes an MCP tool only when an agent has a real use for it. MCP exposes tools only.
- **One tool, one use case.** A tool is designed for what the agent is trying to do, not copied from a REST operation. When it needs a composed result or a different shape, that is a new use case.
- **Authentication in the adapter, authorization in the use case.** The inbound adapter authenticates the caller (a session or an access token) and passes the actor to the use case: the user, the scopes, and the access token when there is one. The use case checks the scopes. A session carries the scopes of its user's roles, and an access token carries its own, never more than those roles grant (see Users and roles). The actor type lives in `kernel`: `kernel.Actor`, the identifier of the user and the scopes they act with (`Holds` checks one). A REST operation that needs a user is added with `restapi.RegisterForUser`: before its input is read, the REST adapter asks the `Actors` it was given (`AuthenticateWith`, wired in `mountREST` to `FindActor` of `access`, which reads the session and the user with their roles on every request), answers 401 `unauthorized` in the error format when nobody is logged in, and hands the actor to the handler, which passes it to its use case. The spec of such an operation declares the 401. An operation that needs no user, such as the setup and the login itself, is added with `restapi.Register`.
- **MCP language.** Tool names and descriptions are in English. Configuration content is returned in the preferred language of the access token's user.
- **Credentials over MCP.** A tool that returns the value of a credential requires the scope for reading credential values, returns one credential per call, and its use case records the read. No listing returns a value.
- **Nothing ahead of need.** No package, mount point or abstraction is written for an inbound adapter that does not exist.

### API conventions

These apply to REST. Names, identifiers, lists and error codes apply to MCP tools as well.

- **Names.** `snake_case` in bodies, query parameters, error fields and tool arguments.
- **Identifiers.** An identifier leaves the backend as a type prefix plus the Sqids encoding of the integer, at least 8 characters: `prj_Xk9fQ2mB`. An inbound adapter rejects a wrong prefix, decodes on the way in and encodes on the way out. `platform/identifier` holds the encoder and the prefix and alphabet of each aggregate type (the file is `fil_`); neither ever changes. A body declares the form of an identifier with the pattern of its type, so the contract refuses a wrong prefix; one that decodes to no number is answered as a record that does not exist. The integer appears in no response.
- **Prefix.** Everything the binary serves to the outside is under `/api`: the REST operations, MCP at `/api/mcp`, the River panel at `/api/river`, the login operations and the bounce notice. Caddy forwards no other path, except `/up` (see Health).
- **Documentation page.** Huma's documentation page is turned off, because it loads JavaScript from another origin. The API serves no copy of the spec and no schema either. The committed spec, `apps/api/openapi.yaml`, is the reference.
- **No `$schema`.** Huma's schema links are turned off: they add to every body a `$schema` property whose value repeats the host the caller named. No body carries a property the Go type does not declare.
- **Paths.** Resources are plural nouns. A state change is a `POST` ending in the verb of its use case: `POST /projects/{id}/cancel`. A resource that exists once has a singular path: the health report, the setup, the session of whoever is logged in and the state of its login, the consultancy and the user's own profile. The plural-path lint lists them, in `apps/api/tools/speclint/ruleset.yaml`: a singular path joins the list with the operation that creates it. The two navigations of a login through a provider are the one `GET` that ends in what it does (`/start`, `/callback`): the browser is sent there, so they cannot be a `POST`, and the ruleset lists them too.
- **Editing details.** A `PUT` with every editable field required. No `PATCH`. A narrower edit that an agent needs is its own use case.
- **Input.** Unknown body properties and unknown query parameters are rejected. The callback of a login provider is the one exception: the provider writes its query and may add parameters of its own, so it skips the check of its parameters (`SkipValidateParams`) and its use case judges what it reads.
- **Status.** 201 with `Location` for a creation, 202 for asynchronous work, 400 for malformed input, 422 for failed validation or a broken business rule, 409 for a conflict. A 201 leaves `Location` out while the resource it created has no address of its own, as the file of an upload and the user of the way back in have none yet; the header arrives with the `GET` of that resource.
- **Lists.** An envelope with an opaque cursor. `limit` has a default and a ceiling, sorting accepts only listed fields, filters are query parameters, the total is computed only when asked for. No field selection.
- **Single resource.** It is the root of the body. No `success` wrapper.
- **Allowed actions.** When a screen or a tool needs to know what can be done now, the query use case returns `allowed_actions`. The client never derives it from the status.
- **Values.** Timestamps in ISO 8601 UTC. Business dates without a time. Money as a decimal string beside its currency, never a float. Enums as lowercase strings. A country is its ISO 3166-1 two-letter code, a currency its ISO 4217 code and a time zone its IANA name; the browser writes their names in the user's language. The backend checks each against a list it carries beside the value object, whose file names its source: the countries in `kernel`, the currencies and the time zones in `consultancy/domain`.
- **Limits.** A size limit and a read timeout on request bodies: one megabyte, sent within five seconds, set where the REST adapter is mounted. A timeout on every outbound call, and a retry only when the call is idempotent.
- **Sessions.** A request authenticated by the session cookie that changes state must come from the app's own origin. `platform/session` refuses, with 403 `forbidden` in the error format and before the session is read, a request under `/api` with any method but `GET`, `HEAD` and `OPTIONS` that carries the cookie `session` when its `Origin` header is present and differs from `APP_ORIGIN`, or when it has no `Origin` and its `Sec-Fetch-Site` says `cross-site`. A request with no cookie is not checked: the bounce webhook carries none, nor will an access token. A client that sends neither header, such as `curl`, is no page of another site, and passes.
- **Compression and caching.** The API compresses no response and sets no cache header. Caddy does both (see Delivery).
- **Credential values.** A response that carries the value of a credential repeats nothing the caller sent in the request, because responses are compressed (see Delivery).
- **Breaking changes.** Allowed when the web client and the MCP tools change in the same commit. The path carries no version.

### Login

The term login code is defined in `GLOSSARY.md`. The reasons are in ADR-0011.

- **Two ways in.** A login provider (Google, Microsoft, GitHub) or a login code sent by e-mail. Both end in the same session, and both accept registered users only.
- **Login code.** Six digits, valid for 10 minutes, usable once, and only in the browser that asked for it. The third wrong attempt ends it.
- **Nothing stored.** The code is computed with HMAC from a key in the configuration and a random value stored with the request for the code. The job that sends the e-mail and the operation that checks the code compute it again, and the check compares in constant time. The code is in no table, job argument, log or span.
- **Same answer.** Asking for a code answers the same way whether or not the e-mail belongs to a user, past the limits too. For an unknown e-mail nothing is sent. Every ask runs one transaction that counts it, so the time of the answer differs little: an ask for a user also saves the request, the e-mail and its job, a few statements more, which ADR-0011 records as the gap that remains.
- **Operations.** Both live under the session of the browser: `POST /api/session/login_code` asks for a code and answers 202 with no body, and `POST /api/session/login_code/check` checks one and answers 204, with the cookie set again. The e-mail and the code travel in the body. `GET /api/session` answers the logged-in user (name, e-mail, the URL of their photo when they have one, preferred language, the keys of the roles, an enum of the two keys of the setup, which changes when a role a person creates, with a name and no key, arrives with the users, ADR-0026), and `POST /api/session/log_out` ends the session, whether a user is logged in to it or not. `GET /api/session/login` answers the state of the login, which needs no session: the login providers turned on, with their key and display name, and the e-mail the last login through a provider in this browser was refused for; reading it forgets nothing.
- **Checking a code.** The request is read from the session of the browser, and locked. The right code, while its request works, ends the request and logs its user in. Anything else is a wrong attempt, answered the same way whatever the cause, a wrong code or a request ended, expired or used: 422 `login_code_invalid` with `attempts_left`, and at the third `login_code_ended`, which ends the request. A browser that holds no request, because it asked for an e-mail of no active user or asked for none, is answered as a request that no longer works, its attempts counted in its session, so a code typed tells nothing of the e-mail. The right code of a user deactivated since they asked ends the request and answers `login_code_ended`. A code that is not six digits does not follow the contract (`validation_failed`) and is no attempt.
- **Logging in.** Every way in ends in `LogIn` of `access/application`, which the provider login reuses: in one transaction the way in finds its user, the user is checked to be active, and the audit log records `logged_in`, acted by the user on themselves, with the way in (`login_code` or the key of the provider) in its column `way_in`; once it commits, the session token is renewed, so a token known before the login opens nothing, and the session keeps the user and forgets what the login kept. Nothing is recorded for a wrong code or a logout.
- **Through a provider.** `access/adapters/oidc` is the one implementation, for every provider that issues an ID token: it discovers the provider at the first login through it, and again at the next one while it fails, so the API starts whether the provider answers or not, and holds no lock while it calls the provider, so one that hangs keeps waiting only the login that called it; it runs the authorization code flow with PKCE (S256), a state and a nonce, each 32 random bytes from the random source of the use case, and validates the ID token with `coreos/go-oidc/v3` (signature, issuer, client, expiry) and its nonce, in constant time. The e-mail is taken from the token only when `email_verified` is true. A provider is a key (snake_case, 64 at most, never `login_code`), a display name, an issuer URL, a client and its secret, and its redirect URL is `APP_ORIGIN` followed by `/api/session/login_providers/<key>/callback`. `GET /api/session/login_providers/{key}/start?return_to=` keeps the state, the nonce, the verifier and the path to return to in the session and answers 302 to the provider; `GET /api/session/login_providers/{key}/callback` takes them from the session, once, whatever comes of it. A callback that brings another state, or reaches a browser that started nothing, logs nobody in. The user is found by the provider link of the account (provider and subject) or else as the active user with the verified e-mail, and the account is then linked: a user has one link at each provider, so another account of that provider with their e-mail logs nobody in, and an e-mail that changes hands at the provider never logs someone else in. A login through a provider asks for a verified e-mail even when the account is linked. Then `LogIn`, with the key of the provider as the way in. The callback answers 303: to the path to return to, which is a path of the app or else its root (never `//`, a backslash, a scheme, a host or a path under `/api`); to `/login?notice=no_active_user` for a verified e-mail of no active user, unknown or deactivated, whose e-mail the session keeps for the login state; to `/login?notice=another_account_linked` for an account not linked whose verified e-mail is the one of a user who has another account of that provider linked, who logs in with that account or with a login code; and to `/login?notice=login_not_concluded` for anything else, a cancel or an error at the provider, a code it refuses, an e-mail it does not say is verified. All keep `return_to` when it is not the root. Two first logins of one account at once both find it unlinked: the link is inserted with `ON CONFLICT DO NOTHING`, so the second waits for the first, saves nothing, and reads the link the first saved, in its own transaction. A login through a provider has no time limit of its own: what the start keeps stays in the session until the next start replaces it, the callback takes it, or the session ends, and the code at the provider expires. Any outcome but a refusal for an e-mail forgets the refusal before it, and a login forgets it too. A key of no provider turned on answers 404 `not_found`. A failure of the provider writes one line, at `warn`, with the key and the step, and the status and the OAuth code of the answer, never the code, a token, the verifier, the secret or an e-mail; each call to the provider is a span with its method, its host and its status, and no `traceparent` goes with it.
- **Limits per address.** One code a minute and five an hour for each typed e-mail address, whether or not it belongs to a user, counted in PostgreSQL by the use case on the asks for that address (`login_code_asks`). Past them the operation answers 422 with `login_code_limit_reached` for every address alike, and changes nothing: a refused ask is not counted. The table keeps no address: an ask is the HMAC-SHA256 of the address, read in lowercase, under a key derived from the key of the login code (`LoginCodeKey.AskedAddress` of `access/domain`), and when it was made; the daily cleanup removes it an hour later, once no limit counts it. Two asks for one address at once are counted one after the other: the first step of the transaction takes an advisory lock keyed by the address, so the second meets the limit of a minute. A new code ends the previous one.
- **Limit per IP address.** Set in Caddy, on the two operations of the login code only (see Delivery).
- **Local provider.** A generic OIDC provider, turned on by configuration alone and pointed at an identity container (Dex), for development and tests. The production and staging Compose files do not declare it. When it is on, it shows on the login screen and in a log line at the start of the process (`the local login provider is on`, with its key, display name and issuer); the health report, which the next effort builds, shows it too. Its five variables, `LOCAL_LOGIN_PROVIDER_KEY`, `_DISPLAY_NAME`, `_ISSUER_URL`, `_CLIENT_ID` and `_CLIENT_SECRET`, turn it on together; some set and others empty stop the API at its start. Dex is the `dex` service of `compose.yaml`, published on `DEX_PORT` of the machine, with the issuer `http://localhost:<DEX_PORT>`, so the browser and the API on the machine reach one address; its configuration is a config of the Compose file, with one static client, read from the same variables, and the two people of the seed at the default mailbox, whose fictitious passwords the README lists. Those people are written in four places, which change together: the configuration of Dex in `compose.yaml`, `access/adapters/oidc/oidctest`, `apps/web/e2e/system.ts` and the README; the configuration of Dex does not follow `SEED_MAILBOX`. Its storage is memory, and it logs at `warn`, because at `info` it writes the e-mail and the name of each person who logs in (see Privacy in Observability). The tests of `oidc` and of the binary drive its login page over HTTP (`access/adapters/oidc/oidctest`), and the end-to-end test in a browser.
- **Session.** It ends after 7 days without use and, in any case, 30 days after the login. Deactivating a user ends their sessions: each request reads the user of its session, and a user deactivated or gone has their session destroyed and gets 401. A session of theirs that no request reads ends at its next request, or when it expires: nothing ends them at the moment of the deactivation.
- **Where the session lives.** `platform/session`: scs keeps it in the `sessions` table through pgxstore, which stores the SHA-256 of the token and never the token. The cookie, `session`, is opaque, `HttpOnly`, `SameSite=Lax` and `Secure` when `APP_ORIGIN` is `https`. A session exists before the login, for what the login keeps between two requests, and only a request under `/api` loads one: liveness and readiness touch no session. An area keeps what it writes in the session behind an interface its `application` declares, which its `rest` adapter implements; asking for a code writes the session the same way for every e-mail, so the cookie of the answer tells nothing.

### Setup

The terms setup and setup code are defined in `GLOSSARY.md`. The reasons are in ADR-0024.

- **Command.** `issue-setup-code`, a command of the API binary, prints a setup code and says what it opens: the first setup, or the registration of a new administrator when the setup is already concluded. It also prints when the code stops working and the address of the setup, which is the origin of the app, taken from configuration, followed by `/setup`.
- **Setup code.** Twenty letters and digits in four groups, with no character that reads like another. It lasts one hour, and a new one ends the one before. Only its SHA-256 hash is stored. The code is in no table, log or span.
- **In the body.** The screen keeps the code in memory and sends it in the body of each request of the setup, never in a path, a query parameter or a header.
- **No limit of its own.** A wrong code does not end the setup code. Caddy limits the checking for each IP address (see Delivery).
- **One operation.** Concluding saves the consultancy, the administrator and the two first roles in one transaction and ends the setup code. An image chosen in the setup is uploaded under the setup code and confirmed when the setup is concluded.
- **Open or closed.** One operation with no session tells whether the setup is concluded and whether a setup code is waiting. Until the setup is concluded every address of the app leads to it. Afterwards its address leads to the login screen, except while a setup code is waiting.
- **Way back in.** A setup code issued after the setup is concluded registers a new administrator and changes no existing user. An e-mail that belongs to a user is refused.

### Users and roles

The terms user, role, administrator, developer and scope are defined in `GLOSSARY.md`. The reasons are in ADR-0026.

- **Where.** The `consultancy` area holds the consultancy, the users, the roles and the setup code, because the setup saves them together.
- **Scopes in the code.** The list of scopes is fixed in the code, in `kernel` (`kernel.EveryScope`, empty until the first use case that checks one): read or write in one area of the system. A scope arrives with the use case that checks it. `consultancy/domain` says what each role grants (`ScopesOf`), and `FindActiveUserByID` of `consultancy/application` gives the user of a session with the scopes of their roles.
- **Roles in the database.** A role is a record with a set of scopes. A user has at least one role, and their scopes are those of all their roles, read on every request.
- **Administrator.** The administrator role grants every scope and cannot be changed or removed. A use case refuses to take it from the last administrator who can log in, and to deactivate that user.
- **No scope.** Reading the name and the logo of the consultancy and editing one's own profile ask only for a logged-in user. The read of the consultancy is `GET /api/consultancy` (`tell_consultancy`), its name and the URL of its logo, registered with `restapi.RegisterForUser`. A user changes their own name, telephone, photo and preferred language; the rest is changed by whoever holds the write scope of users.
- **Deactivated, never removed.** A deactivated user cannot log in and keeps their record.
- **E-mail.** It is stored in lowercase and is unique among users.
- **Identifiers of people and companies.** A tax ID and a national ID are stored as typed, beside their country, with no rule for any country.

### Commands

- **One binary.** `apps/api/cmd/api` builds it. With no command it serves the API; `migrate` applies the pending migrations; `issue-setup-code` issues a setup code (see Setup); `worker` runs the jobs (see Jobs); `probe-readiness` asks the API whether it is ready (see Health). Commands are parsed with the standard library, and each one is a file of that package. The image of the API holds the binary alone (see Publishing in Pipeline), and the servers run each command from it.
- **The seed apart.** `apps/api/cmd/seed` is a second `main` package of the module, with the `seed` command alone, built into its own image (see Seed in Environments). It keeps the rules below as the commands of the API binary do.
- **Wired by hand.** A command wires its own dependencies, in its file: it loads the configuration, builds the pool and the adapters, and passes each part to what uses it. No injection library is used, and no other package builds a dependency for another.
- **Testable whole.** A command receives its arguments, the environment and its outputs, and returns the exit status: 0 when it succeeded, 1 when it failed, 2 when it was not understood. A test runs it as the process would.
- **The tool of the environments.** `apps/api/tools/environment` is a `main` package of the module, run with `go run` and built into no image: on the maintainer's machine (`task staging:on`, `task staging:off`, `task staging:status`), and in the pipeline, whose publish job runs `go run ./tools/environment publish staging` (see Publishing in Pipeline). It keeps the rules above. Its commands also receive what they reach outside the process, the provider, readiness, GitHub, the server a publish works on, its app and the clock among them, so that its tests give them in-memory implementations.

### Configuration

- **Environment variables only.** The binary reads its configuration from environment variables, once, when it starts, into one typed struct in `platform` (caarlos0/env). No other code reads the environment: `config.Environment` reads the variables for the command that starts, and for the support of the tests (`databasetest`), which needs the connections of the databases it creates. The OpenTelemetry SDK is the one exception: it reads the `OTEL_` variables its specification defines, such as the headers and the timeout of an export. The address it exports to is read by the configuration and passed to it.
- **Fails at the start.** A missing or invalid value stops the process before it serves anything, with a line that names the variable and never its value.
- **By command.** A command requires only the variables it uses. The API never receives the connection of the migration role, and `migrate` never receives the one of the API role.
- **Secrets do not print.** A value that carries a secret, such as a connection URL, is a type that prints as a placeholder. It keeps that type until the adapter hands it to the library that uses it, at the last moment: the settings of the pool and of the storage hold the type, not the text. A secret a domain computes with keeps a secret type of that domain, which prints as a placeholder too: the command turns the key of the login code (`config.Key`) into `access/domain.LoginCodeKey` at once, and only the HMACs of `access/domain` read its bytes. The tool of the environments keeps the token of the provider, the password of Mailpit and the private keys of a server as `config.Secret` too, until the file, the template or the program that uses each.
- **No file.** The binary reads no `.env` and no configuration file. Compose and Task load the variables. The tool of the environments, which is no part of the binary, is the one exception: when the process has no token of the provider, or no password of the Mailpit of staging, it reads it from the `.env` of the main checkout, also when it runs in a worktree, whose own `.env` Task loaded instead; and the first time staging is turned on it appends that password to the file, creating it when there is none and leaving every other line as it was (see Servers in Environments).
- **Passed in.** A domain area receives the values it needs when it is built. `application` and `domain` never import the configuration struct.
- **Example.** Every variable of the system is listed in the committed `.env.example`, with no real secret. The variables of the tool of the environments are not, because they live on the maintainer's machine alone and the publish asks a staging value for every name listed there: `HETZNER_TOKEN`, the token of the provider, and `STAGING_MAILPIT_PASSWORD`, the password of the Mailpit of staging, which the tool writes itself.

### Telemetry

How the telemetry is collected, kept and shown is in Observability.

- **Nothing sensitive.** Logs, spans and metric labels never carry personal data, the value of a credential, a login code, an access token or a session cookie. A test proves it for logs.
- **Where.** Spans, metrics and logs are created in `adapters` and `platform` only. `application` and `domain` create none: a use case returns a typed error, and the inbound adapter records it.
- **Spans.** An inbound adapter opens the span: of a request, of an MCP tool call (in a middleware of the MCP adapter), of a job (see Jobs). Outbound adapters are instrumented: pgx (see Database), the AWS SDK through its OpenTelemetry adapter (see Files) and the HTTP clients. The span of a request is opened by `telemetry.Requests` and carries the method, the route and the status, never a path, a query string or a header. Liveness and readiness open none, and nothing under them does, their queries included: a probe every few seconds would fill the traces. `telemetry.Requests` marks them, by the paths `health.Paths` gives, and the tracer drops every span started under them; a failed one still writes its line. A request under `/api` always opens its span.
- **Actor.** A span and a log line carry the public identifiers of the user, the access token, the client and the project. A metric label carries none of them.
- **Logs.** JSON lines on the container output, each with the trace identifier of its request, as `trace_id`. A line written outside a request, such as the start of the process, has none. The logs go to the error output of the process, and the standard output is left to what a command prints. Nothing is sent as OTLP logs. The level comes from configuration (`LOG_LEVEL`) and is `info` by default. A command builds its logger with `telemetry.NewLogger` and writes with the context of the request, and makes it the logger of the process with `telemetry.RouteTheStandardLogger`, so a library that writes with the standard library's logger, or with the default logger of slog, writes a JSON line of it at `info`: every command does, `migrate`, `issue-setup-code` and the seed included.
- **Plain lines.** Two commands write plain text, because a person runs them and reads what they print: `migrate` prints each migration it applied, and `issue-setup-code` prints the setup code, on the standard output, and each writes its failure as one line on the error output (`api: ...`). The logger of `issue-setup-code` writes only a failed export of traces. `probe-readiness`, which Docker runs and keeps the output of, prints nothing when the API is ready and one plain line on the error output when it is not. A configuration that is not valid is one plain line for every command, written before a logger can exist, because the level of the logger is part of the configuration.
- **What is logged.** A 5xx or a panic with its raw error, a failed outbound call, the start and the stop of the process, the requests cut by a shutdown. A successful request and a 4xx write no line. No line carries a body, a query string or a header.
- **The line of a request.** `telemetry.Requests`, which wraps the handler of the API, writes it when the request ends: the method, the route, the status and the raw error. Whatever answers a 5xx hands its raw error to `telemetry.RecordFailure` and writes no line of its own. A line names the route, never the path.
- **Metrics.** HTTP requests, the Go runtime, the database pool and the use of LLM providers. No business metric until a case needs one.
- **Traces.** Every trace is kept. Nothing is sampled, except the probes above, which record no span.
- **LLM calls.** A call records the provider, the model, the input and output tokens, the duration and the outcome, on its span and as metrics, under the `gen_ai` names of the OpenTelemetry semantic conventions. It never records the prompt or the answer.
- **Trace identifier.** The `trace_id` in an error body is the trace identifier of the request. An incoming `traceparent` is honoured. An outbound call carries it only to the agents of ADR-0004, never to an LLM provider, a login provider or the e-mail service.
- **Export.** Traces and metrics leave over OTLP/HTTP, to an address taken from configuration: `OTEL_EXPORTER_OTLP_ENDPOINT`, the base URL of the receiver. With no address, nothing is exported and the telemetry collector is `not_configured`. A span still gets its trace identifier, so the `trace_id` of a log line and of an error body is real. A failed export is logged and stops nothing. `platform` wraps the metric exporter and keeps the time and the error of the last export (see Health).
- **Tests.** Tests run with no telemetry. A test that reads the spans a command exported gives it the address of the collector of `telemetrytest`.
- **Test of sensitive values.** `sensitive_test.go` in `apps/api/cmd/api` runs the commands of the binary with a sensitive value in each place one goes by, then searches the logs and the exported spans for each value. Code that handles a new kind of sensitive value adds it there; the file says how.
- **Audit log.** A sensitive operation (reading the value of a credential, a login, creating or revoking an access token, issuing a setup code, concluding a setup, changing the roles of a user, deactivating or reactivating a user) writes to the audit log from its use case. The audit log is append-only and is not telemetry.

### Health

The terms system health, part and health report are defined in `GLOSSARY.md`.

- **Liveness.** `GET /livez` answers 200 and touches no part.
- **Readiness.** `GET /readyz` answers 200 when PostgreSQL answers and the database schema is the version the binary expects, and 503 otherwise. The version the binary expects is the newest migration it carries. The check runs under the timeout of a check (see Checks). It feeds the Compose health check of the API container, `[CMD, /api, probe-readiness]`: the image has no shell and no HTTP client, so the binary asks itself, at `API_LISTEN_ADDRESS`, with the loopback address in place of a host that names every interface, and succeeds only on a 200.
- **Outside the contract.** Liveness and readiness live in `platform`, outside `/api` and outside the OpenAPI spec. They need no authentication and answer with the status code alone. Caddy forwards only `/api`, so they are reachable only inside the Compose network.
- **One public path.** Caddy answers `/up` with the status code of readiness and no body, for the monitor outside the server (see Observability). It is the only way to readiness from outside.
- **Parts.** PostgreSQL is the only essential part. The file storage, the LLM provider, the telemetry collector, the job worker and the e-mail server are optional: one of them down makes the system `degraded`, never unready. The file storage is required, though: the API refuses to start without its configuration (the six `FILE_STORAGE_` variables), so it is never `not_configured`. Any other optional part with no configuration is `not_configured` and does not degrade the system.
- **Health report.** `GET /api/health` is a query use case in the system health area, which has no aggregate. It follows the API conventions, with a singular path because there is one health report. It always answers 200, with the system health in the body: `operational`, `degraded` or `down`.
- **Checks.** They run when the health report is asked for, in parallel, each under a timeout taken from configuration (2 seconds by default). Nothing is checked in the background and nothing is stored. The LLM provider is checked by a call that spends no tokens, such as listing its models. The telemetry collector is not probed: its state is the last export of metrics, and it is `down` when that export failed or is overdue. The job worker is checked in the database, through the leader record River keeps there. The e-mail server is checked by a call that sends nothing.
- **Content.** For each part: its name, whether it is essential or optional, its state (`operational`, `down` or `not_configured`), the response time, the time of the check and, when it is down, a stable error code. The raw error goes to the logs only. The health report also carries the version, which is the number in `VERSION` (see Releases), the commit, the build date, the time the process started, the schema version applied and the one expected, and the login providers that are turned on.
- **Access.** The health report asks for the read scope of the system health area, from a session or from an access token. Only the administrator role grants it at first.
- **When.** Liveness and readiness are written with the first backend code. The health report needs administrators, who exist once the setup is built (ADR-0024). Nothing provisional shows the state of the optional parts until then.
- **Not built.** An MCP tool for the health report, a check of the login providers, an operation per part, and history, which would be a periodic job (see Jobs). Alerts come from Grafana (see Observability).

### Shutdown

The API stops without cutting work short. It runs as one instance, so a deploy is a restart, and a few seconds without service are accepted.

- **Signal.** `SIGTERM` or `SIGINT` starts the shutdown. A second signal exits at once, with status 1 and without waiting for anything.
- **Ownership.** `platform` owns the process lifecycle and exports the shutdown notice. No domain area handles a signal. The notice is the context every command receives: it ends when the shutdown starts. A command that runs to its end passes it on, so that a signal stops it. The command that serves only watches it, and never derives the context of a request or of a query from it.
- **Health.** Readiness fails from the signal on. Liveness keeps answering, on a connection that was already open.
- **Requests.** New connections are refused. A connection that was already open is still answered while the drain lasts, and is closed when no request is in flight. A request in flight, an MCP tool call included, has the drain deadline to finish: 20 seconds, taken from configuration (`SHUTDOWN_DRAIN_DEADLINE`). No timeout on a non-streaming outbound call exceeds the drain deadline.
- **Streams.** A stream closes when the shutdown starts: an LLM answer being streamed, a progress stream, the MCP listening stream. A streaming operation watches the shutdown notice. An SSE operation first sends a final error event with the code `service_restarting`, carrying the fields of the error format. The MCP listening stream closes with no event, because MCP defines its own reconnection.
- **Deadline reached.** The context of each remaining request is cancelled, which rolls back its transaction, and the API logs how many requests were cut.
- **Exit.** The API then closes its connections to PostgreSQL, flushes telemetry under a short deadline of its own and exits: with status zero when every request finished, non-zero when one was cut. Closing the connections waits for PostgreSQL, for 15 seconds at most when it does not answer: after a query is cancelled, pgx asks PostgreSQL to cancel it and waits for the answer. So the stop grace period of the API container, `stop_grace_period` in the Compose files of the servers, is longer than the drain deadline plus those 15 seconds and the flush of telemetry, whose deadline is 5 seconds. Docker gives 10 seconds by default and then kills the process. `deploy/compose.base.yaml` gives the API and the worker 45 seconds.
- **No goroutine outlives its request.** Work that must survive the request is a job (see Jobs). The shutdown of the API does not wait for it.
- **Test.** A test in `platform` holds a slow request and a stream open, sends the signal and checks that the request finishes, the stream receives its final event, a new connection is refused, and a reached deadline cancels what is left. It is in `platform/lifecycle`; its part about the stream is written with the first stream. The tests of the command check the same through the binary, with the exit status. They hand the signal to the code, so they run on every system; on Unix two more run the binary as a process and send it `SIGINT` and `SIGTERM`.

### Domain modelling

These rules apply inside `domain`. API types and database rows in `adapters` are plain structs.

- **Aggregate.** It guards its invariants. State changes through a method named after the business operation, and fields are unexported. Callers tell the aggregate what to do; they do not read its state to decide for it. Read accessors exist for adapters to persist and map.
- **Status.** An aggregate with a lifecycle has a status: a value object listing a fixed set of values. One transition table in `domain` lists, for each operation, the statuses it can start from and the status it leads to. The method named after the operation asks the table, and an operation the table does not allow is a typed error. A function in `domain` returns the operations a status allows; the query use case calls it for `allowed_actions` and keeps the ones the actor may perform. A rule that needs more than the status stays in the method.
- **Steps.** Work in progress is a status. A record processed in steps has one status for each step, and each step is a job that moves it to the next one (see Jobs).
- **No status.** A record with two conditions keeps a field. A condition that follows from time and counters, such as an expired login code, is computed and never stored.
- **Value object.** A primitive becomes a value object when it carries a rule (validation, format, invariant) or is an identifier. It is built through a constructor that returns an error, and it is immutable. A collection with a rule of its own gets its own type on the same test. Free text and plain counters stay primitives.
- **Repository.** One per aggregate, declared in `application`. It reserves the next identifier from a database sequence, and the use case creates the aggregate with it, so an aggregate never exists without an identity. Loading an aggregate locks its row (`SELECT ... FOR UPDATE`) until the transaction ends, so two writes to the same aggregate run one after the other and the second sees what the first saved. A method of the repository that locks nothing serves two reads only: a use case that checks an aggregate and saves nothing of it, such as the ones that open the setup and start an upload of the setup, and a check before the transaction that the transaction repeats when it loads the aggregate with the lock, such as the setup code that concluding the setup checks before it confirms the images. A read for a screen goes through a query use case and locks nothing.
- **One call per collaborator.** An expression talks to the object it holds, and that object does the rest.

### Errors

A broken rule is a typed error with a stable code and its parameters, declared beside the rule in `domain` or `application`. Each inbound adapter translates it: REST into `application/problem+json` with `code`, `params` and `trace_id`, MCP into a tool error carrying the same stable code and English message.

Every REST failure uses that format, including unknown routes, wrong methods and panics, which need their own handlers because they bypass Huma's error hook. No response carries a stack trace, SQL or an internal name.

- **A broken rule.** Its error type has `Code() string`, which returns the stable code, and a text in English that is safe to answer with. One with parameters also has `Params() map[string]any`. `SetupCodeInvalidError` in `consultancy/domain` is the model.
- **A rule about a field.** A rule one value of the input breaks, such as a country outside its list or a text over its limit, is a broken rule declared beside its value object. A use case checks every field before it saves anything and returns the ones that break a rule together, in a `kernel.InvalidFieldsError` that names each field as its input is structured: `consultancy.address.city`. `ConcludeSetup` in `consultancy/application` is the model.
- **Status.** A REST operation lists the broken rules it answers with, under the status of each: 422 for a broken rule, 409 for a conflict. A rule about a field is listed under 422 with `restapi.InField`. A broken rule it does not list answers 500, so the spec names every code an operation answers with.
- **Body.** `title`, `status`, `detail` (the text in English), `code`, `params` and `trace_id`. A failed validation adds `errors`, with one entry for each field: its `location` (`body.name`, `query.limit`, `path.id`), a `code` (`required`, `invalid`, or the code of the rule about the field), its `params` and a `message` in English. Fields that break a rule of the domain answer the same way, with `validation_failed`, so a screen shows each on its field. An empty text where the contract asks at least one character is `required`. No entry repeats the value that was refused. `type` is left out, which RFC 9457 reads as its default.
- **The contract checks first.** A body declares what a client can check before it sends: a required text has at least one character, the limits of a text and the forms of an e-mail, a telephone and a website. The body type takes them from the domain, in its `TransformSchema`, so the two never drift. A list a value must be on is checked by the domain alone.
- **Other failures.** A failure that is not a broken rule takes its code from the name of its status: `bad_request`, `not_found`, `method_not_allowed`, `request_entity_too_large`, `too_many_requests`, `internal_server_error`. The one exception is a request that does not follow the contract, which is `validation_failed`. Caddy names the failures it answers for the API the same way, in the same format (see Delivery): `too_many_requests`, and `bad_gateway`, `service_unavailable` and `gateway_timeout` when the API cannot be reached.
- **5xx.** The answer carries the title, the status, the code and the `trace_id`, and nothing else. The raw error goes to the logs (see Telemetry).

### Database

PostgreSQL 18 is the only database, with one schema.

- **Access.** A `postgres` adapter runs SQL through pgx, version 5.13.0 or later. Its queries are SQL files inside the adapter package, and sqlc generates the Go functions and the row structs from them. The generated code is committed and never edited.
- **Generating.** `apps/api/sqlc.yaml` has one entry for each `postgres` adapter, and reads the schema from the migrations. `task api:generate` writes the code into the adapter package, in the files named `sqlc_*.go` and `*.sql.go`. `task api:generate:check` fails when the committed code is stale, and `task check`, `task check:quick` and the pipeline run it.
- **Transactions.** `platform` opens the transaction of a use case and carries it in the context (`database.Transactions`). A `postgres` adapter keeps no connection: every method runs its SQL on what the context carries (`database.Within`), the transaction when there is one and the pool otherwise. So everything a use case writes, through the adapters of any area, is in its one transaction.
- **Parameters.** A value reaches SQL as a parameter. No value is concatenated into SQL text.
- **Lists.** A list is fixed SQL: an optional filter is `(parameter IS NULL OR column = parameter)`, and each allowed sort is its own query. A list that does not fit is written by hand on pgx, with a comment saying why.
- **Pool.** One pgxpool in `platform`, shared by every domain area and by the session store. Its maximum size, the statement timeout and the idle-in-transaction timeout come from configuration. The session store runs no cleanup of its own (pgxstore with an interval of zero): the daily cleanup of the worker removes the sessions that ended (see Jobs), so the API runs no query outside a request.
- **Migrations.** Numbered SQL files in `apps/api/migrations`, with zero-padded numbers (`00001_create_projects.sql`), embedded in the binary and applied by goose with its lock turned on. A migration has no down part. A mistake is fixed by a new migration, and a migration that has run outside a development machine is never edited.
- **Running them.** The `migrate` command of the API binary applies the pending migrations, as its own step before the API starts. The API never changes the schema.
- **Roles.** The migration role, `migration`, owns the database and its tables. The API role, `api`, reads and writes rows; on the audit log table it can only insert and select. It creates no temporary table either: PostgreSQL gives every role that right on a new database, and a migration takes it from everyone, so it holds on every database the migrations build. The superuser creates both roles when the data directory is created. The system never logs in as the superuser; only the tests do, to create their temporary databases. The migration that creates a table grants the API role its permissions in the same file. A right on the database itself, such as the one to create temporary tables, is set by a migration too: it lives on the database and not in it, so a database created from a template does not inherit it (`databasetest` gives each copy the rights of its template), and the script of the roles runs only when the data directory is created. The monitoring role reads the statistics of the server (`pg_monitor`) and no table; only the telemetry collector uses it.
- **Tables.** Foreign keys cross domain areas. An identifier is a `bigint` from the sequence of its aggregate type. An instant is `timestamptz`, a business date is `date`, money is `numeric`, and a fixed set of values is `text` with a `CHECK` constraint listing the values the API uses.
- **Extensions.** `pg_stat_statements` only. PostgreSQL does not mark it as trusted, so a migration cannot create it: the superuser does, beside the roles. Another extension arrives in the migration of the first feature that needs it, which works for the ones PostgreSQL marks as trusted. One that the official PostgreSQL image does not ship starts with a new ADR.
- **Traces.** pgx is traced with otelpgx. The SQL text is recorded; query parameters never are.

### Jobs

Work that nobody waits for on a screen runs as a job. Jobs are stored in PostgreSQL by River.

- **Saving.** A use case saves a job through an interface it declares, inside its own transaction. The API saves jobs and never runs one. The `river` adapter implements that interface with `jobs.Saver` of `platform`, which saves in the transaction the context carries and refuses a context that carries none.
- **Running.** The `worker` command of the API binary runs the jobs, as its own service. It needs the database, the sender of e-mail (see E-mail) and the file storage, which it cleans up (see Files), and nothing of the API's listening address, and it stops at its start when a migration is pending, because it has no readiness to tell. A job worker lives in `adapters/river`: a `river.Worker` that reads the job's arguments and calls one use case. The command registers it in `mountJobs`, as `mountREST` registers the REST operations. `platform/jobs` is the model of its test, with River's `rivertest` in a temporary database.
- **Arguments.** A job's arguments are identifiers. They never carry personal data or the value of a credential, because the River panel and the telemetry show them.
- **Idempotent.** A job can run more than once. A second run changes nothing that the first one already did.
- **Attempts.** Five by default. A job kind that needs more attempts, or more time than River's default timeout, declares it. When the attempts run out, the domain record is marked as failed, and a person can ask for a new attempt.
- **Queues.** Light work and heavy work run in separate queues, `light` and `heavy`, each with a limit of concurrent jobs taken from configuration (`WORKER_LIGHT_QUEUE_MAX_JOBS`, `WORKER_HEAVY_QUEUE_MAX_JOBS`). A job kind names its queue in the `InsertOpts` of its arguments.
- **Periodic work.** A periodic job is declared in the worker, in `mountJobs`, with `jobs.AddPeriodic`, which registers its job worker and its interval together, and scheduled by River. Its arguments are the zero value of their type: it names no record. River keeps the schedule in the memory of the worker it elects, so a restart or a new election starts it over: the job also runs when the worker starts, so a worker restarted more often than the interval still runs it. It tolerates a missed run and a run more than it needed. Two run today: the cleanup of the file storage every hour (see Files), and the daily cleanup, once a day, so the database is reached for it no more often (the maintainer may move to a database billed by use): `clean_up_old_records`, whose worker in `access/adapters/river` runs `CleanUpOldRecords` of `access`, which removes the login code requests a day after they ended or expired, the asks for a login code an hour after they were made, the sessions that ended, and, through `CleanUpBounceNotices` of `email`, the bounce notices received more than 30 days before. Each removal is a statement of its own on the pool, and one that fails stops none of the others.
- **Answer.** An operation that starts a job answers 202 with the resource, whose status says the work is in progress.
- **Progress.** The use case of a job announces each change of status inside its transaction, through an interface it declares. The adapter sends the announcement with PostgreSQL's `NOTIFY`, and the API, on a connection that only listens, passes it to the progress stream (see Shutdown). The announcement says which record changed and nothing else. The screen reads the resource when the stream opens and at each announcement; an agent reads the resource.
- **Kept.** River deletes finished jobs. What must be kept is a domain record, never a job.
- **Shutdown.** The worker handles the signal as the API does. It stops taking jobs, gives the running ones a deadline taken from configuration (`WORKER_SHUTDOWN_DEADLINE`), then cancels them. A job works under the context River gives it, and a cancelled job returns the error of that context: River then makes it available again with its attempt given back, so it runs again later and uses up no attempt.
- **Tables.** River's tables come from SQL exported by the River CLI, one migration per version of River's `main` line, named `apply_river_main_migration_NNN`: `go tool -modfile=river.mod river migrate-get --line main --version N --up`, run in `apps/api`. Version 1, the table of River's own migrator, is left out. The exported SQL is kept as it is, with goose's `StatementBegin` and `StatementEnd` around each function body and `DO` block, and the grants after it. An update of River that changes its schema brings the new migration in the same commit. The worker uses the API role, which gets `SELECT`, `INSERT`, `UPDATE` and `DELETE` on each table, `USAGE` on its sequence and `MAINTAIN` on the jobs table, for River's reindexer.
- **Traces.** River is traced with its OpenTelemetry middleware, with trace propagation on, in the API and in the worker. The span of a run, `river.work/<kind>`, is a trace of its own with a link to the span that saved the job, inside the trace of the request. It records the kind, the queue, the attempt and the identifier of the job, never its arguments.
- **Panel.** The River panel is served by the API at `/api/river`, behind the session, only to an administrator, and outside the OpenAPI spec. Until the API serves it that way, it runs in development only: River UI's own container in `compose.yaml`, on `RIVER_PANEL_PORT`, reading the development database as the API role. Its image is updated with River.

### Files

Files live in S3 storage: Garage in development and staging, Backblaze B2 in production (see Environments).

- **Access.** An `s3` adapter uses the AWS SDK for Go v2 and only what the S3 API defines: no bucket notification, nothing that one server alone offers. Addresses are path-style. The calls are traced and carry no signature or key. No key of an object is in a log or a span either: the error of a call and the texts the SDK records on its span name a request by the address of the storage alone, because the rest of its URL holds a key, or a token of the listing made from one, and a key the storage repeats in its answer becomes `[key]`. What the SDK needed against Garage is in ADR-0017.
- **Bucket.** One private bucket. An object key is random (128 bits in hexadecimal) and never contains the file's name or an identifier.
- **Upload.** The use case checks the permission and the type and size declared, saves the file, and returns a URL signed for five minutes. The declared type and size are signed with it, so the browser sends a `PUT` whose `Content-Type` and `Content-Length` are the declared ones. The browser sends the bytes to the storage. A large file goes in parts, each with its own signed URL.
- **Confirmation.** The browser then tells the API that the upload ended. The use case checks the object in the storage, its size and its first bytes, and only then is the file available. An object over the size limit, or one whose type is not accepted, is removed. A file confirmed again keeps its first confirmation, and the storage is not read again.
- **Out of reach of its upload URL.** Confirming copies the object inside the storage (`CopyObject`) to a new random key that no URL was signed for, in the transaction that checks the file, before the file names the key. The object the upload was sent to is removed after the commit, so a `PUT` to that URL until it expires changes nothing the system reads. When that removal fails, the confirmation stands and the object is left behind for the job below (ADR-0017). This holds for every file, not only the images of the setup.
- **Images.** A PNG, a JPEG or a WebP of at most 2 MiB, told by the first bytes of the object. Nothing is resized or cropped. The rules are in `files/domain`, and the contract takes the types and the size from there.
- **Between areas.** The `files` area has the use cases that start and confirm an upload, and the one that signs a download, and no REST operation of its own yet; the use case of another area that allows an upload or shows a file calls them, after it checked the permission. Another area holds a file by its identifier.
- **Never confirmed.** A periodic job, every hour, removes the uploads that were never confirmed, and the objects no file names, which a failed removal or a confirmation that did not commit leaves behind: it lists the bucket. An upload never confirmed a day after it started is abandoned, and its file is deleted: nothing refers to it, and it records no business fact. Then each object more than a day old, by the time the storage says it was last written, is removed when no confirmed file names its key; this removes the object of an abandoned upload too. The files go first: a confirmation that holds one ends before it is deleted, or keeps it confirmed. A confirmed file is never touched: the copy a confirmation makes is written when it is made, so it is young while the confirmation commits, and named once it is old. The job runs in the light queue with a timeout of ten minutes, and its arguments are empty. A worker cleans up the bucket it is given as if its database were the only one that names objects there. In development the bucket is shared with the tests, so a worker removes, a day later, objects only a database of a test names, and no test runs the cleanup on the bucket of development: the end-to-end test, whose worker runs it, has a bucket of its own (see Tests in the frontend rules).
- **Download.** The use case checks the permission and returns a signed URL that expires in minutes. No file passes through the API, except the file of a credential (see Credentials). `SignDownload` of `files/application` signs a `GET` for the public host name of the storage, valid for 15 minutes, for a confirmed file only: the object of an upload never confirmed holds whatever its sender chose to send. It reads the key of the file without locking it, and `URLOf` signs a file that may be absent, a nil identifier, as the photo of a user or the logo of the consultancy, and answers no URL then. An image is shown from such a URL, embedded in the answer of the read that shows it: the photo of the user in who is logged in (`TellLoggedInUser` of `access`), the logo in the consultancy (`TellConsultancy` of `consultancy`). The app reads both again every ten minutes (`signedURLsRenewEvery` in `shared/api/storage.ts`), so the URL it shows always works. A download operation of its own, for a file other than an image, arrives with the first such file.
- **Address.** The browser reaches the storage at a public host name taken from configuration: a host name of its own in the Caddyfile of development and of staging, the provider's host name in production. The bucket accepts cross-origin requests from the app's origin only.
- **Processing.** A job reads a file from the storage and writes its result there. Nothing stays on the local disk after the job ends.

### Credentials

- **Encrypted by the API.** The value of a credential is encrypted with AES-256-GCM, from the standard library, before it reaches the repository. The key comes from configuration and is never in the database.
- **Key identifier.** Each row stores the identifier of the key that encrypted it. The configuration holds the key that encrypts and the older keys that still decrypt.
- **Bound to its credential.** The identifier of the credential is authenticated together with the value, so a value copied to another row does not decrypt.
- **Files.** A credential that is a file, such as an SSH key or a `.pfx` certificate, is a value: encrypted the same way and stored in PostgreSQL, under a small size limit taken from configuration. It goes through the API and never to the file storage.
- **Where.** `platform` holds the cipher, and the use case reaches it through an interface. Only the use case that reads a value decrypts it.

### E-mail

The system sends e-mail and receives none. The `email` area holds the record, the templates and the senders.

- **Record.** Every e-mail is a row: the recipient, the subject, its template and language, what it refers to (the kind and the identifier of a record of another area), its status, which is `queued`, `sent`, `failed` or `bounced`, the identifier the sender gave the message (for `smtp` the Message-ID the adapter sets, which Mailpit and any mail client show and search by, not the queue identifier the server answers; for `resend` the identifier its API answers), when it was sent, failed or bounced, and a stable code when it failed (`content_unavailable`, `sending_failed` or `delivery_failed`). `QueueEmail`, which another area calls from its own use case, creates the row and saves the sending job in the caller's transaction.
- **Delivery.** The job calls `DeliverEmail`, which loads the record with its lock, writes the e-mail, sends it and marks it `sent`, in one transaction. A record that is no longer `queued` sends nothing. A failed attempt leaves it `queued` for the next one; the failure of the last attempt marks it `failed`, in a transaction of its own under a context of its own, bounded by ten seconds, and the job still fails; a run that ran out of its time is such a failure, and a run cut by the shutdown, whose context River cancelled, marks nothing. The sending is inside the transaction, so a commit that fails after the sender took the e-mail sends it again on the next attempt, which the idempotency key of `resend` turns into one e-mail.
- **Content.** What an e-mail says beyond its texts, such as a login code, is its content, and it is in no table and no job argument. The area that asks for the e-mails of a template gives their content when each one is delivered, from the record the e-mail refers to, through a `ContentSource` of `email/application`, which `mountJobs` wires for each template. The subject takes no value from the content: it is written in the record when the e-mail is queued.
- **Sending.** The job sends through one interface with two adapters, chosen by configuration (`EMAIL_SENDER`): `smtp` (go-mail), which development, tests and staging point at Mailpit, and `resend` (the Resend API), used in production. The sender address and name are configuration too. Each sender requires its own variables of the worker, and only when it is the one chosen: `EMAIL_SMTP_ADDRESS` for `smtp`, `RESEND_API_KEY` for `resend`.
- **Once.** The `resend` adapter sends the identifier of the e-mail record as the idempotency key (`email-<identifier>`) and as the tag `email_id`, and stores the identifier that Resend returns. Resend keeps an idempotency key for 24 hours, so a job that runs again within them sends one e-mail. The adapter is a client of its own on the standard library, one call to `POST /emails`, with no SDK: a library would carry Resend's messages into the errors. A failure is told by the status and the name of Resend's answer (`answered 422 validation_error`), never by its message, which may quote the address.
- **Bounces.** Resend reports a bounce to `POST /api/resend/bounces`, outside the OpenAPI spec, registered only when `RESEND_WEBHOOK_SECRET` is set; with no secret the path is unknown. Its handler is one of its own on the mux, beside the REST adapter: it reads its body and answers its failures with `restapi.ReadBody`, `restapi.WriteRefusal` and `restapi.WriteFailure`, so the limits of a body and the error format hold, and no middleware of the REST adapter runs on it. It carries no cookie, so the check of the origin in `platform/session` lets it through. It verifies the signature of Svix on the raw body itself, with the standard library: HMAC-SHA256, with the key the secret carries after `whsec_` in base64, over the `svix-id`, the `svix-timestamp` and the body joined by full stops, matched against any `v1` signature of `svix-signature`, and a timestamp within five minutes of now. A wrong one answers 401. It reads the e-mail record from the tag `email_id` and runs `MarkEmailBounced`, which keeps the `svix-id` in `bounce_notices` in its transaction, so a notice delivered again changes nothing, and marks the e-mail `bounced` only when Resend's identifier is the one the record stored. Another event, a notice of no record of the system, of another message, or of an e-mail that failed or already bounced is answered 204 and changes nothing; a notice of an e-mail still `queued` answers 500, keeps nothing, and Resend delivers it again later.
- **Text.** Each e-mail is a template in the backend, with an HTML part and a plain-text part. Its texts are in translation files for every supported language, and the build fails when a language is missing a key, or has one the default language does not. The language is the recipient's preferred language, or the default language when it is not known.
- **Format of a template.** It lives in `internal/email/domain`, embedded in the binary: a name in the list of `template.go`, and two files, `templates/<name>.html`, written with Go's `html/template`, which escapes every value, and `templates/<name>.txt`, with `text/template`. Styles are inline, as mail clients read them. A part reads `{{.Text.<text>}}` for a text, `{{.Content.<field>}}` for a value of the content and `{{.Language}}` for the tag of the language. A shared layout arrives with the second template.
- **Format of a translation file.** `locales/<language>.json`, one per supported language, beside the templates: an object with one object for each template, whose keys name its texts, `{"login_code": {"subject": "...", "lifetime": "..."}}`, as the app's files are. A text is a `text/template` over the content, so it writes a value of it as `It works for {{.Minutes}} minutes.` A text or a value that is missing fails the e-mail, and never leaves a blank in it.
- **Privacy.** An address is in the e-mail record. It never appears in logs, spans, metric labels or job arguments. An SMTP server repeats an address it refuses, and go-mail lists it in its errors, so the `smtp` adapter tells a failure by its step and the codes of the answer only; the `resend` adapter, by the status and the name of the answer. A bounce notice carries the address, and its handler reads nothing of the notice but the type, the identifier Resend gave the e-mail and the tags. `cmd/api/sensitive_test.go` runs a job against a server that refuses the recipient (`email/adapters/smtp/smtptest`) and searches the logs, the spans and the jobs River keeps, and sends the API a signed bounce notice that carries an address while PostgreSQL does not answer.
- **Not built.** Attachments, links for people outside the system, receiving e-mail, and events other than a bounce.

### Control flow

Handle the exceptional case first and return, so the main path runs unindented to the end of the function. `task api:lint` rejects an `else` and a second level of nesting (see Enforcement).

### Naming

A domain concept uses the exact term from `GLOSSARY.md`, spelled in full, in every name that carries it: type, function, package, table, error code. Go's conventional short names (`ctx`, `err`, one-letter receivers) stay.

### Tests

The default seam is the use case, exercised with in-memory implementations of its interfaces. Each real adapter (Postgres, the S3 storage, SMTP, LLM) has its own tests against the real service, but the adapters of the tool of the environments, whose services no test calls (see below).

Assertions use testify: `require` for what the rest of the test depends on, `assert` for what it checks.

What the REST adapter alone does is tested through HTTP: the error format on each kind of failure, strict input and the limits of a body. `platform/restapi` tests it on an operation of the test, and `apps/api/cmd/api` through the binary, on the operations it serves.

What Caddy does in front of the API is tested through HTTP too, in `apps/api/cmd/api/caddy_test.go`: each test starts a container of the Caddy image with the Caddyfile of development, in front of the binary and of a stand-in for Vite, listening where `API_LISTEN_ADDRESS` says. `caddy_staging_test.go` does the same with the Caddyfile of staging, over HTTPS, in front of stand-ins for the API, Garage and Mailpit and of a fixture of the built app. `task services` builds the image.

The in-memory implementations of an area live in its `application/applicationtest` package: its repositories, a clock that stands still until the test moves it, and transactions that undo what a failed operation wrote, as a rollback does. A test of another area that calls the use cases of this one builds them on the same implementations.

A `postgres` adapter test runs against the development PostgreSQL container. Each test gets a temporary database, cloned by pgtestdb from a template that the migrations built, so tests run in parallel and never touch the development database. `databasetest.Migrated` in `platform` gives it. The template is built by the function the `migrate` command calls, and again only when a migration file changes. A test that fails or is killed leaves its database behind (`test_...` or `testdb_...`), and pgtestdb keeps a template for each state the migrations had: `task db:clean` drops them, and no other database, when no test runs.

`databasetest.New` gives the creation of a database 30 seconds. On a machine under load, with several packages creating databases at once, many tests can fail together at exactly that limit (`context deadline exceeded`). A test that fails by a timeout is run again alone, with `task api:test -- -run <test> <package>`, before anything is concluded from it. The limit stays: raising it, or running the packages one at a time, would slow every run.

The `s3` adapter test runs against the Garage of development, in the one bucket every checkout shares: each test writes under keys of its own and removes them. A request of the browser is sent to Garage with the host name of the URL as its `Host`, as Caddy forwards it, and `caddy_test.go` sends one through Caddy too.

A use case that saves a job is tested with an in-memory implementation of the interface that saves it. A job worker in `adapters/river` is tested with River's test helpers, in a temporary database. The `smtp` adapter is tested against the Mailpit of development: each test sends to an address of its own and reads the e-mail back through Mailpit's API, at `MAILPIT_PORT`. The `resend` adapter is tested against a stand-in of Resend's API that answers as its documentation says, with every pull request, and against Resend's test addresses (`delivered@resend.dev`, `bounced@resend.dev`), which deliver nothing. Its webhook is tested with notices signed as Svix signs them (`email/adapters/resend/resendtest`). The `oidc` adapter is tested against the Dex of development, whose login page the test drives over HTTP as a browser does (`access/adapters/oidc/oidctest`, which the tests of the binary use too), and against an issuer the test stands up for the ID tokens Dex never writes: an e-mail it does not say is verified, another client, another issuer, an expired token.

The tests of the `resend` adapter against Resend call a service outside the repository. Their names start with `TestAgainstResend`, `task api:test:resend` runs them, and they skip when `RESEND_TEST_API_KEY` is empty, which `.env.example` leaves it: only the scheduled workflow `resend.yml` gives them the key, from a secret, never a pull request (see Pipeline). No test calls a real LLM provider: every one meets the LLM stand-in (see Environments).

No test calls Hetzner or GitHub either. The tool of the environments is tested through its commands, with in-memory implementations of the provider, of GitHub, of readiness and of the server a publish works on. Its adapters are tested against stand-ins: Hetzner's against a stand-in of Hetzner's API that answers as its documentation says, as the `resend` adapter is, the `gh` adapter against a stand-in of `gh` that records its arguments and its standard input, and the SSH adapter against an SSH server the test starts in its own process. Turning staging on and off and publishing a pull request to it, by hand, prove them against the real services (see Servers in Environments).

### Enforcement

A rule that tooling can check fails the build. `task api:lint` runs the checks of the Go code, and `task check`, `task check:quick` and the pipeline run it.

- **Linter.** golangci-lint, configured in `apps/api/.golangci.yml`: its standard set of linters, and nolintlint, which makes a `//nolint` name the rule it silences and say why.
- **Formatter.** gofumpt, run by golangci-lint, so a file that is not formatted fails the lint. `task api:format` rewrites the files.
- **Imports.** `apps/api/tools/guard/imports` checks the table of the Dependency rule on every import under `internal`, and that every package there has a place in the Layout. A test file may also import a third-party library, in any layer.
- **Control flow.** `apps/api/tools/guard/controlflow` rejects an `else`, an `else if` included, and a block that opens inside another block of the same function. The blocks are `if`, `for`, `switch` and `select`. A function literal starts again from zero. It judges every Go file of the module, the tests and `apps/api/tools` included, as the linter does; the imports are judged under `internal` only.
- **Mapping.** The exhaustruct linter of golangci-lint, limited to the types of the adapter packages: a struct literal of an API type or of a database row that leaves a field out fails the lint. The zero value returned beside an error is allowed.
- **Generated code.** A file is generated when it carries the comment Go defines for it, `// Code generated ... DO NOT EDIT.`, before the package clause. Its format, its lint and its control flow are not judged. Its imports are.
- **Fixtures.** No check reads a `testdata` directory.

The checks written in this repository are the commands of `apps/api/tools/guard`, with their tests beside them. A finding is a line with the file, the line and what to do.

The generated OpenAPI spec and the code generated by sqlc are committed, and the build fails when either is stale. For the code of sqlc the check is `task api:generate:check` (see Database). `task check`, `task check:quick` and the pipeline run the checks of the spec too:

- **Spec up to date.** `task api:spec` writes `apps/api/openapi.yaml` from the operations the command that serves registers, and `task api:spec:check` fails when the committed file differs from them.
- **Lint of the spec.** `task api:spec:lint` runs vacuum with the rules in `apps/api/tools/speclint`: `snake_case` in the properties of a body and in query and path parameters, and plural paths (see Paths in API conventions). Their tests, beside them, run vacuum on a spec that keeps the conventions and on one that breaks each.
- **Breaking changes.** `task api:spec:breaking` shows the report of oasdiff against the main branch. A breaking change does not fail it (ADR-0009); a failure of oasdiff itself, such as a base revision it cannot read, does.
- **Lists of the app.** `task api:lists` writes the countries of `internal/kernel` and the currencies and the time zones of `internal/consultancy/domain` into `apps/web/src/shared/places/lists.gen.ts`, and `task api:lists:check` fails when the committed file differs from them (see Contract in the frontend rules). A test of `consultancy/domain` does both, as the spec is written, because those two lists are unexported.

Once a check exists, its rule here shrinks to a pointer at the check.

### Deferred

Domain events, CQRS, event sourcing and a second bounded context each start with a new ADR, written when a concrete case needs one. So do WebSocket, GraphQL and gRPC as inbound adapters, live notifications (which need domain events), OAuth for agents, and MCP resources and prompts.

The system runs as one instance of the API, on Docker Compose. More than one instance, and Kubernetes, are not planned, and each starts with a new ADR. The known obstacles are listed in ADR-0016.

The steps of the document processing pipeline, and where files are converted, are decided when the processors are implemented. Recording who moved a record from one status to another waits for the history of work that screens show. How the system writes to a project's Git repository starts with its own decision. The language of a client's contact is decided with the modelling of contacts.

The domain areas named so far are `consultancy`, `access` (login code, session, link to a login provider and, later, access tokens), `email`, `files`, `audit` (the audit log) and the system health area.

Nothing is cached yet. A read measured as slow first gets an in-memory cache inside the API. A cache service (Redis, Valkey) and a connection pooler (PgBouncer) each start with a new ADR.

A lint for migrations and the column type for case-insensitive text are decided with the first case that needs one.

The foundation and the setup are built: the first backend code, the first screen and the checks of a pull request. So are the login and the shell of the app, with River and the worker command, the e-mails with Mailpit and Resend, Dex as the local login provider, the seed, the periodic job that removes the uploads never confirmed, the daily cleanup, the signed URLs that read an image from the storage and the check of the origin of a request. The consultancy settings, the users and the system health follow, specified now that the login is built, with the scopes and their checks, the health report and its screen, and the River panel behind the session. There is no walking skeleton. Three conventions were fixed with the first code: where dependencies are wired is in Commands, where a use case takes the current time from is in Use cases, and the assertion library of the tests is in Tests. The first record removed is the file of an upload never confirmed, which is deleted, because nothing refers to it and it records no business fact (see Files). Whether a business record is deleted or archived is still fixed with the first one that is removed; a user is deactivated and never removed.

Some joints between the decided tools are recorded in their ADRs as read in documentation and not tried yet. Each is proven by the work named below, and a joint that does not hold changes its ADR before that work goes on:

- **The login and the shell of the app.** sqlc reading the SQL that River exports, goose applying it, and the permissions the API role needs on River's tables: proven, goose with its annotations around dollar quotes (see Tables in Jobs and ADR-0016). Whether River's OpenTelemetry middleware links the trace that saved a job to the one that ran it: proven, by a link (see Traces in Jobs). The login with a login code read from Mailpit, and through Dex: proven, by the end-to-end test, on the real system through Caddy, with the worker sending the code to Mailpit (see Tests in the frontend rules). The seed creating its data twice with the same result: proven, by the tests of `cmd/seed` (see Seed in Environments). How each login provider states that an e-mail is verified, which ADR-0011 checked when the login was built: corrected, because the login was built with Dex alone, whose ID token carries `email_verified` as a boolean; ADR-0011 now checks each real provider when it is configured (see Their own efforts below).
- **The consultancy settings, the users and the system health.** A TanStack DB collection loaded by following the cursor of the API to the end. The check of the job worker and of the e-mail server for the health report.
- **The deploy.** Backblaze B2 with the AWS SDK for Go v2, and pgBackRest against it. The Content-Security-Policy with Base UI and the fonts: proven, with Caddy serving the built app through the login with a login code (see Security headers in Delivery). A Vite plugin that writes Brotli copies under Vite+: proven, by a plugin of this repository (see Compression in Delivery). The server created from the cloud-init file, the life of a staging server, its address under `sslip.io` and its certificates, and the publish with its check of the variables: proven, by staging turned on and off for real and a pull request published to it (see Servers and Secrets in Environments, and Publishing in Pipeline). The restore test. A pull request from a fork. `task outdated` and its weekly workflow. The first release and the ruleset that protects its tag. What ADR-0012 lists about Grafana and the collector, which waits for the telemetry containers.
- **Their own efforts.** The MCP handler on the same router as Huma (ADR-0008). The LLM stand-in and a call to a provider that spends no tokens (ADR-0004, ADR-0019). How Google, Microsoft and GitHub each state that an e-mail is verified, checked when each is configured (ADR-0011).

What the login left for later efforts, each where it goes:

- **The users.** Unlinking an account of a login provider from a user, with the settings of the user: until then a user who lost the account they linked at a provider logs in through another provider or with a login code (ADR-0011).
- **The system health.** The service name of the spans of the worker, which is `api`, as for every command of the binary, and the River panel behind the session, for an administrator (Jobs).
- **The deploy.** The account of Resend the tests against Resend use, and the secret `RESEND_TEST_API_KEY` (`docs/providers.md`). How each real login provider states that an e-mail is verified, checked as each is configured (ADR-0011).
- **With no effort of its own yet.** A bucket, or a prefix of keys, for each database in development and in the Go tests, which share the bucket of development: a worker of development removes, a day later, what only a database of a test names (Files). The image of River UI, updated with River in the same commit (Jobs).

The Go type for money (`kernel` imports the standard library only) and the client library for LLM providers are each chosen with the first code that needs one. The templates and translation files of e-mails were chosen with the login code (see E-mail). How a build reads `VERSION` is decided with the health report, which shows it; until then the `info.version` of the OpenAPI spec is `unversioned`.

The rate-limit plugin of Caddy and the number of the setup code were chosen with the setup, and the numbers of the login code with the login (see Rate limit in Delivery). The routine that encrypts credential values again under a new key is written with the first change of key.

`ETag` with `If-Match`, `Idempotency-Key` and rate limiting beyond the login code and the setup code are added to the first case that needs one, without a new ADR. A version in the path waits for the first consumer that does not ship with the backend, and reopens ADR-0009.

## Frontend (`apps/web`)

`apps/web` is the only JavaScript package of the repository. It holds the app and the design system, and a screen is built in it directly, with no prototype first (ADR-0025).

### Layout

```
apps/web/src/
├── routes/              # TanStack Router files: path, search parameters, loader
├── <domain area>/
│   ├── screens/         # one screen per route
│   ├── components/      # pieces of this area's screens
│   ├── data/            # collections, queries and writes
│   ├── locales/         # en-US.json, pt-BR.json, es.json
│   └── index.ts         # what routes and other areas import
├── shared/
│   ├── api/             # the fetch the generated client calls
│   ├── api/generated/   # orval output, never edited
│   ├── components/      # pieces of screens of more than one area
│   ├── errors/          # translation of API error codes
│   ├── places/          # the countries, currencies and time zones the API accepts, named by the browser
│   ├── stores/          # Zustand stores used by more than one area
│   └── locales/         # texts used by more than one area
├── testing/             # the support of the screen tests
├── ui/                  # the design system: components, hooks, the stylesheet
├── app.tsx              # the app put together: texts, router, query client
└── main.tsx             # starts the app in the browser
```

A domain area has the same name as in the backend and in `GLOSSARY.md`. A route file declares the path, the search parameters and the loader, and opens a screen from an area. A layout route that puts the pieces of several areas into a recipe of `shared` imports each from the `index.ts` of its area, so no area depends on the others for it: `routes/_shell.tsx` puts the menu of the user, from `access`, and the consultancy, from `consultancy`, into the `AppShell` around every screen after the login, and its loader reads the consultancy before the shell opens.

### Dependency rule

Dependencies point one way: `routes` → a domain area → `shared` → `ui`.

| Code | Imports |
| --- | --- |
| `routes` | the `index.ts` of areas, `shared` |
| a domain area | its own files, the `index.ts` of other areas, `shared`, `ui` |
| `data` of an area | the same, plus `shared/api/generated` |
| `shared` | `ui`, third-party libraries; no area |
| `ui` | third-party libraries; nothing else of the app |
| `testing` and a test | anything, the generated simulated responses included; nothing but a test imports `testing` |
| the files directly in `src` | anything: they put the app together |

Only `data` imports the generated client. Screens and components never call it or build a URL. The one exception is `shared/locales/languages.ts`, which reads the supported languages from the generated client, because the single list of ADR-0005 is the backend's and reaches the app through the contract.

`task web:imports` checks the table with dependency-cruiser, whose rules are in `apps/web/.dependency-cruiser.mjs`.

### State

Each kind of state has one home:

- **Data from the API** lives in the area's `data` module, never in a store or in component state.
- **Filters, sorting and the current page** are search parameters in the URL, typed by TanStack Router and validated with Zod.
- **Interface state shared between screens** (theme, open panels, drafts) is a Zustand store, small and about one subject. Zustand arrives with the first such store. State that a library holds for the whole app, outside React, stays where the library keeps it: the theme of the system on the root element (`shared/theme.ts`), and the language, in i18next, with the language of the selector beside it in `shared/locales/i18n.ts`, which no component reads, only the gate and the start of the app.
- **A flow with several steps** on a screen, such as an upload in parts or the login code, is one reducer in the component, whose state is a union of named steps. No state machine library. A step holds what the screen alone knows: the step of the login code keeps the e-mail and when the last code was asked for, and no attempts left, which it shows from the last answer of the API.
- **Everything else** is React state in the component that uses it.

### Data module

- **Names.** It exposes the area's reads and writes under glossary names. A module that only forwards a call is correct.
- **Bounded sets.** A set that grows with the business (clients, projects, configuration content) is a TanStack DB collection, loaded whole and queried in the browser with live queries.
- **Unbounded sets.** A set that grows with activity (hours logged, comments, the audit log) is read through TanStack Query with the API's cursor.
- **Optimistic writes.** Only for a simple, reversible edit. A creation and an operation with a business rule wait for the server. A refused write rolls back and shows the error.
- **Allowed actions.** A screen shows an action only when `allowed_actions` from the API includes it. It never derives that from the status.
- **The session.** Who is logged in is a read of `access` (`loggedInUserQuery`), which resolves with `null` where the API answers 401. The gate, the `beforeLoad` of `routes/__root.tsx`, reads the state of the setup and then that read: the setup while it is not concluded, the login without a user (keeping any other address in `return_to`, with no notice: an address the app does not have reaches the login the same way), and the app with one, in the user's preferred language; a logged-in user at the login goes to `return_to` when it is a path of the app, or to the root. A 401 met past the login, by any read or write, opens the login with `notice=session_ended` and `return_to`: `request` in `shared/api/request.ts`, which every generated function calls, tells the listener set with `whenUnauthorized`, and `access` sets it in `createApp`, where the router is (`openTheLoginWhenTheSessionEnds`). A screen never handles a 401 itself. Logging out and a session that ended forget every query, and the gate puts the app back in the language of the selector. Who is logged in and the consultancy (`consultancyQuery`) carry the URLs of the photo and of the logo, which work for 15 minutes, so both are read again every ten minutes while the shell shows, as well as when the person comes back to the tab. The loader of `routes/login.tsx` reads the login state (`loginStateQuery`) before the login screen opens, after the gate, so a logged-in user never reads it: the buttons of the login providers come from it, and so does the e-mail of the notice `no_active_user`, which travels in no address; without a refusal in the login state that notice shows nothing. A button of a provider is a link to the start of its login, whose address the data module builds (`providerLoginAddress`) with the `return_to` the login was given, when it is a path of the app other than the root that the contract takes: the browser navigates there, and the generated client never calls it.
- **Documentation.** TanStack DB is recent. Read its current documentation before writing a collection.

### Contract

The API client, the Zod schemas and the simulated responses in `shared/api/generated` are generated by orval from the committed OpenAPI spec. They are committed and never edited by hand. A change to the API is made in the backend and regenerated: `task web:generate` writes them (`apps/web/orval.config.ts`), and `task web:generate:check` fails when they are stale. A generated function resolves with the body of a success and rejects with an `ApiError` (`shared/api/request.ts`) that carries the error body.

A set of values the API checks against a list it carries, and that the contract does not declare as an `enum`, reaches the app the same way: generated, committed, never typed again in the app and never taken from what the browser happens to support. The countries, the currencies and the time zones the backend carries (`apps/api/internal/kernel` and `apps/api/internal/consultancy/domain`) are written by `task api:lists` into `shared/places/lists.gen.ts`, as constants whose values are literal types, and `task api:lists:check` fails when the file is stale. The list of supported languages comes through the contract instead (see Dependency rule). Data the backend does not carry, such as the currency of each country, lives in one file of `shared/places` that names its source and release, typed against those lists.

### Forms and validation

Forms use TanStack Form with the generated Zod schemas. A form validates only what the contract declares: required, format, length. Business rules are not repeated in the frontend. A 422 from the API is shown on the field it names, with the `trace_id` of the answer, and so is a refusal the API answers about one field without naming it, such as an e-mail in use. `shared/errors/problems.ts` does both in the codes the API answers with: `checkTheContract` checks a schema, and `refusedFields` reads the fields an answer refused.

- **Names.** A field is named by its place in the body (`consultancy.address.city`), so the problem the API names at `body.consultancy.address.city` lands on it with no mapping.
- **Optional texts.** A form holds an empty text for an optional field the person left empty, and the body leaves it out, because an empty e-mail breaks `format: email`. The data module turns the typed values into the body, and `checkTheContract(schema, toBody)` checks what will be sent.
- **Steps.** A form of several steps is one TanStack Form held by the screen beside the reducer of the flow, so it outlives every step. Each step is a `form.FormGroup` whose `onSubmit` validator checks its part of the schema (`ConcludeSetupBody.shape.consultancy`), and `group.handleSubmit()` moves on. A field forgets its problems when it unmounts, and React in development mounts a new component, unmounts it and mounts it again. So a 422 answered on a later step is held by the flow, which moves to the first step that holds one, and each step, when it opens, puts the problems of its own fields on the form with `form.setErrorMap({ onSubmit: { fields } })` from an effect, which runs again after that second mount (`useStepOpening` in `consultancy/components/setup-form.ts`).
- **Fields.** `TextField` and `ListField` in `shared/components/form-fields.tsx` draw a field of TanStack Form with its label, its description and its problem. A field whose value is chosen from a long list is a `ListField`, on the Combobox of `ui`. No field passes `onBlur`: TanStack Form drops the answer of the API on a field when the field is left, and the line under it with it, which moves a button under the pointer between its press and its release. The answer stays until the value of the field changes, so a step whose field the API refused moves on once the person changed it.
- **Files.** A file the body refers to is a field that holds the identifier of the file, set once the file is stored, so concluding sends it and a 422 lands on it like any other. The file is checked when chosen for what the contract declares, its type and its size, and sent at once: the API signs a URL and the data module sends the bytes there (`shared/api/storage.ts`). Its upload is one reducer of named steps (State) held by the screen beside the form, so it goes on while the person moves between steps; a result for a file replaced or removed meanwhile is dropped. An image shows from the browser's own copy. The images of the setup are the first: `consultancy/components/setup-image.ts`.

### Errors

An API error is translated by its `code` and `params` through `shared/errors`, in the user's language, and shows its `trace_id`. A failure while rendering falls to an error screen that offers a reload. The frontend sends no telemetry.

### Values

The frontend formats values with the browser's `Intl`, in the user's language, and computes none: totals arrive ready from the API. No date or decimal library is added until a screen proves it needs one.

The name of a country or a currency is `Intl.DisplayNames` in the language of the screen. A time zone, which `Intl.DisplayNames` does not name, is written as the IANA database writes it, followed by the name `Intl.DateTimeFormat` gives it (`timeZoneName: "longGeneric"`). `shared/places/places.ts` builds the lists once for each language. A code the browser cannot name shows as the code.

### Components

- `apps/web/src/ui` holds the design system: shadcn/ui components on Base UI primitives, styled with Tailwind CSS. Every colour, radius, font, spacing step and shadow is a CSS custom property. The rest of the app imports it as `#ui/components/<name>`, `#ui/hooks/<name>` and `#ui/lib/<name>`.
- `ui` has no translation of its own. A component takes each word as a prop and the screen supplies it, in its language. That includes a name read only by assistive technology, such as the label of a close button: the component requires it and writes no default.
- [`DESIGN.md`](DESIGN.md) is the source of the design system: its rules, the style variables with their roles and values, the components and the recipes. Code that disagrees with it is fixed, and a change to any of these updates it in the same change.
- A component enters `ui` when a screen needs it, through the shadcn CLI, and the component table of `DESIGN.md` gains it in the same change. The CLI is run in `apps/web`, with its exact version. It writes each dependency of the component as a range: change it to `catalog:` and add its exact version to the catalog.
- A specialised library (charts, rich text, drag and drop, calendar) is added when the first screen needs it. The one shadcn/ui already wraps is the first candidate.
- React Compiler memoizes components. Do not write `useMemo`, `useCallback` or `memo`.

### Naming

A domain concept uses the exact term from `GLOSSARY.md` in component, route, folder and variable names.

### Tests

The default test renders a whole screen in Vitest's Browser Mode, with the API simulated by MSW from the generated responses, and acts as a user does: it clicks, types and reads what is on the screen. It opens the whole app at an address with `renderApp`, in the languages the browser asks for and in StrictMode, as `main.tsx` renders it, and hands the simulated API each answer it needs with a handler orval generated (`src/testing`); a request with no answer fails the test. The tests run in the Chromium of the Playwright in the catalog, which `task install` downloads. A few end-to-end tests in Playwright cover critical flows against the real backend. They log in through the local provider, Dex, or with a login code read from Mailpit (see Login in the backend rules).

The end-to-end tests are in `apps/web/e2e`, on the test runner of that same Playwright (`playwright/test`, with `apps/web/playwright.config.ts`), and `task e2e` runs them, at the end of `task check` and in the pipeline; `task check:quick` runs every other check and leaves them out, so it runs beside `task dev`. It starts the services, Mailpit and Dex among them, and builds the API, creates an empty database of its own (`software_factory_e2e`, kept after the run so that a failure can be read in it), and applies the migrations. It also has a bucket of its own in Garage, `software-factory-e2e-files`, created on its first run and kept: its worker cleans up the bucket it is given, and on the bucket of development it would remove what only the development database names (see Files in the backend rules), while on its own it removes what earlier runs left there a day before. Playwright starts the API binary and its `worker` command on that database and that bucket, and Vite, where Caddy reaches them; the worker is ready once its log says it started. The tests open the app at `APP_ORIGIN`, through Caddy, and drive the commands of the binary as a person does. A server already listening on port 8080 or 5173, such as one of `task dev`, fails the run instead of being used.

The tests run one at a time, in the order they are written, in two projects of Playwright: `setup.spec.ts`, then `login.spec.ts`, which runs only once the setup passed and continues on the database it left. The setup registers the first administrator with the e-mail of the administrator Dex knows. The login then goes through Dex to the shell, with the name and the logo of the consultancy and the photo in the user's menu read from the bucket through the URLs the API signed, and logs out; logs in with a code the worker sent to Mailpit, read through Mailpit's API as the first e-mail to that address after the ones Mailpit already held; and goes through Dex as the person who is no user, to the notice with their e-mail. In development every request of the machine reaches Caddy from the same address (see Rate limit), so `task e2e` restarts Caddy, which empties the limits, and the test that spends a limit is the last of its file: the one of the setup ends `setup.spec.ts`, the one of the login code ends `login.spec.ts`. A failed test keeps its trace in `apps/web/test-results`, which the pipeline keeps as an artifact. Its data is fictitious (ADR-0021), and so are its images, in `e2e/images`, and the people of Dex, which `e2e/system.ts` names.

### Enforcement

- `vp check` (format, lint, types) and the screen tests fail the build. `vp staged` runs format and lint on changed files at each commit.
- Knip fails the build on unused files, exports and dependencies: `task web:knip`, configured in `apps/web/knip.ts`. The components of `ui` count as used, because the design system is kept whole.
- The build fails when an import breaks the dependency rule (`task web:imports`), when the generated client is stale (`task web:generate:check`), when the lists of the API are stale (`task api:lists:check`, in the backend job, which has Go), or when a language is missing a translation key (ADR-0005).
- The size of the JavaScript of `apps/web`, initial and per route, has a limit that fails the build. It is measured on the Brotli copies the build writes (see Delivery), which is what the browser downloads. Its numbers and its tool are not chosen yet (see Deferred).
- Load and response times of the main screens (LCP, CLS, INP) are reported against the "good" Core Web Vitals thresholds (2.5 s, 0.1, 200 ms) and do not fail the build.

Once a check exists, its rule here shrinks to a pointer at the check.

For investigating performance in a real browser, agents have the Chrome DevTools MCP: see [`docs/agents/browser-tools.md`](docs/agents/browser-tools.md).

### Deferred

A client for Server-Sent Events is written with the first streaming operation. Telemetry from the browser waits for users other than the maintainer. The limit on the size of the JavaScript, its numbers and its tool, and the tool of the time report (see Enforcement) are still the maintainer's to choose: staging already serves the built app as production will, with the Brotli copies they measure. The colour of the circle of a person, muted initials for now, is chosen with the first screen that tells people apart (DESIGN.md).

The system health screen waits for the health report (see Health in the backend rules). It reads the health report when it opens and on a refresh button, with no automatic refresh and no warning on other screens.

## Delivery (`deploy`)

How responses travel from the server to browsers and agents. Compression, cache headers, security headers, the limit on the login code and the setup code, and the masking of addresses in the logs are set in one place: the snippets of `deploy/delivery.caddyfile`, which the Caddyfile of each environment imports from beside it, `deploy/Caddyfile` in development and `deploy/Caddyfile.staging` in staging. A rule is written once there, and each site imports the snippets that apply to it.

- **Every environment.** The site of the app imports `site-of-the-app`, with the address of the API: compression, `/up`, `/api` with `Cache-Control: no-store`, the two limits and the answers of Caddy in the error format of the API. The global options import `logs-without-whole-addresses`.
- **Where Caddy serves the built app.** The site of the app imports `the-built-app`, the files of `/srv` with their cache headers, and `security-headers`, with the host name of the file storage. Development imports neither: Vite serves the app there, and the policy refuses the inline script and the style elements Vite writes into the page to reload it as the code changes. The security headers are seen in staging.
- **Tested.** `apps/api/cmd/api/caddy_test.go` starts Caddy with the Caddyfile of development, and `caddy_staging_test.go` with the one of staging, over HTTPS with certificates of Caddy's local authority, for host names under `localhost`.

### Compression

- **In Caddy only.** The site of the app turns on `encode` with its defaults: zstd or gzip, chosen from the request's `Accept-Encoding`, for a response of 512 bytes or more with a compressible content type. That covers the files of the SPA, `/api` and MCP. Request bodies are not compressed.
- **Defaults kept.** The `match` block of `encode` is not written, because a custom block replaces the whole default list of content types. Error bodies (`application/problem+json`) are outside that list and stay uncompressed; they are small.
- **Streams.** A Server-Sent Events response is compressed like any other, and Caddy sends each event as it is written. The first streaming operation comes with a test that goes through Caddy and checks that events arrive one by one.
- **Brotli copies.** The build of `apps/web` writes a Brotli copy (`.br`) beside each text file it produces: JavaScript, CSS, HTML, SVG and JSON. Caddy serves the copy through `file_server` with `precompressed br`. Caddy does not compress Brotli on the fly, so a client that does not accept Brotli gets the file through `encode`.
- **Plugin.** A Vite plugin of this repository writes the Brotli copies: `apps/web/brotli-copies.ts`. Once the build has written every file, it compresses each text file with the Brotli of Node.js, at its highest quality. It needs no dependency, where no plugin of the ecosystem had been tried with Vite+ (Rolldown). `task web:build` runs the build.
- **Risk accepted.** The size of a compressed response can reveal a secret when the same body carries text chosen by an attacker (the BREACH attack). No response is excluded from compression. The rule on credential values in the API conventions keeps the two apart.

### Caching

- **Files with a hash.** A file whose name carries a content hash is served with `Cache-Control: public, max-age=31536000, immutable`. Vite writes every file of the build but `index.html` under `/assets/`, with its hash, so the rule is the path `/assets/*`. The app has no folder of files Vite copies as they are; one would come out at the root, without a hash, and fall under the next rule.
- **Files without a hash.** `index.html`, and any other file of the SPA without a hash in its name, is served with `Cache-Control: no-cache`, so the browser revalidates it and a deploy shows on the next load.
- **Routes and failures.** A path that is no file is a route of the app, answered with `index.html`. A file under `/assets/` that does not exist is a 404, never the app. Each cache header is set as the response leaves, so a failure carries none.
- **Inlined files.** The build inlines nothing as a `data:` URL (`build.assetsInlineLimit: 0` in `apps/web/vite.config.ts`), so a small font or image is a file of its own under `/assets/` too, which the security headers allow.
- **API.** Every response under `/api` carries `Cache-Control: no-store`, set by Caddy.

### API out of reach

- **Same format.** When Caddy cannot reach the API, it answers a 502, 503 or 504 under `/api` in the error format of the API, with the code of the status (`bad_gateway`, `service_unavailable`, `gateway_timeout`), `params` and the `trace_id` of the trace it started, as it answers the 429 (see Rate limit). The body is written in `deploy/delivery.caddyfile`, and `apps/api/cmd/api/caddy_test.go` checks it against the schema `Problem`. The app translates the three codes. `/up` keeps its status and no body.

### Security headers

- **In Caddy only.** The snippet `security-headers` sets them for the app's site, where Caddy serves the built app. The API sets none.
- **Content-Security-Policy.** Scripts, styles, fonts and connections come from the app's own origin only, plus the host name of the file storage for connections, images and media. That host name comes from configuration, because it differs by environment: the snippet takes `FILE_STORAGE_PUBLIC_URL`. No inline script, no `eval`, no other origin. The page cannot be shown inside a frame.
- **Exact values.** `default-src 'self'; script-src 'self'; style-src 'self'; style-src-elem 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' blob: <files>; media-src 'self' blob: <files>; connect-src 'self' <files>; object-src 'none'; base-uri 'none'; frame-ancestors 'none'`. They were closed with Caddy serving the built app, through the login with a login code: `blob:` is the image a screen shows from a file chosen on the machine, before it is sent; `style-src-elem` lets in inline style elements and nothing else inline, because input-otp writes one for the field of a code and Base UI one to hide a scrollbar. A style attribute stays refused: React sets styles through the CSSOM, which the policy does not govern.
- **Zod and eval.** Zod 4 tries `new Function` once, when it builds its first object schema, to know whether it may compile a schema. The policy refuses it, the browser reports one violation on its console, and Zod checks every value without compiling. `z.config({ jitless: true })` does not stop the try, because the schemas of the generated client are built in a chunk that runs before the code of `main.tsx`.
- **Others.** `Strict-Transport-Security: max-age=31536000`, `X-Content-Type-Options: nosniff` and `Referrer-Policy: same-origin`, which keeps the address of a page inside the origin of the app and still sends the `Origin` header the API checks.
- **Frontend.** Code in `apps/web` loads nothing from another origin and writes no inline script.

### Rate limit

- **The two codes only.** Caddy limits, for each IP address, the two operations of the login code, asking for one and checking one, and the checking of a setup code. No other path has a limit.
- **Setup code.** Every `POST` under `/api/setup` checks a setup code, so they share one limit: 10 requests a minute for each IP address, counted together. `GET /api/setup` has none.
- **Login code.** The two operations of the login code, `POST /api/session/login_code` and `POST /api/session/login_code/check`, share a zone of their own, `login`: 10 requests a minute for each IP address, counted together, apart from the zone of the setup. Every other path under `/api/session` has none.
- **Answer.** Over the limit, Caddy answers 429 in the error format of the API, with the code `too_many_requests`, `Retry-After` and `Cache-Control: no-store`. Its `trace_id` is the trace Caddy started for the request, which never reached the API. The body is written in `deploy/delivery.caddyfile`, and `apps/api/cmd/api/caddy_test.go` and `caddy_staging_test.go` check it against the schema `Problem` of the committed spec. An operation under the limit lists `restapi.LimitedByCaddy` under 429, so the spec, and the client generated from it, declare the answer.
- **Development.** Every request of the machine reaches Caddy from the gateway of Docker, so the browser, `curl` and the tests share the count of ten of each zone. The counts live in the memory of Caddy: `docker compose restart caddy` empties them, as `task e2e` does before it runs.
- **Staging.** No proxy stands in front of Caddy, so each caller is counted by its own address. The server has no IPv6 address, so the weakness below does not reach it yet.
- **Image.** Caddy has no rate limit of its own, so its image is built in this repository with a plugin: [`mholt/caddy-ratelimit`](https://github.com/mholt/caddy-ratelimit), the rate limiter of Caddy's author, in `deploy/caddy/Dockerfile`. Its `rate_limit` directive keeps a sliding window for each key in memory, and its 429 is an error that `handle_errors` writes. It logs each request it refuses with the address of the caller, which the default log of every Caddyfile cuts down to its network (see Privacy in Observability).
- **IPv6.** The plugin stays at v0.1.0, its last release, of August 2024. That release keys an IPv6 caller by its whole address, so a caller who holds an IPv6 range escapes the limit by changing address. The main branch of the plugin keys an IPv6 caller by its prefix (a /64) and has no release. This is settled with the deploy, when the system is exposed.

## Observability (`deploy`)

How telemetry is collected, kept and shown. The configuration of the collector and the dashboards, alert rules and contact point of Grafana are files in `deploy/`. What the backend emits is in Telemetry, in the backend rules. The reasons are in ADR-0012.

### Where it runs

- **Development only, for now.** The two telemetry containers are declared in the development Compose file. Production and staging do not declare them until someone other than the maintainer uses the system. The rules below describe the deployment once it has them.
- **Production meanwhile.** The API and the worker export nothing, because they have no address to send to. Their output stays in Docker's log files, which rotate by size, and is read on the server.
- **Monitor.** A hosted monitor outside the server calls `/up` (see Health) and receives a signal at the end of each backup (see Environments). It warns the maintainer by e-mail when the system does not answer or a backup does not report.

### Collection

- **Two containers.** An OpenTelemetry Collector gathers the telemetry and forwards it to `grafana/otel-lgtm`, which stores and shows it. The API and the worker send to the collector, never to `otel-lgtm`.
- **Collector.** It has no public port. It mounts, read-only, the server's file system and Docker's log folder. The Docker socket is never mounted.
- **Sources.** The traces and metrics of the API, the worker and Caddy. The output of every container, read from Docker's log files. The metrics of the server (disk, memory, CPU), PostgreSQL, Caddy and the file storage.
- **PostgreSQL.** The collector connects with the monitoring role (see Database).
- **File storage.** Its metrics endpoint is read with a token.
- **Docker logs.** The log files of every container rotate by size, set in the Compose files, in every environment.

### Privacy

The rule that telemetry carries nothing sensitive covers every container.

- **Caddy.** Tracing is on for the app's site, so the trace starts in Caddy and its identifier is in the access log. The access log carries the caller's IP address cut down to its network, and no query string. So does every other line Caddy writes, such as the one of the rate-limit plugin when it refuses a request and the one of a request that failed: the default log of every Caddyfile, the snippet `logs-without-whole-addresses` of `deploy/delivery.caddyfile`, filters the fields that hold an address (`remote_ip`, `client_ip`, `request>remote_ip`, `request>client_ip`) and keeps 24 bits of an IPv4 address and 48 of an IPv6 one, which `apps/api/cmd/api/caddy_test.go` and `caddy_staging_test.go` check. Caddy's spans are another matter: its tracing records what the HTTP instrumentation of OpenTelemetry records, the whole address of the caller and the path among them. Caddy exports nothing yet (see Development below); when the telemetry containers export its spans, the collector cuts them as the next rule says.
- **Dex.** It logs at `warn` in development: at `info` it writes, for each login, the e-mail and the name of the person, and a failed login of the API at the token endpoint. Its failures are lines at `warn` and above.
- **Spans.** A span carries the caller's IP address cut down to its network, and no query string. The collector removes what a source cannot leave out.
- **PostgreSQL.** It logs an error without its detail (`log_error_verbosity = terse`), because the detail can carry the values of a row.

### Storage

- **Retention.** Logs, traces and metrics are kept for 30 days. The metrics also have a size limit. Both are set in the Compose file, because the image keeps logs forever by default.
- **Volume.** One volume holds the stored telemetry. It is not in the backup.

### Grafana

- **Address.** Grafana has a host name of its own in the `Caddyfile`. No other port of `otel-lgtm` is published, because none of them has a login.
- **Login.** Through GitHub, for the maintainer's account only, as administrator. Anonymous access and the password form are turned off; the image starts with both on.
- **Files.** Dashboards, alert rules and the contact point are loaded from `deploy/` when Grafana starts. A change to one of them is a commit. Nothing made on Grafana's screens is kept.
- **Alerts.** Grafana evaluates the rules and warns by e-mail, through an SMTP server taken from the deploy configuration. The first rules are five: the API or the worker stopped sending metrics, the rate of 5xx is high, the disk is over 80% full, PostgreSQL is down or near its connection limit, and an optional part is down. The thresholds are set with the first real traffic.

### Development

The two telemetry containers are in a Compose profile that is off by default. Without them the API and the worker run as usual, and their logs are on the container output. Caddy traces every request and exports nothing (`OTEL_TRACES_EXPORTER=none` in `compose.yaml`, and in `deploy/compose.staging.yaml`, which declares no telemetry either).

### Deferred

The telemetry containers in production wait for users other than the maintainer. Which hosted monitor is used is chosen with the deploy.

Logs over OTLP wait for stable exporters in the Go SDK.

The cost of LLM calls per client or per project is decided with the first LLM feature. If it matters, it is business data in the database, with its own ADR.

## Environments (`deploy`)

Where the system runs and what each environment declares. The reasons, and the table of what each environment runs, are in ADR-0019. Backup and production files are in ADR-0020.

### Providers

- **One list.** Every outside service the system depends on is in [`docs/providers.md`](docs/providers.md): why it was chosen, what it costs and what is configured there.
- **No secret.** It holds addresses, host names, DNS records, regions and names. It never holds a key, a token or a password.
- **Kept true.** A change made at a provider is written there in the same pull request, or right after when the change is made by hand.

### Compose files

- **Three files.** `compose.yaml` at the root is development. `deploy/compose.staging.yaml` and `deploy/compose.production.yaml` are the two servers.
- **One base.** The three extend `deploy/compose.base.yaml`, which holds the services they share and the tag of every image, so a version keeps one home (see Versions), and what a service is wherever it runs: the command of Garage, its configs and its health check, the script of `garage-cors`, the connection of the seed, the health check and the stop grace period of the API. The file of an environment adds what differs there: ports, the order of start, a restart policy, the image of a commit. `extends` copies a service and nothing else: a volume or a config the service names is declared at the top level of the file that extends it. The configs of PostgreSQL and Garage are written with their content in `compose.yaml` and again in `deploy/compose.staging.yaml`, and a change goes into both: a config of a file would be a bind mount of the checkout, which development avoids (see Development).
- **Declared or absent.** A service that an environment must not run is not declared in its file. No configuration value turns it on. Production declares no seed, no Dex, no Mailpit, no Garage and no LLM stand-in. Staging declares no Dex, no River panel, no telemetry and no LLM stand-in.
- **Staging.** `deploy/compose.staging.yaml`, of the project `software-factory-manager-staging`, declares PostgreSQL, Garage and `garage-cors`, Mailpit, Caddy, `migrate` and `seed`, which run once and exit, and `api` and `worker`. The API, the worker and the migration run the image of the API, each with its command; Caddy and the seed run their images, all tagged by `COMMIT`. `migrate` waits for PostgreSQL, the seed, the API and the worker wait for `migrate` to end well, and Caddy for `garage-cors`. The volumes `postgres-data` and `garage-data` are removed on every publish, and so is the container of Mailpit, which keeps its e-mails inside it; `caddy-data`, which holds the certificates, and `caddy-config` are kept while the server lives, so `docker compose down --volumes` is never run there.
- **Only Caddy publishes a port.** On a server Caddy publishes 80 and 443 over TCP, and 443 over UDP for HTTP/3, and no other service publishes one: Docker opens a published port past the firewall of the server. The services reach each other on the network of the project, by the name of the service.
- **Values.** Every value comes from the variables file of the server, except what the Compose file names itself: the services of its network and their ports (`FILE_STORAGE_URL`, `EMAIL_SMTP_ADDRESS`, `API_LISTEN_ADDRESS`, the host of the connections) and the sender of e-mail. A connection is written from the password of its role, so the variables file holds no URL. Each service lists the variables its command reads, by name, and none other (see By command in Configuration). A variable the file requires is written `${NAME:?...}`, and one with a default in the binary `${NAME:-}`.
- **Checked.** `task compose:check` validates `deploy/compose.staging.yaml` with `docker compose config --quiet`, with the values of `.env.example` and a value for each name only a server is given. Without `--quiet` the command prints every value, so it never runs on a server or in a workflow.

### Development

- **Tools on the machine.** `task dev` runs the API, the worker and Vite on the machine. The Compose file runs the services they talk to.
- **One origin.** The browser opens the app through Caddy, at `http://localhost`, which forwards `/api` and `/up` to the API on port 8080 of the machine and the rest to Vite on port 5173. The file storage is at `http://files.localhost`, which Caddy forwards to Garage; the API reaches Garage on the machine, at `GARAGE_PORT`. Caddy reaches the machine as `host.docker.internal`, which Compose maps to the host gateway. Docker Desktop reaches a server on 127.0.0.1 there, the default of `.env.example`; Docker Engine on Linux, the runner of the pipeline included, reaches only one on every interface, so there `API_LISTEN_ADDRESS` and `WEB_LISTEN_HOST` take `0.0.0.0`.
- **Variables.** Task loads `.env`, falls back to `.env.example` for what it lacks, and passes the variables to Compose, so development runs with no `.env`. A `.env` file, never committed, changes a value. A Compose command is run through its Task target.
- **One set of containers.** `compose.yaml` has a fixed project name, so every checkout of the repository, a Git worktree included, drives the same containers and volumes. Compose creates a container again when `up` runs from another directory and the container mounts a file of the checkout, because the path differs. PostgreSQL mounts none, so that the tests of one checkout keep their connections: the file it needs is a config written in the Compose file. Garage does the same, so that an upload in flight survives. Caddy mounts `deploy/Caddyfile` and `deploy/delivery.caddyfile`, and being created again costs it nothing.
- **File storage.** Garage runs on one node and creates the one bucket and its key, from the `FILE_STORAGE_` variables, when it starts. A step of the Compose file, `garage-cors`, gives the bucket its cross-origin rule, the origin of the app only, and Caddy waits for it, so `task services` returns once the rule is set.
- **A real LLM provider.** The stand-in is the default. Pointing at a real provider to work on a prompt is a change to the local `.env`, made by the maintainer. Nothing automated calls a real provider outside production (ADR-0019).

### Seed

- **Its own command.** The seed is a `main` package of its own in the Go module of the API, built into its own image. It is not a command of the API binary.
- **Through use cases.** It creates data by calling use cases, as a person would: it issues a setup code and concludes the setup with it. It runs no SQL.
- **Deterministic.** Two runs on an empty database produce the same data, with the same identifiers: each identifier is the next value of a sequence, and the seed takes the same steps in the same order. The times recorded, and the hash of the setup code it issues, are the moment of the run; the code is random, so a run cut short leaves no code anyone could guess. A run on a database whose setup is concluded changes nothing and succeeds. The tests of `cmd/seed` prove both.
- **Users.** Their e-mail addresses are derived from one mailbox taken from configuration, `SEED_MAILBOX` (`name+admin@`, `name+developer@`). The default is a fictitious mailbox, whose users are the ones in the configuration of Dex. The seed prints no address: in a container its output is a log.
- **Nothing real.** No real client, person or address appears in the seed (ADR-0021).
- **In development.** `task seed` runs the package on the machine, with `go run`, as `task migrate` runs the migrations: the tools run on the machine, and the services they talk to in Compose (see Development). The image is built from `apps/api/cmd/seed/Dockerfile`, with the Go module as its context, and declared by `compose.yaml` as the `seed` service, in a Compose profile of its own, because it runs once and exits and `task services` waits for services that stay up.
- **Staging.** Every publish to staging removes the volumes of PostgreSQL and Garage, and the e-mails of Mailpit, starts the three again, applies the migrations and runs the seed, from its image of the commit published, with `SEED_MAILBOX` at its fictitious default: Mailpit receives every e-mail, so the users of the seed log in with a login code read there (see Publishing in Pipeline). What the seed prints is in the public log of the job.

### LLM stand-in

- **Its own command**, in the same Go module, built into its own image.
- **What it answers.** The Chat Completions format, with fixed answers, streamed ones included, and the list of models that the health check calls.
- **Where.** Development and the pipeline, and staging once a feature calls an LLM: until then staging declares none. Its answers are fixed so that an end-to-end test can assert on them.

### Servers

- **One file.** `deploy/cloud-init.yaml` prepares every server: Docker from Docker's own repository, at the versions the file names; the deploy user, in Docker's group; the host key it is given; SSH by key only; the firewall; the rule that keeps every container from the metadata service of the provider; automatic security updates; and, last, the signal that the server is prepared. Its steps run as one script that stops at the first one that fails, so a server that is not prepared never gives the signal. The first step checks the file against cloud-init's own schema, since cloud-init only warns about a key it does not understand and goes on without it. The tool of the environments writes the keys of each new server into it and hands it to the provider.
- **Its choices.** The provider, the region, the size, the system image and the name of an environment's server are in `deploy/server.<environment>.yaml`, which the tool reads. Moving to another provider changes that file and adds an adapter to the tool, in `apps/api/tools/environment/provider`; the cloud-init file, the pipeline and the publish stay as they are. Hetzner's adapter, on hcloud-go, is the only one, and creates no firewall at Hetzner.
- **Nothing only by hand.** A change made by hand on the production server is written into that file as well, so a new server comes up the same.
- **Ports.** 22, 80 and 443, and no other: the firewall inside the server, ufw, lets nothing else in. Docker publishes the port of a container past ufw, through rules of its own in iptables, so the Compose file of a server publishes the ports of Caddy and nothing else.
- **The metadata of the provider.** Its service, at 169.254.169.254, gives the user data of the server, the private host key among it, to whatever asks from the server. The server itself asks it; no container reaches it. A unit of systemd that the cloud-init file writes, `containers-without-metadata.service`, drops what a container sends there, in `DOCKER-USER`, the chain of iptables through which Docker sends what a container sends out and which it never changes. It runs on every boot after ufw and before Docker, and Docker starts no container without it.
- **SSH.** Key login only, as the deploy user. No password and no root login. The tool generates the host key and gives it to the server, which offers no other (sshd's `HostKey` names it alone, and the keys the system makes again are deleted), so the tool, the pipeline and the maintainer trust that key alone, from the first connection, and refuse a server that answers with another before offering it the deploy key. Each new server gets a new deploy key and a new host key.
- **Staging, on and off.** By hand, from the maintainer's machine, in the main checkout or a worktree of it; GitHub Actions holds no credential of the provider. `task staging:on` makes sure the GitHub Environment `staging` and the label `staging` exist, creates the server, waits until it answers SSH with its host key and cloud-init has prepared it, within about ten minutes, and gives the GitHub Environment what reaches the server (see Secrets); a failure after the server was asked for destroys it, with its deploy key at the provider, takes its values out of GitHub, removes the local copy and exits with an error. Turning on when staging is on checks that the server answers, gives GitHub its values again and creates nothing. `task staging:off` destroys the server, its address and its deploy key at the provider, and deletes its values from GitHub but the password of Mailpit; when staging is off it succeeds and says so. `task staging:status` tells whether it is on, since when, at which address and host names, and which open pull request has the label `staging`. Nothing turns staging off by itself.
- **GitHub.** The tool reaches GitHub through `gh`, logged in on the machine as the maintainer, in the checkout it runs in, so `gh` finds the repository from its remote. A value, of a secret or of a variable, goes to `gh` on its standard input, never as an argument, which every process of the machine sees; its tests run a stand-in of `gh` that records both. An environment that exists is never written to, so what was set on it by hand stays. A secret or a variable is deleted only when the environment lists it, so turning off twice succeeds.
- **The password of Mailpit.** Chosen at random the first time staging is turned on, kept in the `.env` of the main checkout as `STAGING_MAILPIT_PASSWORD`, where the maintainer reads it, and written to GitHub on every turn-on.
- **The token.** `HETZNER_TOKEN`, the API token of the Hetzner project of staging, from the variables of the process or else from the `.env` of the main checkout. It never reaches GitHub or a server.
- **The local copy.** While an environment is on, the main checkout keeps in `.servers/<environment>/`, which git ignores, the deploy key, the host key as a line of known hosts and a configuration of OpenSSH that uses both: `ssh -F .servers/staging/ssh_config staging`, from the main checkout, logs in as the deploy user. Only the owner of the files can read them, which OpenSSH requires of a private key; on Windows that is an access list of their own. Turning off removes them.
- **Nothing printed.** No command of the tool prints a token, a private key or a password. Its tests run each command with known secrets and search what it printed, and what it gave GitHub for the token.
- **Staging address.** Provisional, until the domain is registered: each server has the host names `app`, `files` and `mail` under its own address written for `sslip.io`, with dashes (`app.203-0-113-10.sslip.io`), so they change each time staging is turned on (see [`docs/providers.md`](docs/providers.md)). The origin of the app is `https://` and the host name of the app. The one of Mailpit asks for a password.
- **Staging certificates.** Caddy keeps its certificates in a volume of the server that every publish keeps, so they last as long as the server. A new server has new host names, so no certificate is asked for twice.

### Secrets

- **Where.** The GitHub Environments `staging` and `production`. The secrets of `production` can be used only by a run of `main`. A job reads a secret or a variable of an environment only when it declares that environment.
- **What staging holds.** Only what reaches its server, written by `task staging:on` and deleted by `task staging:off` (see Servers): the secrets `DEPLOY_KEY`, the private deploy key in OpenSSH's format, and `MAILPIT_PASSWORD`, the password the browser asks for on the host name of Mailpit; and the variables `SERVER_ADDRESS`, the IPv4 address of the server, `SERVER_HOST_KEY`, its host key as a line of known hosts, and `TURNED_ON_AT`, when the server was created at the provider, in RFC 3339 and UTC. Turning on again writes the same time, so it counts from the first turn-on. The address is written last and deleted first: a publish takes staging for off while `SERVER_ADDRESS` is empty. Turning off leaves `MAILPIT_PASSWORD` alone. `gh` takes the line breaks off the end of a value it reads, so the deploy key arrives without its last one.
- **On the server.** The publish writes one variables file, readable by the deploy user alone. Compose loads it. On staging it is `staging/.env` in the deploy user's home, beside the Compose files, so that a command of Compose run there by hand reads it too: every name of `.env.example`, in its order, empty or with its value in single quotes, which Compose takes as it is, the dollar signs of a bcrypt hash included.
- **Checked first.** The publish compares the names in `.env.example` with the values it has and stops, before it logs in to the server, when one is missing. Staging decides a value for every name in `apps/api/tools/environment/variables.go`, of one of four kinds: fixed; derived from what the publish is given, which are the address of the server (the origins of the app, of the files and of Mailpit), the commit (`COMMIT`) and the password of Mailpit (`MAILPIT_PASSWORD_HASH`); drawn at random on each publish (the passwords of PostgreSQL, the secret of Garage, the key of the storage and the key of the login code), so that GitHub keeps nothing that lasts beyond the data it protects; or deliberately empty, for a name that nothing on staging reads or that its Compose file writes itself. A new name of `.env.example` gets its decision there in the same change; the tests of the tool prove that a name with none stops the publish before any remote step, that every variable the Compose files of staging require gets a value, and that each service of `deploy/compose.staging.yaml` that runs the binary or the seed receives, in its `environment:`, every variable its commands require, as `config.Load` reads them. A variable the binary comes to require, decided in `variables.go` but not listed for its service, so fails the checks instead of a publish that has removed the data already.
- **Never printed.** No step prints a variable, the variables file or the configuration of a container.
- **Kept twice.** The key that encrypts credential values and the passphrase of the backups are also in the maintainer's password manager, because GitHub does not give a secret back. The password of the Mailpit of staging is in the `.env` of the main checkout, for the same reason.
- **Not a secret.** A value that is not secret, such as a host name, is a variable of the GitHub Environment.

### Backup

- **Database.** pgBackRest archives the write-ahead log continuously, takes a full backup every week and a differential one every day, and keeps 30 days. The backups are encrypted and go to a bucket of their own at Backblaze B2.
- **Started by a timer** on the server, set up by the cloud-init file. Each backup ends by signalling the monitor (see Observability).
- **Restore test.** Every month a workflow creates a server, restores the latest backup there, checks it and destroys the server. It never runs on a GitHub runner.
- **Files.** The production bucket has object versioning. An old version is removed after 30 days.
- **Before a migration.** The publish takes a `pg_dump` when a migration is pending and keeps the last three on the server.
- **Out of the bucket.** The key that encrypts credential values and the passphrase of the backups are never stored with the backups.

## Pipeline (`.github`)

How a change reaches `main` and the servers. The repository is public, which sets some of these rules (ADR-0021).

### Flow

- **Every change is a pull request.** A short branch, a pull request, a squash merge. `main` accepts no direct push and merges only with the checks passing.
- **The maintainer merges.** An agent opens the branch and the pull request with `gh` when asked. Nothing merges by itself.
- **Staging is offered.** The question that asks to open a pull request also asks whether it goes to staging. When it does, the agent turns staging on first and opens the pull request with the label only once staging is on; it turns staging off only when the maintainer says so (ADR-0019).
- **A merge publishes.** A merge into `main` is published to production once the checks pass on `main`.

### Checks on a pull request

- **Nothing from outside.** A pull request runs every check that needs no outside service: format, lint, types, generated code up to date, the tests of use cases and of screens, the adapter tests against containers of PostgreSQL, Garage, Mailpit and Dex, and the end-to-end tests against the development Compose file.
- **The same commands.** A workflow calls Task targets. A check that fails in the pipeline fails the same way on the machine. `task check` runs every check of a pull request, the end-to-end test last, so it needs ports 8080 and 5173 free and restarts the shared Caddy; it is what runs before a pull request. `task check:quick` runs all of them but the end-to-end test, for daily use and beside `task dev`.
- **The images are built.** The three images a commit is published with are built on every pull request and published nowhere, so a change that breaks one fails before the publish exists. The backend job builds the image of the seed and of the API, with `task seed:image` and `task api:image`; the app job, which has the Node.js of the app, builds the web app and the Caddy image of the servers with it inside, with `task caddy:image`, which runs `task web:build`, the one build of the app. `task check` and `task check:quick` build the three too.
- **The Compose file of staging is checked.** The backend job runs `task compose:check`, as `task check` and `task check:quick` do (see Compose files in Environments).
- **Workflows are checked too.** actionlint and zizmor run on every workflow file, each from its image: `task workflows:check`, which `task check` and `task check:quick` run as well. The system libraries Chromium needs on the Linux runner are installed by `task install:linux`, and the packages apt downloads for them are kept in the cache of GitHub Actions, for each system image of the runner.
- **Documents only.** A pull request that changes only documents skips the builds and the tests. `VERSION` is not a document (see Releases).
- **One required check.** The ruleset of `main` requires a single check: the `Checks` job of `ci.yml`, which fails when a job it needs failed or was cancelled, and passes when one was skipped. A new check is a job added to its `needs`, never a change to the ruleset.

### Scheduled workflows

- **Outside services.** The tests of the `resend` adapter run every day and on a manual trigger, with a key of their own: `resend.yml`, with the secret `RESEND_TEST_API_KEY`. Until the secret exists they skip and the run passes. No scheduled test calls a real LLM provider (ADR-0019).
- **Vulnerabilities.** govulncheck, `pnpm audit` and Trivy on the images run every week.
- **New versions.** `task outdated` runs every week and its list is kept in one open issue on GitHub. The workflow rewrites the issue, adds a comment when the list changed and closes it when the list is empty. It opens no pull request. The issue is a notice: work is still tracked in `.scratch/` (see [`docs/agents/issue-tracker.md`](docs/agents/issue-tracker.md)).
- **Restore test.** Every month (see Backup).
- **Staging reminder.** Nothing turns staging off by itself, and the pipeline holds no credential of the provider (see Servers). `staging-reminder.yml` runs every six hours and on a manual trigger, and fails, giving how many hours staging has been on, once `TURNED_ON_AT` is more than 24 hours old; it passes while the variable is empty or recent. It reads that variable of the GitHub Environment `staging` and nothing else (see Secrets), names no secret, and its token has no permission. A variable of an environment reaches only a job that declares the environment, so its job declares `staging` with `deployment: false`, which records no deployment: the history of staging keeps only the publishes. Such a job still waits for required reviewers and a wait timer, and fails at once on a custom protection rule, so `staging` has none of them. A run while the environment does not exist creates it, empty, as `task staging:on` would.
- **A failure warns.** A scheduled workflow that fails warns the maintainer and blocks no pull request.

### Publishing

- **Images.** GitHub Actions builds the images, tags each with the hash of its commit and pushes it to the GitHub container registry, where it is public. Production runs only an image built from `main`. A commit has three, each built by a Task target that publishes nothing, under a local name the publish tags again: the API (`software-factory-manager-api`, from `apps/api/cmd/api/Dockerfile`, by `task api:image`), Caddy with the built web app (`software-factory-manager-caddy`, the stage `servers` of `deploy/caddy/Dockerfile`, whose context is the output of `task web:build`, by `task caddy:image`), and the seed (`software-factory-manager-seed`, from `apps/api/cmd/seed/Dockerfile`, by `task seed:image`). The Caddy image of development is the stage `development` of the same Dockerfile, with no app. The stage published of each carries the label `org.opencontainers.image.source`, the address of this repository, which links its package in the registry to the repository. The server pulls without logging in, so each package is public: the three came up public at their first push, with nothing done by hand, and a new one that came up private would be made public once, by hand (see [`docs/providers.md`](docs/providers.md)). Each image is pushed as one manifest, with no attestation beside it (`BUILDX_NO_DEFAULT_ATTESTATIONS`).
- **On the server.** The workflow logs in over SSH, writes the variables file, pulls the images and restarts the Compose services, in the order the README describes: the dump when a migration is pending, the migration, then the API and the worker.
- **Staging.** The job `Publish to staging` of `ci.yml` publishes a pull request of this repository that has the `staging` label, once `Checks` has passed. `ci.yml` runs on a label added to a pull request too, so that adding `staging` to one that is open runs its checks again and publishes it; a run started by adding another label publishes nothing. The job is not among the jobs `Checks` needs, so the required check does not change, and it declares the GitHub Environment `staging`, whose values a job reads only when it declares it and only once it runs: its first step tells whether staging is on, and when `SERVER_ADDRESS` is empty it writes a notice, publishes nothing and passes. It asks for reading the contents, writing packages, to push the images, and writing pull requests, to read the open pull requests, take the label off them and comment, and restores no cache. The first publish proved the comment with that permission; should GitHub refuse it taking the label off, the comment of its `permissions` names the change, `issues: write`. It builds the three images of the commit the checks tested, the merge of the pull request, with the Task targets the checks use, pushes them tagged with its hash to `ghcr.io/dannilodms/software-factory-manager-{api,caddy,seed}`, the names `deploy/compose.staging.yaml` runs, and runs the publish of the tool of the environments, which receives the commit, the pull request, the address, the host key, the deploy key and the password of Mailpit through its environment, never as arguments.
- **The publish of staging.** It checks every value before it logs in. It reads again whether the pull request is open and has the label, and publishes nothing when it is not: the run of a push made before the label came off, by hand or by another publish, may reach its publish after. Then, in this order, it: logs in as the deploy user, trusting only the host key it is given; copies the Compose files and the two Caddyfiles of `deploy/` into `staging/` of the deploy user's home; writes the variables file; pulls the images; stops the API and the worker; removes the containers of PostgreSQL, Garage and Mailpit, then the volumes of the first two, so that no e-mail of an earlier pull request is left, a login code among them; starts PostgreSQL, Garage and `garage-cors`, Mailpit and Caddy, waiting until each is healthy or done; runs the migration, then the seed, each once with `docker compose run --rm`; starts the API and the worker; and waits, for up to five minutes, until `https://` and the host name of the app answers `/up` with 200, asked from where the tool runs, trusting the certificate authorities of the system, which proves the host name, the firewall, the certificate, Caddy and the readiness of the API. From the migration on, each step starts its services with `--no-deps`, so that none starts again what an earlier step started. A step that fails stops the publish and names the step: the next publish starts from the beginning. It then takes the label off every other open pull request that has it, with a comment there naming the pull request staging shows now, so that a push to one of them publishes nothing, and comments on the pull request it published the address of the app and of Mailpit, with the user Mailpit asks for, `maintainer`, and the name of its password in the `.env` of the main checkout. It tries each of those, and fails once they were all tried when one failed. It reads the open pull requests from GitHub's list of them, not from its search, which can still list a label taken off a moment ago. What the commands on the server print reaches the log of the job; none of them carries a value, and none prints the variables file or the configuration of a container.
- **Never cancelled.** Publishes share the concurrency group `staging`, which never cancels the one running; one waiting gives its place to a newer one. The runs of a pull request with the label have a concurrency group of their own, apart from its runs without it, and wait for each other, so that a push never cuts a publish short; a new run of a pull request without the label cancels the run before it, which never publishes. Adding the label starts a run beside the checks in flight without it, which go on, and cancels the run with it in flight: `gh pr create --label staging` adds the label just after it opens the pull request, so the run of `opened` already has it, and the pull request is then published once, not twice. That run is a publish only if the label was taken off by hand and added again while it published, and the new run publishes again. A run started by adding another label has a group of its own, so that it cancels no checks in flight and takes the place of no publish waiting.
- **Going back.** A manual workflow publishes an earlier commit of `main`, named by its hash or by a release.

### Releases

A release gives a number and notes to a milestone that is already in production. It publishes nothing new (ADR-0023).

- **One number.** The whole system has one version, in the `VERSION` file at the root: three numbers, with no `v` and nothing after them.
- **Numbering.** Below 1.0.0 the number reads `0.MILESTONE.FIX`. A milestone raises the middle number and sets the last to zero. A release that only fixes raises the last.
- **When.** The maintainer decides, when a deliverable is complete in production. Nothing makes a release by itself.
- **A pull request.** A release is a pull request that changes `VERSION` and `CHANGELOG.md` and nothing else. An agent opens it when asked, proposing the number and the notes. It runs every check and is published like any other.
- **Notes.** `CHANGELOG.md` is in English, with the newest release first. Each section is written from the pull requests merged since the last release: what changed for someone who uses the system and, under a heading of its own, what someone who installs it has to do, such as setting a new variable. It has no section for what is not released yet.
- **Tag.** After a publish to production succeeds, when no tag exists for the number in `VERSION`, the publish workflow creates the tag `v` plus the number on that commit and a GitHub Release with that version's section of `CHANGELOG.md`. Nobody creates a tag by hand.
- **Images.** The same step adds the number, without the `v`, as a second tag to the images of that commit. It builds nothing. Production keeps running images by their commit hash.
- **A failed publish.** It creates no tag. The tag goes to the next commit published with that number.
- **Between releases.** The build takes its version from `VERSION`, so it shows the last release it contains. The commit tells one build from another.
- **Only forward.** A tag is never moved or deleted, and a ruleset on GitHub refuses both. A fix goes into `main` and becomes a new release. There are no release branches and no pre-releases.
- **Permission.** Only the job that creates the tag may write to the repository contents and add a tag to an image.

### Public repository

- **No `pull_request_target`.** No workflow uses that event. A pull request from a fork gets no secret and is never published to staging.
- **Approval.** A workflow of an outside contributor runs only after approval.
- **Least permission.** Every workflow declares the permissions of its token, and a job asks for no more than it uses.
- **Public logs.** What a step prints is public. See Secrets.
- **Push protection.** GitHub's secret scanning with push protection is turned on.

### On the maintainer's machine

Agents reach GitHub through the `gh` command-line tool: pull requests, runs and their logs, secrets, manual triggers. So does the tool of the environments (see Servers in Environments). The permissions of the token `gh` holds have one home, its row in the GitHub section of [`docs/providers.md`](docs/providers.md). No GitHub MCP server is configured.

## Versions (the whole repository)

Every tool, library and container image runs at a version written in a committed file, and so does the system itself. Each version has one home, and an update is a change to that file.

### Where each version lives

| What | Home |
| --- | --- |
| The system itself | `VERSION` at the root (see Releases) |
| Go toolchain | the `go` and `toolchain` lines of `go.mod` |
| Go libraries, hcloud-go, the library of the provider, included | `go.mod` and `go.sum` |
| Go command-line tools (sqlc, vacuum, oasdiff, the River CLI) | the `tool` line of a module file of its own beside `go.mod`, one for each tool (`apps/api/sqlc.mod`), run with `go tool -modfile=sqlc.mod` |
| The Go linter and formatter (golangci-lint, with gofumpt inside it) | the `tool` line of `apps/api/golangci-lint.mod`, a module file of its own, run with `go tool -modfile=golangci-lint.mod` |
| Node.js | `devEngines.runtime` in `apps/web/package.json`, written by `vp env pin` |
| pnpm | `devEngines.packageManager` in `apps/web/package.json`, written by `vp env pin` |
| JavaScript libraries and tools, Vite+ included | the catalog in `apps/web/pnpm-workspace.yaml`, and `apps/web/pnpm-lock.yaml` |
| Container images (PostgreSQL, Garage, Mailpit, Dex, Caddy, OpenTelemetry Collector, Grafana otel-lgtm) | the image tag in `deploy/compose.base.yaml` and in each Dockerfile |
| What the images built in this repository are built on (the API, Caddy with the built web app, the seed) | the `FROM` lines of their Dockerfiles: `apps/api/cmd/api/Dockerfile`, `deploy/caddy/Dockerfile` and `apps/api/cmd/seed/Dockerfile`. The Go image of the API and of the seed names the Go version of `go.mod` again and is updated with it. No Dockerfile names Node.js: the app is built outside the image, by `task web:build`, and the Caddy image copies its output |
| Caddy plugins | the Dockerfile that builds the Caddy image |
| pgBackRest | the Dockerfile that builds the PostgreSQL image |
| GitHub Actions and the tools a workflow installs (Task, Trivy) | the workflow file: an action by its full commit hash, a tool by its exact version |
| The linters of the workflows (actionlint, zizmor), run from their images | a variable of `workflows:check` in `Taskfile.yml` whose name ends in `_IMAGE`: the image by its tag and by the digest of that tag in its registry, `name:1.2.3@sha256:...` |
| The system image of a server | `system_image` in the choices of its environment, `deploy/server.<environment>.yaml` |
| Docker on the servers | the `packages` of `deploy/cloud-init.yaml`, each at the exact version of Docker's repository built for the release of Ubuntu of the system image |
| MCP servers for agents | `.mcp.json` |
| Docker, Task, the global `vp`, the installed Go and the GitHub CLI | the prerequisites table of `README.md`, as a minimum version |
| What the maintainer's machine has installed (the prerequisites above, Git, Node.js and its version manager, pnpm) | `docs/machine-tools.yaml`, as the version in use |

### Rules

- **Exact.** A version is exact, and `task versions:check` rejects one that is not (see Enforcement). Only the prerequisites table holds minimums.
- **Catalog.** `apps/web`, the only JavaScript package, takes its versions from the pnpm catalog (`catalog:`), so each version is written once, beside the settings of pnpm. The root of the repository has no `package.json`.
- **Lockfiles.** `go.sum` and `pnpm-lock.yaml` are committed. The build installs from them and fails when they would change.
- **Nothing global.** A tool the build uses runs from its home above (`go tool`, `vp`), never from a copy installed on the machine. Docker, Task, the global `vp`, the GitHub CLI and the installed Go, which only downloads the toolchain that `go.mod` names, are the only exceptions.
- **One-off commands.** A command that downloads what it runs (`npx`, `pnpm dlx`, `go run` with a module path) carries an exact version.
- **Copied code.** The shadcn/ui components in `apps/web/src/ui` have no version. Updating one means running the shadcn CLI again for that component and reviewing the diff.
- **Prose.** A document names a version only when a rule depends on it, as a minimum (PostgreSQL 18, pgx 5.13.0 or later). The version in force is read from its home. A number that must appear in two files is listed in both as such, as in [`docs/agents/browser-tools.md`](docs/agents/browser-tools.md).

### Updating

- **Listing.** `task outdated` lists what has a newer release among the versions the repository pins: the Go toolchain, Go modules and tools, Node.js, pnpm, JavaScript packages, container images, Caddy plugins, pgBackRest, GitHub Actions and the tools a workflow installs, the system image of the servers, Docker on the servers and MCP servers. It is written when the deploy is built.
- **Every week.** A scheduled workflow runs the listing and warns the maintainer through one issue (see Scheduled workflows). Updating stays a decision of the maintainer.
- **The machine.** `task outdated:machine` is run by hand on the maintainer's machine, which no workflow can see. It reads `docs/machine-tools.yaml` and lists the tools that have a newer release and the ones whose installed version differs from the recorded one. After an update the file is corrected (see `AGENTS.md`).
- **One at a time.** An update is its own commit: one dependency, or one family released together (TanStack, OpenTelemetry), with the build and the tests passing.
- **Major versions.** A major version is updated after reading its migration notes. When it changes what an ADR decided, that ADR is updated in the same commit.
- **Pre-releases.** A library adopted before its stable release (TanStack DB) moves to the stable release as soon as there is one.

### Enforcement

`task versions:check` rejects a range (`^`, `~`), `latest` and a floating tag such as `postgres:18`. It is `apps/api/tools/guard/versions`, and `task check`, `task check:quick` and the pipeline run it. It recognises a file by its name, wherever the file is, and reads:

- **Go module files** (`go.mod`, and the module file of each tool: `golangci-lint.mod`, `sqlc.mod`, `vacuum.mod`, `oasdiff.mod` and `river.mod`): the `go` line and the `toolchain` line with their three numbers, and each required module at one release. A tool takes its version from the line that requires its module.
- **Compose files and Dockerfiles**: every `image`, every `FROM` and every `COPY --from`. A tag starts with the full version of the image, which has three numbers; the images whose version has two, such as PostgreSQL, are listed in the check. An image with another scheme is pinned by its digest. A service that is built from a Dockerfile is not judged, and neither is an image this repository publishes, `ghcr.io/dannilodms/software-factory-manager-<name>`, tagged with one variable of Compose, `${COMMIT...}`: its versions are in its Dockerfile, and the publish gives the commit. In a Dockerfile, `xcaddy build` too: the Caddy version, when one is named, and each plugin, as `--with module@v1.2.3`.
- **Workflows**, and any other YAML file under `.github`: an action by its full commit hash, a tool by the three numbers of a variable whose name ends in `_VERSION` or of an input named `version` or ending in `-version`, an image as above, and a runner by the version of its system, never `-latest`.
- **The pnpm catalog** in `pnpm-workspace.yaml`, with its overrides, and `package.json`: a dependency is `catalog:` or one version, and so are the runtime and the package manager.
- **`.mcp.json`**: a server that `npx` or `pnpm dlx` downloads names its package as `name@1.2.3`.
- **`Taskfile.yml`**: a variable under `vars` whose name ends in `_IMAGE`, as an image above, such as the image of a linter that `workflows:check` runs, and one whose name ends in `_VERSION`, by its three numbers. A tag followed by its digest passes, and so does a tag alone: the check asks no image for its digest, which the linters carry by the rule of their row above.

What the check does not read stays a rule for the author: pgBackRest in its Dockerfile, and a one-off command in `Taskfile.yml` that names its version in its text. Each joins the check when its file is written. The system image of the servers and Docker on them are judged by the tests of `apps/api/tools/environment` instead, which `task api:test` runs: each package of Docker in the cloud-init file names an exact version built for the release of Ubuntu that the system image of every environment names, so the two change together.
