# syntax=docker/dockerfile:1

# Build stage. It runs on the build machine's own architecture and
# cross-compiles for the target one (no emulation), which is fast because
# the binary needs no C compiler.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/readysetgo \
 && mkdir /out/data

# Final stage: no shell or package manager, running as a non-root user.
# Maintenance runs as subcommands: docker compose exec app /app migrate status
FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
COPY --from=build /out/app /app
# An empty /data owned by the app's user, so a volume mounted there starts
# out writable.
COPY --from=build --chown=nonroot:nonroot /out/data /data
EXPOSE 8080
ENTRYPOINT ["/app"]
CMD ["serve"]
