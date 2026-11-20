# The Caddy image of every environment: Caddy with a rate-limit plugin, which
# Caddy does not have. The plugin and the versions are pinned here, and
# `task versions:check` reads both. Rules: Rate limit and Compression in
# Delivery, Publishing and Versions in CODING_STANDARDS.md.
#
# Two images come out of it. `development`, which compose.yaml builds with
# this folder as its context, holds no app: Vite serves it there. `servers`,
# which `task caddy:image` builds with the built web app as its context,
# holds the app, which Caddy serves on the servers.

FROM caddy:2.11.9-builder-alpine AS builder

# The builder image names its own Caddy version, so xcaddy builds 2.11.9.
RUN xcaddy build --with github.com/mholt/caddy-ratelimit@v0.1.0

FROM caddy:2.11.9-alpine AS development

COPY --from=builder /usr/bin/caddy /usr/bin/caddy

# The same Caddy, with the files of the built web app, their Brotli copies
# included, in the folder Caddy's image serves. The build context is the
# output of `task web:build`, so no Node.js version is named here: Node keeps
# its one home in apps/web/package.json.
FROM development AS servers

# The repository the image is built from, which links its package in the
# GitHub container registry to the repository.
LABEL org.opencontainers.image.source="https://github.com/dannilodms/software-factory-manager"

COPY . /srv
