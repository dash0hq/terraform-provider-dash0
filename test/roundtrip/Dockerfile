FROM ghcr.io/dash0hq/cli:latest AS dash0-cli

# ---------------------------------------------------------------------------
# Build the Terraform provider from source.
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS provider-build
RUN apk add --no-cache make
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build

# Set up the filesystem mirror so tofu can find the locally-built provider.
RUN mkdir -p /provider-mirror/registry.opentofu.org/dash0hq/dash0/0.0.1/linux_$(go env GOARCH)/ && \
    cp terraform-provider-dash0 \
       /provider-mirror/registry.opentofu.org/dash0hq/dash0/0.0.1/linux_$(go env GOARCH)/terraform-provider-dash0_v0.0.1

# ---------------------------------------------------------------------------
# Runtime image with all tools the tests need.
# ---------------------------------------------------------------------------
FROM python:3.13-alpine

# Install bash and curl.
RUN apk add --no-cache bash curl

# Install OpenTofu.
RUN ARCH="$(uname -m)" && \
    case "$ARCH" in \
      x86_64)  TOFU_ARCH="amd64" ;; \
      aarch64) TOFU_ARCH="arm64" ;; \
      *)       echo "Unsupported arch: $ARCH" && exit 1 ;; \
    esac && \
    TOFU_VERSION="1.11.5" && \
    curl -fsSL "https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/tofu_${TOFU_VERSION}_linux_${TOFU_ARCH}.tar.gz" \
      | tar -xz -C /usr/local/bin tofu

# Install PyYAML (the only Python dependency).
RUN pip install --no-cache-dir PyYAML

# Copy the dash0 CLI binary from the official image.
COPY --from=dash0-cli /dash0 /usr/local/bin/dash0

# Copy the locally-built provider mirror.
COPY --from=provider-build /provider-mirror /root/terraform-provider-mirror

# Copy the test scripts.
COPY test/roundtrip/ /tests/

WORKDIR /tests
ENTRYPOINT ["bash"]
