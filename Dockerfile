# syntax=docker/dockerfile:1

# Build the manager binary on the runner's native architecture. Go cross-compiles
# to the requested image platform, so an amd64 workstation can still build the
# arm64 image without running the Go toolchain under QEMU.
FROM --platform=$BUILDPLATFORM golang:1.20 AS builder
ARG TARGETOS
ARG TARGETARCH
ARG TARGET=operator

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# Cache dependencies before copying source so source changes do not invalidate
# the module download step. The mount is reused by persistent local builders.
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the go source
COPY cmd/${TARGET}/main.go cmd/${TARGET}/main.go
COPY api/ api/
COPY controllers/ controllers/

# Build without -a so unchanged standard-library and module packages can be
# reused across builds. CI publishes arm64 only, while these automatic BuildKit
# arguments keep local cross-platform builds working.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -o /out/manager cmd/${TARGET}/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /out/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
