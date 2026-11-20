# The image of the API: the binary of cmd/api alone, built from the Go
# module of the API, which is the build context. The servers run it with each
# of its commands: with none it serves the API, and `migrate`, `worker` and
# `issue-setup-code` are the command of a Compose service or of a run, such as
# `docker compose run --rm api issue-setup-code`. `probe-readiness` is the
# health check of the container, which has no shell to run one with.
# Development runs the binary on the machine instead (ADR-0019). Rules:
# Commands, Health, Publishing and Versions in CODING_STANDARDS.md.

FROM golang:1.27.4-alpine AS builder

WORKDIR /source
# The modules first, so that a change to the code downloads them again only
# when go.mod or go.sum changed. The build fails when go.sum would change.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Static, so that it runs on an image with no C library. The cache of the
# compiler is kept by Docker between builds on the same machine, so a build
# after a change compiles only what changed; it leaves nothing in the image.
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -o /api ./cmd/api

# No shell and no package manager: the binary, the root certificates and a
# user that is not root. The image has no version of its own, so it is pinned
# by its digest, the one the image of the seed names.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

# The repository the image is built from, which links its package in the
# GitHub container registry to the repository.
LABEL org.opencontainers.image.source="https://github.com/dannilodms/software-factory-manager"

COPY --from=builder /api /api
ENTRYPOINT ["/api"]
