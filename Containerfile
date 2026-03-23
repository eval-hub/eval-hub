# Multi-stage build for the evaluation hub Go service
# Build stage
FROM --platform=$BUILDPLATFORM registry.access.redhat.com/ubi9/go-toolset:1.25 AS builder

ARG TARGETARCH

USER 0

# Set working directory
WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for versioning, please ensure to modify also in the Runtime stage below
ARG BUILD_NUMBER=0.3.0
ARG BUILD_DATE
ARG BUILD_PACKAGE=main

# Build eval-hub binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s -X '${BUILD_PACKAGE}.Build=${BUILD_NUMBER}' -X '${BUILD_PACKAGE}.BuildDate=${BUILD_DATE}'" \
    -a -installsuffix cgo \
    -o eval-hub \
    ./cmd/eval_hub

# Build eval-runtime-sidecar binary (same image can run either via container command override)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s -X '${BUILD_PACKAGE}.Build=${BUILD_NUMBER}' -X '${BUILD_PACKAGE}.BuildDate=${BUILD_DATE}'" \
    -a -installsuffix cgo \
    -o eval-runtime-sidecar \
    ./cmd/eval_runtime_sidecar

# Build the eval runtime init binary (S3 test-data download)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s -X '${BUILD_PACKAGE}.Build=${BUILD_NUMBER}' -X '${BUILD_PACKAGE}.BuildDate=${BUILD_DATE}'" \
    -a -installsuffix cgo \
    -o eval-runtime-init \
    ./cmd/eval_runtime_init

# Runtime stage
FROM --platform=$TARGETPLATFORM registry.access.redhat.com/ubi9/ubi-minimal:latest

# Create user and app directory
RUN groupadd -g 1000 evalhub && \
    useradd -u 1000 -g evalhub -s /bin/bash -m evalhub && \
    mkdir -p /app/config && \
    chown -R evalhub:evalhub /app

# Copy both binaries from builder
COPY --from=builder --chown=evalhub:evalhub /build/eval-hub /app/eval-hub
COPY --from=builder --chown=evalhub:evalhub /build/eval-runtime-sidecar /app/eval-runtime-sidecar
COPY --from=builder --chown=evalhub:evalhub /build/eval-runtime-init /app/eval-runtime-init


# The config file should not really be part of the image.
COPY --chown=evalhub:evalhub config/config.yaml /app/config/config.yaml
COPY --chown=evalhub:evalhub config/providers /app/config/providers
COPY --chown=evalhub:evalhub config/collections /app/config/collections

# Set working directory
WORKDIR /app

# Switch to non-root user (numeric UID so Kubernetes runAsNonRoot can verify)
USER 1000

# Expose service port
EXPOSE 8080

# Environment variables
ENV PORT=8080 \
    TZ=UTC

# Redeclare build ARGs for labels (ARGs don't cross stage boundaries)
ARG BUILD_NUMBER=0.3.0
ARG BUILD_DATE

# Labels for metadata
LABEL org.opencontainers.image.title="eval-hub" \
      org.opencontainers.image.description="Evaluation Hub REST API service" \
      org.opencontainers.image.version="${BUILD_NUMBER}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.authors="eval-hub" \
      org.opencontainers.image.vendor="eval-hub"

# Health check removed - wget not available without package installation

# Run the binary
CMD ["/app/eval-hub"]
