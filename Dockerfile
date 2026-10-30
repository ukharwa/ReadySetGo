# syntax=docker/dockerfile:1

# Build stage. It runs on the build machine's own architecture and
# cross-compiles for the target one (no emulation), which is fast because
# the binary needs no C compiler.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/kickstart \
 && mkdir /out/data

# Final stage: no shell or package manager, running as a non-root user.
# Maintenance runs as subcommands: docker compose exec app /app migrate status
FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
COPY --from=build /out/app /app
# An empty /data owned by the app's user, so a volume mounted there starts
# out writable.
COPY --from=build --chown=nonroot:nonroot /out/data /data
EXPOSE 8080
ENTRYPOINT ["/app"]
CMD ["serve"]
