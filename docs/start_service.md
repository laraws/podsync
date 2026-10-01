# Podsync 服务启动说明

下列命令均从项目根目录执行。配置文件通过 `--config` / `-c` 选择，不需要覆盖现有的 `config.toml`。

| 方式 | 适用场景 | 启动命令 |
| --- | --- | --- |
| 源码运行 | 本地开发 | `go run ./cmd/podsync serve -c config.local-mysql.toml` |
| 编译后二进制 | 本地运行或服务器部署 | `make build` 后执行 `./bin/podsync serve -c config.local-mysql.toml` |
| Docker | 独立容器 | 构建本仓库镜像，再挂载配置和数据目录 |
| Docker Compose | 后台运行、自动重启 | `docker compose up -d --build` |
| systemd | Linux 开机启动 | `sudo systemctl enable --now podsync` |
| 单次更新 | 手动同步或外部定时任务 | `./bin/podsync update -c config.local-mysql.toml` |

CLI 使用 Cobra。`serve` 常驻运行，`update` 同步一次后退出，`init-db` 初始化数据库。直接执行 `podsync` 显示帮助，启动服务必须指定 `serve`。全局参数 `--config`、`--debug`、`--no-banner` 可放在子命令前或后。

```bash
./bin/podsync --help
./bin/podsync serve --help
./bin/podsync update --help
./bin/podsync init-db --help
./bin/podsync --version
```

生成 zsh 补全文件后，将它放入自己的 zsh `$fpath` 目录并初始化 `compinit`；也支持 `bash`、`fish`、`powershell`：

```bash
./bin/podsync completion zsh > /tmp/_podsync
```

## 1. 选择配置

本地环境已经准备好 `config.local-mysql.toml`：

- 文件存储：`local`，媒体、RSS 和 OPML 保存到 `./data`。
- 数据库：`175.178.51.216:3306/podsync-dev`，MySQL TLS 加密连接。
- HTTP 地址：`http://localhost:8080`，仅监听 `127.0.0.1`。
- 下载器：使用 PATH 中的 `yt-dlp`，不自动升级。
- 日志：写入 `log/YYYY-MM-DD.log`，按进程本地时区每日一个文件。
- 保留现有的 `PK1` 播放列表，查询页大小为 1，不配置自动清理。
- 显式设置 `cron_schedule = "@every 1h"`，启动时不立即同步，约一小时后首次执行。

实际配置含凭据，已加入 `.gitignore` 和 `.dockerignore`，文件权限为 `600`。其他环境可从不含凭据的模板创建：

```bash
# 仅在文件不存在时复制，避免覆盖已有凭据。
cp -n config.local-mysql.toml.example config.local-mysql.toml
chmod 600 config.local-mysql.toml
mkdir -p data log
```

填写模板的数据库密码、YouTube API Key，并准备 `cookie.txt`；不使用 cookies 时删除 `youtube_dl_args` 中的 `"--cookies", "cookie.txt"` 两项。也可以通过 `PODSYNC_YOUTUBE_API_KEY` 设置 API Key，它会覆盖 TOML 中的对应 token。

切换配置的两种方式：

```bash
./bin/podsync serve -c config.local-mysql.toml --no-banner

PODSYNC_CONFIG_PATH=config.local-mysql.toml ./bin/podsync serve --no-banner
```

显式 `--config` 优先于 `PODSYNC_CONFIG_PATH`；二者都没有时读取工作目录中的 `config.toml`。相对存储、cookies 和下载器路径以进程工作目录为准。

配置由 Viper 读取，优先级为：**显式命令行参数 > 环境变量 > TOML > 默认值**。例如 `--debug=false` 可以覆盖文件里的 `log.debug = true`。数据库、存储、HTTP、日志和下载器等静态字段支持 `PODSYNC_` 前缀环境变量，层级用下划线连接；feed 列表在 TOML 中配置。

```bash
PODSYNC_SERVER_PORT=9090 \
PODSYNC_SERVER_HOSTNAME=http://localhost:9090 \
PODSYNC_LOG_DIR=log \
./bin/podsync serve -c config.local-mysql.toml --debug=false
```

常用变量还包括 `PODSYNC_DATABASE_TYPE`、`PODSYNC_DATABASE_DSN`、`PODSYNC_STORAGE_LOCAL_DATA_DIR` 和 `PODSYNC_DOWNLOADER_CUSTOM_BINARY`。原有的 API Key 和 R2 命名变量也由 Viper 读取；R2 命名变量优先于通用层级命名。已设置为空字符串的环境变量会覆盖文件值。

修改配置后需重启服务。feed ID 保留大小写，因此 `PK1` 的数据库标识和 RSS 地址不会被转成 `pk1`。TOML 使用严格语法解析，同一表中不能重复定义相同键。

切换到 local 不会自动迁移 R2 中的历史文件。数据库中原有的下载记录仍然保留，后续同步时缺失的本地媒体可能被重新下载。`page_size = 1` 是 API 查询页大小，不保证一次同步总共只下载一个节目。

## 2. 源码运行

需要 Go 1.25 或更高版本、`yt-dlp`、`ffmpeg`。当前 Dockerfile 还准备了 Deno 和 yt-dlp EJS 支持，用于 YouTube JS challenge；本地下载遇到相关错误时也要检查这些依赖。

检查本机依赖：

```bash
go version
yt-dlp --version
ffmpeg -version
deno --version
```

启动：

```bash
mkdir -p data
go run ./cmd/podsync serve -c config.local-mysql.toml --no-banner
```

前台运行按 `Ctrl+C` 停止。需要排查问题时添加 `--debug`。

配置已启用每日文件日志，运行期间的日志写入 `log` 目录：

```toml
[log]
dir = "log"
```

目录会自动创建，同一天重启追加到当天文件；跨天后的第一条日志自动写入新文件，例如 `log/2026-10-01.log`、`log/2026-10-02.log`。文件不会自动压缩或删除。`dir` 优先于旧的 `filename` 配置，每日模式不使用 `max_size`、`max_age`、`max_backups` 和 `compress`。

```bash
tail -f "log/$(date +%F).log"
```

跨天后重新执行 `tail` 查看新文件。文件日期采用运行进程的本地时区；需要固定北京时间时可设置 `TZ=Asia/Shanghai`。配置加载之前的启动输出和错误仍会显示在终端。

## 3. 编译后二进制运行

```bash
make build
./bin/podsync serve -c config.local-mysql.toml --no-banner
```

`make build` 只构建，默认 `make` 还会运行测试。构建后执行二进制不需要 Go，但仍然需要下载器和 `ffmpeg`。

服务启动会自动初始化表结构；数据库本身需提前存在，账号需有相应建表权限。仅检查连接并初始化表结构，可以执行：

```bash
./bin/podsync init-db --config config.local-mysql.toml
```

`init-db` 会执行建表和必要的兼容迁移，并非只读连接测试；无需单独执行它才能启动服务。

## 4. Docker 运行

使用本仓库构建镜像，确保包含当前的 MySQL、R2 等实现：

```bash
docker build -t podsync:local .
```

先准备独立的容器配置：

```bash
cp -n config.local-mysql.toml config.docker.toml
chmod 600 config.docker.toml
```

将 `config.docker.toml` 的 `server.bind_address` 改为 `"*"`，`storage.local.data_dir` 改为 `"/app/data"`，cookies 路径改为 `"/app/cookie.txt"`。容器内的 `127.0.0.1` 无法通过宿主机的端口映射访问。`config.docker.toml` 也已加入 Git 和 Docker 的忽略列表。

```bash
mkdir -p data
docker run -d --name podsync --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v "$PWD/data:/app/data" \
  -v "$PWD/log:/app/log" \
  -v "$PWD/config.docker.toml:/app/config.toml:ro" \
  -v "$PWD/cookie.txt:/app/cookie.txt:ro" \
  podsync:local serve --config /app/config.toml --no-banner

docker logs -f podsync
docker stop podsync
docker start podsync
```

每日完整日志保存在宿主机 `./log`，`docker logs` 主要显示配置加载前的输出。容器默认时区可能是 UTC，需要北京时间时为 `docker run` 添加 `-e TZ=Asia/Shanghai`。

使用 MySQL 不需要挂载 SQLite 的 `db` 目录。不使用 cookies 时删除对应挂载。配置源文件和 cookie 文件必须真实存在。

## 5. Docker Compose 运行

仓库现有 `docker-compose.yml` 会构建本地代码，使用 `laraws/podsync` 镜像名，挂载 `config.toml`、`data`、`log`、`db` 和 `cookie.txt`，并设置 `restart: always`。

使用原有配置启动：

```bash
docker compose up -d --build
docker compose logs -f podsync
docker compose restart podsync
docker compose down
```

要使用新 MySQL/local 配置，先按 Docker 章节准备 `config.docker.toml`，再创建 `compose.local-mysql.yml`：

```yaml
services:
  podsync:
    build: .
    image: podsync:local
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - ./data:/app/data
      - ./log:/app/log
      - ./config.docker.toml:/app/config.toml:ro
      - ./cookie.txt:/app/cookie.txt:ro
    command: ["serve", "--config", "/app/config.toml", "--no-banner"]
```

```bash
docker compose -f compose.local-mysql.yml up -d --build
docker compose -f compose.local-mysql.yml logs -f podsync
docker compose -f compose.local-mysql.yml down
```

不要同时启动占用同一端口、访问同一数据目录的多个实例。`restart` 不会重新加载挂载路径等 Compose 配置变更；变更后重新执行 `up -d`。

## 6. Linux systemd 运行

假设项目位于 `/opt/podsync`，已编译 `bin/podsync`，并创建有目录读写权限的 `podsync` 用户。确认该用户可执行 `yt-dlp` 和 `ffmpeg`；若不在系统 PATH 中，将 `downloader.custom_binary` 设置为真实绝对路径。

创建 `/etc/systemd/system/podsync.service`：

```ini
[Unit]
Description=Podsync
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=podsync
WorkingDirectory=/opt/podsync
ExecStart=/opt/podsync/bin/podsync serve --config /opt/podsync/config.local-mysql.toml --no-banner
Restart=on-failure
RestartSec=5
Environment=PATH=/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=multi-user.target
```

配置文件权限为 `600` 时，它的所有者必须是运行服务的用户。启动和管理：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now podsync
sudo systemctl status podsync
sudo journalctl -u podsync -f
sudo systemctl restart podsync
sudo systemctl stop podsync
```

每日完整日志位于 `/opt/podsync/log`；运行用户需有该目录的写权限，`journalctl` 主要显示配置加载前的输出。

## 7. 单次更新与检查

停止常驻实例后，执行一轮实际同步并退出：

```bash
./bin/podsync update -c config.local-mysql.toml --no-banner
```

`update` 不启动 HTTP 服务，会更新所有配置的 feed，可能下载媒体并修改数据库。它会忽略常驻服务的 cron 等待时间。全部成功时退出码为 0；任一 feed 更新失败时记录错误并返回非零退出码。

常驻 local 服务检查：

```bash
curl --fail --max-time 10 http://localhost:8080/health
curl --fail --max-time 10 http://localhost:8080/index.html
# 以下文件要在成功同步后才生成：
curl --fail --max-time 10 http://localhost:8080/PK1.xml
curl --fail --max-time 10 http://localhost:8080/podsync.opml
```

`/health` 会读取数据库，并检查过去 24 小时的节目下载失败记录；健康时返回 200，数据库查询失败或有近期下载失败时返回 503。使用 S3/R2 存储时程序不启动本地 HTTP 服务，应从配置的公网存储地址访问生成文件。

## 8. MySQL TLS 与本次验证

2026-10-01 在本机真实验证了 `175.178.51.216:3306/podsync-dev`：

| 连接方式 | 结果 |
| --- | --- |
| 不使用 TLS | MySQL 错误 3159；`require_secure_transport=ON` |
| Go 驱动 `tls=true` | 服务端自动生成的证书无法通过本机 Go 的证书校验 |
| Go 驱动 `tls=skip-verify` | 连接成功；会话加密套件 `TLS_AES_128_GCM_SHA256` |
| 实际启动 Podsync | `/health` 返回 200、`healthy`；`/index.html` 返回 200；SIGTERM 后正常退出 |

本次使用真实下载器、远程数据库和 local 存储启动服务。显式 cron 阻止启动时下载，验证未执行节目同步或媒体下载；启动过程仍会执行项目已有的表结构初始化。

当前代码的 `mysqlDSN` 会保留 TLS 参数，因此这次无需修改 Go 代码。在 DSN 查询参数中添加 `tls=skip-verify` 即可运行。该模式强制加密，但不验证服务端身份；`tls=preferred` 允许回退到非加密连接，不适合这里。参见 [当前驱动版本的 TLS 说明](https://github.com/go-sql-driver/mysql/tree/v1.7.0#tls)。

如果需要完整的证书验证，应为 MySQL 配置受信任且身份匹配的证书，再使用 `tls=true`；私有 CA 场景可通过 `mysql.RegisterTLSConfig` 注册自定义 TLS 配置，目前应用没有提供专门的 CA 文件配置项。

`server.tls` 控制 Podsync 的 HTTP/HTTPS；DSN 的 `tls` 控制 MySQL，二者独立。设置 `server.hostname` 只改变 RSS 等公开链接，不会开启 HTTP TLS。
