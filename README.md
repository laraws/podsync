# Podsync

![Podsync](docs/img/logo.png)

[![](https://github.com/mxpv/podsync/workflows/CI/badge.svg)](https://github.com/mxpv/podsync/actions?query=workflow%3ACI)
[![Nightly](https://github.com/mxpv/podsync/actions/workflows/nightly.yml/badge.svg)](https://github.com/mxpv/podsync/actions/workflows/nightly.yml)
[![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/mxpv/podsync)](https://github.com/mxpv/podsync/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/mxpv/podsync)](https://goreportcard.com/report/github.com/mxpv/podsync)
[![GitHub Sponsors](https://img.shields.io/github/sponsors/mxpv)](https://github.com/sponsors/mxpv)
[![Patreon](https://img.shields.io/badge/support-patreon-E6461A.svg)](https://www.patreon.com/podsync)

Podsync - is a simple, free service that lets you listen to any YouTube / Vimeo channels, playlists or user videos in
podcast format.

Podcast applications have a rich functionality for content delivery - automatic download of new episodes,
remembering last played position, sync between devices and offline listening. This functionality is not available
on YouTube and Vimeo. So the aim of Podsync is to make your life easier and enable you to view/listen to content on
any device in podcast client.

## ✨ Features

- Works with YouTube and Vimeo.
- Supports feeds configuration: video/audio, high/low quality, max video height, etc.
- mp3 encoding
- Update scheduler supports cron expressions
- Episodes filtering (match by title, duration).
- Feeds customizations (custom artwork, category, language, etc).
- OPML export.
- Supports episodes cleanup (keep last X episodes).
- Configurable hooks for custom integrations and workflows.
- One-click deployment for AWS.
- Runs on Windows, Mac OS, Linux, and Docker.
- Supports ARM.
- Optional yt-dlp self update every 24 hours while serving (custom binaries are excluded).
- Supports API keys rotation.

## 📋 Dependencies

If you're running the CLI as binary (e.g. not via Docker), you need to make sure that dependencies are available on
your system. Currently, Podsync depends on `yt-dlp` ,  `ffmpeg`, and `go`.

On Mac you can install those with `brew`:
```
brew install yt-dlp ffmpeg go
```

## 📖 Documentation

- [服务启动说明（中文）：源码、二进制、Docker、Compose、systemd](./docs/start_service.md)
- [How to get Vimeo API token](./docs/how_to_get_vimeo_token.md)
- [How to get YouTube API Key](./docs/how_to_get_youtube_api_key.md)
- [Podsync on QNAP NAS Guide](./docs/how_to_setup_podsync_on_qnap_nas.md)
- [Schedule updates with cron](./docs/cron.md)

## 🌙 Nightly builds

Nightly builds uploaded every midnight from the `main` branch and available for testing:

```bash
$ docker run -it --rm ghcr.io/mxpv/podsync:nightly
```

### 🔑 Access tokens

In order to query YouTube or Vimeo API you have to obtain an API token first.

- [How to get YouTube API key](https://elfsight.com/blog/2016/12/how-to-get-youtube-api-key-tutorial/)
- [Generate an access token for Vimeo](https://developer.vimeo.com/api/guides/start#generate-access-token)

## Project structure

```text
main.go              Process entry point, signals and exit code
cmd/                 Cobra commands, flags and configuration resolution
internal/app/        Dependency composition, one-shot updates and service lifecycle
internal/scheduler/  Cron scheduling, per-feed deduplication and cancellation
internal/update/     Metadata sync, downloads, retention and publication
internal/source/     YouTube/Vimeo/SoundCloud/Twitch adapters and API credentials
internal/downloader/ yt-dlp subprocess adapter
internal/feed/       Pure RSS/OPML rendering
internal/storage/    Atomic local writes and S3/R2 object storage
internal/db/         SQL metadata and download-state persistence
internal/model/      Domain metadata and episode state
internal/web/        HTTP file serving, embedded Web UI and health query
internal/hooks/      Cancellable post-download commands
internal/config/     YAML loading, defaults and startup validation
internal/logging/    Console output or daily log files
internal/notify/     Telegram episode notifications
internal/buildinfo/  Build metadata
```

The entry point is the repository root: `go run . serve -c config.local-mysql.yaml`. CLI adapters pass resolved configuration to the application; application-specific setup stays under `internal/`.

## Update flow and database

An update fetches source metadata, synchronizes it in one transaction, downloads
eligible episodes newest first, applies retention, then publishes RSS and OPML.
`page_size` caps fetched episodes and downloads per update; it does not cap the
number retained over multiple updates. Download failures return a nonzero exit
code while successful episodes are still published. Scheduled updates are serial;
repeated triggers for a queued or running feed are coalesced.

The database contains `feeds` (source metadata and creation/update timestamps)
and `episodes` (metadata, numeric playlist order, download status, object key,
size, attempt count, last attempt time, last error and download completion time).
Metadata refreshes preserve download state. Foreign keys cascade feed deletion;
indexes support feed iteration and the recent-failure health query. `/health`
counts currently failed episodes by their last attempt time, independent of their
publication date. Fresh SQLite/MySQL schemas are initialized on opening the database;
there are no old-schema migrations or object-key backfills.

Local writes publish files through a temporary file and rename. The Web UI is
embedded in the binary. For S3/R2, `public_url` is the public directory URL before
the storage `prefix`; RSS, OPML and hook URLs use the same object-key rules.
Post-download hooks receive `EPISODE_FILE` (absolute path for local storage,
empty for cloud), `EPISODE_KEY`, `EPISODE_URL`, `FEED_NAME`, and `EPISODE_TITLE`.
`log.dir` enables daily log files; leaving it empty selects console output.

Platform integration tests require explicit credentials. Normal unit tests use
local HTTP fixtures or injected adapters; `go test -race ./...` covers the queue,
update pipeline and storage recovery without downloading media or sending Telegram messages.

## ⚙️ Configuration

You need to create a configuration file (for instance `config.yaml`) and specify the list of feeds that you're going to host.
See [config.yaml.example](./config.yaml.example) for all possible configuration keys available in Podsync.

Configuration is loaded through Viper. Precedence is explicit CLI flags, environment variables, YAML values, then defaults. Feed identifiers retain their original case. Only `.yaml` and `.yml` files are accepted; unknown fields, duplicate keys and multiple documents are rejected. Audio is the default format; select video or custom explicitly when needed.

Minimal configuration would look like this:

```yaml
server:
  port: 8080
storage:
  local:
    data_dir: "/app/data/"
tokens:
  youtube: "PASTE YOUR API KEY HERE"
feeds:
  ID1:
    url: "https://www.youtube.com/channel/UCxC5Ls6DwqV0e-CYcAKkExQ"
```

If you want to hide Podsync behind reverse proxy like nginx, you can use `hostname` field:

```yaml
server:
  port: 8080
  hostname: "https://my.test.host:4443"
```

Server will be accessible from `http://localhost:8080`, but episode links will point to `https://my.test.host:4443/ID1/...`

### Cloudflare R2 storage

R2 stores episodes, generated RSS XML, and OPML outside the local data directory. `public_url` must be an enabled R2 custom domain (recommended) or `r2.dev` URL; it is intentionally separate from the authenticated S3 API endpoint.

```yaml
storage:
  type: "r2"
  r2:
    endpoint_url: "https://ACCOUNT_ID.r2.cloudflarestorage.com"
    bucket: "podcasts"
    public_url: "https://media.example.com"
    prefix: ""
```

Set the credentials with `PODSYNC_R2_ACCESS_KEY_ID` and `PODSYNC_R2_SECRET_ACCESS_KEY`. Downloaded episode rows persist only the object key; RSS enclosure URLs are assembled from `public_url` at generation time.

### Telegram episode notifications

Use a Telegram bot to receive one MarkdownV2 message per episode download attempt. Messages include completion time with timezone, feed and episode titles/IDs, elapsed time, source URL, and either file size or a failure reason. Titles use the feed's custom title when configured. Existing files and filtered/skipped episodes do not generate notifications.

```yaml
telegram:
  enabled: true
  bot_token: "REPLACE_WITH_TELEGRAM_BOT_TOKEN"
  user_id: 123456789
  timeout: "10s"
```

Start a private chat with the bot and send `/start` before enabling notifications. `user_id` is used as Telegram's `chat_id`. Prefer `PODSYNC_TELEGRAM_BOT_TOKEN` to keep credentials out of shared configuration. All four fields accept their corresponding `PODSYNC_TELEGRAM_*` environment variables.

Notifications use the [go-telegram/bot SDK](https://github.com/go-telegram/bot) without polling or webhooks. Markdown characters are escaped and long fields are truncated within Telegram's message limit. Telegram 429 responses retry up to three attempts within the configured total timeout; other send errors are logged without changing download status or stopping subsequent downloads. The timeout defaults to 10 seconds, including during shutdown. Network access uses Go's standard `HTTPS_PROXY`/`HTTP_PROXY`/`NO_PROXY` settings.

### 🌍 Environment Variables

Podsync supports the following environment variables for configuration and API keys:

Static configuration fields also support `PODSYNC_` environment variables with dots replaced by underscores, such as `PODSYNC_SERVER_PORT`, `PODSYNC_DATABASE_DSN`, and `PODSYNC_LOG_DIR`. Dynamic feed entries are configured in YAML. API keys and R2 fields use the documented names below; alternate names are not supported. Empty environment values override file values.

| Variable Name                | Description                                                                               | Example Value(s)                              |
|------------------------------|-------------------------------------------------------------------------------------------|-----------------------------------------------|
| `PODSYNC_CONFIG_PATH`        | Default configuration file path when `--config` is not specified                         | `/app/config.yaml`                            |
| `PODSYNC_DATABASE_TYPE`      | Database driver                                                                        | `sqlite` or `mysql`                           |
| `PODSYNC_DATABASE_DSN`       | Database connection string                                                             | `user:password@tcp(host:3306)/podsync?tls=true` |
| `PODSYNC_SERVER_PORT`        | HTTP server port                                                                       | `8080`                                       |
| `PODSYNC_LOG_DIR`            | Directory for daily YYYY-MM-DD.log files                                                | `log`                                        |
| `PODSYNC_LOG_DEBUG`          | Debug logging; overridden by explicit `--debug` or `--debug=false`                       | `true`                                       |
| `PODSYNC_NO_BANNER`          | Hide the startup banner                                                                | `true`                                       |
| `PODSYNC_YOUTUBE_API_KEY`    | YouTube API key(s), space-separated for rotation                                          | `key1` or `key1 key2 key3` |
| `PODSYNC_VIMEO_API_KEY`      | Vimeo API key(s), space-separated for rotation                                            | `key1` or `key1 key2`        |
| `PODSYNC_SOUNDCLOUD_API_KEY` | SoundCloud API key(s), space-separated for rotation                                       | `soundcloud_key1 soundcloud_key2`             |
| `PODSYNC_TWITCH_API_KEY`     | Twitch API credentials in the format `CLIENT_ID:CLIENT_SECRET`, space-separated for multi | `id1:secret1 id2:secret2`                     |
| `PODSYNC_R2_ENDPOINT_URL`     | Cloudflare R2 S3 API endpoint                                                          | `https://ACCOUNT_ID.r2.cloudflarestorage.com` |
| `PODSYNC_R2_ACCESS_KEY_ID`    | R2 S3 access key ID                                                                   | `...`                                         |
| `PODSYNC_R2_SECRET_ACCESS_KEY`| R2 S3 secret access key                                                               | `...`                                         |
| `PODSYNC_R2_BUCKET`           | R2 bucket name                                                                        | `podcasts`                                    |
| `PODSYNC_R2_PUBLIC_URL`       | Enabled R2 public custom domain or development URL                                     | `https://media.example.com`                   |

## 🚀 How to run

The CLI uses Cobra. Run `podsync serve` for the scheduled service, `podsync update` for a single update, or `podsync init-db` to initialize database tables. Use `--help`, `--version`, or `completion zsh` for help, version information, and shell completion. `--config` (`-c`), `--debug`, and `--no-banner` work before or after a subcommand.

Running `podsync` without a subcommand shows help. Single updates return a nonzero exit code if any feed fails.

### Build and run as binary:

Make sure you have created the file `config.yaml`. Also note the location of the `data_dir`. Depending on the operating system, you may have to choose a different location since `/app/data` might be not writable.

```
$ git clone https://github.com/mxpv/podsync
$ cd podsync
$ make
$ ./bin/podsync serve --config config.yaml
```

### 🐛 How to debug

Use the editor [Visual Studio Code](https://code.visualstudio.com/) and install the official [Go](https://marketplace.visualstudio.com/items?itemName=golang.go) extension. Afterwards you can execute "Run & Debug" ▶︎ "Debug Podsync" to debug the application. The required configuration is already prepared (see `.vscode/launch.json`).


### 🐳 Run via Docker:

```
$ docker pull ghcr.io/mxpv/podsync:latest
$ docker run \
    -p 8080:8080 \
    -v $(pwd)/data:/app/data/ \
    -v $(pwd)/db:/app/db/ \
    -v $(pwd)/config.yaml:/app/config.yaml \
    ghcr.io/mxpv/podsync:latest
```

### 🐳 Run via Docker Compose:

```
$ cat docker-compose.yml
services:
  podsync:
    image: ghcr.io/mxpv/podsync
    container_name: podsync
    volumes:
      - ./data:/app/data/
      - ./db:/app/db/
      - ./config.yaml:/app/config.yaml
    ports:
      - 8080:8080

$ docker compose up
```

## 📦 How to make a release

Just push a git tag. CI will do the rest.

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
