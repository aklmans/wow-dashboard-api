FROM golang:1.26.3-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build both the API server and the background worker out of the same module
# so the final image can serve either role just by overriding the entrypoint.
# CGO is off + static linking so the binaries drop straight into distroless.
# TARGETOS / TARGETARCH come from `docker buildx` and let one Dockerfile
# produce linux/amd64 and linux/arm64 images for GHCR.
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH

RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api
RUN go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker
RUN go build -trimpath -ldflags="-s -w" -o /out/river-migrate ./cmd/river-migrate
RUN go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static-debian12:nonroot

# Image layout (deliberately minimal — app schema migrations live outside
# the image; see docs/operations.md for the runbook):
#   /api            — main HTTP server, default entrypoint, listens on 7272
#   /worker         — River background-job worker; override entrypoint
#   /river-migrate  — applies River's queue schema (idempotent, run once)
#   /healthcheck    — liveness probe binary for the HEALTHCHECK below
COPY --from=builder /out/api /api
COPY --from=builder /out/worker /worker
COPY --from=builder /out/river-migrate /river-migrate
COPY --from=builder /out/healthcheck /healthcheck

EXPOSE 7272

# Liveness probe for the default (/api) entrypoint. distroless has no shell or
# curl, so the check runs the static /healthcheck binary, which GETs /healthz on
# $PORT. The /worker and /river-migrate entrypoints serve no HTTP — disable the
# check for those (docker `--no-healthcheck`, or compose `healthcheck.disable`).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/healthcheck"]

ENTRYPOINT ["/api"]
