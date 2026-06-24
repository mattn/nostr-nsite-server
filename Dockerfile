# syntax=docker/dockerfile:1

# pin the build stage to the native runner arch so Go cross-compiles to the
# target arch instead of running the whole toolchain under QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
WORKDIR /src

# cache module downloads
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# build a static binary (no CGO) so it runs on a scratch/distroless base.
# GOARCH comes from buildx; the compile runs natively, only the output differs.
ARG TARGETOS TARGETARCH
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" \
    -o /out/nostr-nsite-server .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/nostr-nsite-server /usr/local/bin/nostr-nsite-server
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/nostr-nsite-server"]
