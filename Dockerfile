# Stage 1: Build — use buildx ARG for multi-architecture support
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy module files first for layer caching
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

# Copy source
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

# Build for the target platform
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -a -ldflags="-s -w" -o manager ./cmd

# Stage 2: Runtime — minimal distroless image
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/manager .

# Run as non-root user (UID 65532 = nonroot in distroless)
USER 65532:65532

ENTRYPOINT ["/manager"]
