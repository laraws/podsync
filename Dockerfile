FROM golang:1.25-bookworm AS builder

ENV TAG="nightly"
ENV COMMIT=""

WORKDIR /build

COPY . .

RUN make build


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
    && rm -rf /var/lib/apt/lists/*


# Install deno for yt-dlp YouTube JS challenge solving
RUN curl -fsSL https://deno.land/install.sh | sh

ENV PATH="/root/.deno/bin:${PATH}"


# Install stable yt-dlp and EJS challenge solver support
RUN python3 -m pip install --break-system-packages -U \
        yt-dlp \
        yt-dlp-ejs


# Enable yt-dlp EJS remote components
RUN mkdir -p /root/.config/yt-dlp && \
    echo "--remote-components ejs:github" \
    > /root/.config/yt-dlp/config


# yt-dlp compatibility alias
RUN ln -sf /usr/local/bin/yt-dlp /usr/local/bin/youtube-dl


RUN chmod 777 /usr/local/bin


COPY --from=builder /build/bin/podsync /app/podsync
COPY --from=builder /build/html/index.html /app/html/index.html


ENTRYPOINT ["/app/podsync"]

CMD ["--no-banner"]