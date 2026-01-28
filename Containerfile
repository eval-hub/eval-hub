# Multi-stage build for the evaluation hub Go service
# Build stage
FROM registry.access.redhat.com/ubi9/go-toolset:1.25 AS builder

USER 0

# Set working directory
WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for versioning
ARG BUILD_NUMBER=0.0.1
ARG BUILD_DATE
ARG BUILD_PACKAGE=main

# Build the binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X '${BUILD_PACKAGE}.Build=${BUILD_NUMBER}' -X '${BUILD_PACKAGE}.BuildDate=${BUILD_DATE}'" \
    -a -installsuffix cgo \
    -o eval-hub \
    ./cmd/eval_hub

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

# Create user and app directory
RUN groupadd -g 1000 evalhub && \
    useradd -u 1000 -g evalhub -s /bin/bash -m evalhub && \
    mkdir -p /app/config && \
    chown -R evalhub:evalhub /app

# Copy binary from builder
COPY --from=builder --chown=evalhub:evalhub /build/eval-hub /app/eval-hub

# Copy configuration file
COPY --chown=evalhub:evalhub config/server.yaml /app/config/server.yaml

# Set working directory
WORKDIR /app

# Switch to non-root user
USER evalhub

# Expose service port
EXPOSE 8080

# Environment variables
ENV PORT=8080 \
    TZ=UTC

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
