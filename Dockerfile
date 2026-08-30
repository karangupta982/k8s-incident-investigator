# Stage 1: Build
FROM golang:1.23-alpine AS builder

WORKDIR /workspace

# Copy go module files first for layer caching
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

# Copy source
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

# Build the manager binary
# CGO_ENABLED=0 for static binary, GOOS=linux for Linux target
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager ./cmd

# Stage 2: Runtime — minimal distroless image
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/manager .

# Run as non-root user (UID 65532 = nonroot in distroless)
USER 65532:65532

ENTRYPOINT ["/manager"]
