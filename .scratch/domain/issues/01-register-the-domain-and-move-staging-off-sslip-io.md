# 01: Register the domain and move staging off sslip.io

**What to build:** Staging's host names are under `sslip.io` until the system has a domain of its own. That is provisional. `sslip.io` is not on the Public Suffix List, so its certificates share one Let's Encrypt limit with every user of it, and that limit has run out in some weeks. The real login providers, Google, Microsoft and GitHub, also need a fixed address to register. The maintainer chooses a registrar and a DNS host and registers the domain. An agent then moves staging, and later production, to host names under it. See ADR-0019 and `docs/providers.md`, Domain and DNS.

**Blocked by:** None (can start immediately). It is in a folder of its own so that it outlives the effort of staging, whose folder is removed when that effort ends.

**Status:** ready-for-human

- [ ] The maintainer has chosen a registrar and a DNS host and registered the domain. `docs/providers.md` records them, with the yearly cost.
- [ ] Decided with the maintainer: how staging's host names follow a server whose address changes each time it is turned on. One option is a reserved address with records set once by hand. Another is a DNS host with an API, updated by the tool on the machine, never by the pipeline.
- [ ] Decided with the maintainer: how Caddy's certificate data outlives a staging server once the host names are fixed. Let's Encrypt refuses more than five certificates for the same set of names in a week, and every new server would ask for the same ones. One option is a private bucket, saved before the server is destroyed and restored on the next one, as the first version of ADR-0019 planned. Another is a provider volume moved from one server to the next. Either is done by the tool on the machine, never by the pipeline.
- [ ] Turning staging off and on again more than five times in a week keeps HTTPS working, without a new certificate for the same names.
- [ ] Staging's host names are under the domain, and nothing refers to `sslip.io` any more.
- [ ] ADR-0019 no longer calls staging's address provisional.
