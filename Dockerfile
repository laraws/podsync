# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS builder

ARG BUILDARCH
ARG TARGETOS
ARG TARGETARCH

# Compile Go natively and use a C cross-compiler for SQLite when needed.
RUN if [ "$BUILDARCH" != "$TARGETARCH" ]; then \
        case "$TARGETARCH" in \
            amd64) packages="gcc-x86-64-linux-gnu libc6-dev-amd64-cross" ;; \
            arm64) packages="gcc-aarch64-linux-gnu libc6-dev-arm64-cross" ;; \
            *) echo "Unsupported target architecture: $TARGETARCH" >&2; exit 1 ;; \
        esac && \
        apt-get update && \
        apt-get install -y --no-install-recommends $packages && \
        rm -rf /var/lib/apt/lists/*; \
    fi

WORKDIR /build

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG TAG=nightly
ARG COMMIT=""
# Limit package parallelism per target for builders with little memory.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    if [ "$BUILDARCH" != "$TARGETARCH" ]; then \
        case "$TARGETARCH" in \
            amd64) export CC=x86_64-linux-gnu-gcc ;; \
            arm64) export CC=aarch64-linux-gnu-gcc ;; \
        esac; \
    fi && \
    CGO_ENABLED=1 GOOS="$TARGETOS" GOARCH="$TARGETARCH" GOFLAGS=-p=1 make build


FROM debian:trixie-slim

WORKDIR /app

ENV DEBIAN_FRONTEND=noninteractive

# Install runtime dependencies
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        python3 \
        python3-pip \
        ffmpeg \
        tzdata \
        curl \
        unzip \
    && rm -rf /var/lib/apt/lists/*


# Install deno for yt-dlp YouTube JS challenge solving
RUN curl -fsSL https://deno.land/install.sh | sh

ENV PATH="/root/.deno/bin:${PATH}"


# Install yt-dlp with its matching EJS challenge solver dependency
RUN python3 -m pip install --break-system-packages -U \
        "yt-dlp[default]"


# Enable yt-dlp EJS remote components
RUN mkdir -p /root/.config/yt-dlp && \
    echo "--remote-components ejs:github" \
    > /root/.config/yt-dlp/config




RUN chmod 777 /usr/local/bin


COPY --from=builder /build/bin/podsync /app/podsync


ENTRYPOINT ["/app/podsync"]

CMD ["serve", "--no-banner"]
