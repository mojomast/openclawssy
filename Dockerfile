# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN make build

# Runtime stage
FROM alpine:3.22

WORKDIR /app

# Install runtime dependencies for shell-heavy agent workflows.
RUN apk add --no-cache \
    bash \
    bind-tools \
    ca-certificates \
    coreutils \
    curl \
    docker-cli \
    findutils \
    gawk \
    git \
    grep \
    iproute2 \
    iputils \
    jq \
    lsof \
    make \
    mtr \
    net-tools \
    netcat-openbsd \
    nmap \
    nodejs \
    npm \
    openssh-client \
    openrc \
    openssl \
    procps \
    py3-pip \
    py3-virtualenv \
    python3 \
    sed \
    socat \
    tcpdump \
    traceroute \
    tzdata \
    unzip \
    zip \
    wget \
    && ln -sf /usr/bin/python3 /usr/local/bin/python \
    && ln -sf /usr/bin/pip3 /usr/local/bin/pip

# Copy binary from builder
COPY --from=builder /app/bin/openclawssy /usr/local/bin/openclawssy

# Copy entrypoint script
COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Create workspace directory
RUN mkdir -p /app/workspace

# Expose the default port
EXPOSE 8080

# Set the entrypoint
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

# Default command (can be overridden)
CMD ["serve", "--token", "change-me", "--addr", "0.0.0.0:8080"]
