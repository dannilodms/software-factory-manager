# The image of the seed: the binary of cmd/seed alone, built from the Go
# module of the API, which is the build context. Development declares it in
# compose.yaml, in a profile of its own, and staging runs it on every publish;
# production never has it (ADR-0019). Rules: Seed in Environments and
# Versions in CODING_STANDARDS.md.

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
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -o /seed ./cmd/seed

# No shell and no package manager: the binary, the root certificates and a
# user that is not root. The image has no version of its own, so it is pinned
# by its digest.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

# The repository the image is built from, which links its package in the
# GitHub container registry to the repository.
LABEL org.opencontainers.image.source="https://github.com/dannilodms/software-factory-manager"

COPY --from=builder /seed /seed
ENTRYPOINT ["/seed"]
