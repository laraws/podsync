FROM golang:1.25 AS builder

ENV TAG="nightly"
ENV COMMIT=""

WORKDIR /build

COPY . .

RUN make build

FROM ubuntu:24.04

WORKDIR /app

ENV DEBIAN_FRONTEND=noninteractive

# deno is required for yt-dlp (ref: https://github.com/yt-dlp/yt-dlp/issues/14404)
RUN apt-get update
RUN apt-get install -y --no-install-recommends ca-certificates python3 python3-pip ffmpeg tzdata curl
RUN python3 -m pip install --break-system-packages -U --pre "yt-dlp[default]"
RUN ln -sf /usr/local/bin/yt-dlp /usr/local/bin/youtube-dl
RUN rm -rf /var/lib/apt/lists/*

RUN chmod 777 /usr/local/bin
COPY --from=builder /build/bin/podsync /app/podsync
COPY --from=builder /build/html/index.html /app/html/index.html

ENTRYPOINT ["/app/podsync"]
CMD ["--no-banner"]
