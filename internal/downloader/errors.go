package downloader

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrTooManyRequests = errors.New("too many requests")
	ErrCookiesInvalid  = errors.New("YouTube cookies 已失效，请重新导出 cookie 文件并重启 Podsync")
)

type FailureKind string

const (
	FailureCookiesExpired FailureKind = "cookies_expired"
	FailureCookiesFormat  FailureKind = "cookies_format"
	FailureBotCheck       FailureKind = "bot_check"
	FailureRateLimit      FailureKind = "rate_limit"
	FailureLogin          FailureKind = "login_required"
	FailurePrivate        FailureKind = "private_video"
	FailureMembersOnly    FailureKind = "members_only"
	FailureAgeRestricted  FailureKind = "age_restricted"
	FailureGeoRestricted  FailureKind = "geo_restricted"
	FailureUnavailable    FailureKind = "unavailable"
	FailureFormat         FailureKind = "format_unavailable"
	FailureJavaScript     FailureKind = "javascript_challenge"
	FailureTimeout        FailureKind = "timeout"
	FailureNetwork        FailureKind = "network"
	FailureForbidden      FailureKind = "http_forbidden"
	FailureDependency     FailureKind = "dependency"
	FailurePostprocess    FailureKind = "postprocessing"
	FailureDisk           FailureKind = "disk"
	FailureArguments      FailureKind = "arguments"
	FailureUnsupportedURL FailureKind = "unsupported_url"
	FailureMetadata       FailureKind = "metadata"
	FailureUnknown        FailureKind = "unknown"
)

// Failure keeps human guidance separate from original subprocess diagnostics.
// Output retains stdout/stderr (with credentials redacted); Cause remains in
// the error chain so callers can still detect timeouts and process exit errors.
type Failure struct {
	Kind       FailureKind
	Reason     string
	Suggestion string
	StopFeed   bool
	Output     string
	Cause      error
}

func (e *Failure) UserMessage() string {
	return e.Reason + "\n处理建议：" + e.Suggestion
}

func (e *Failure) OriginalError() string {
	output := strings.TrimSpace(e.Output)
	if e.Cause != nil {
		if output != "" {
			output += "\n"
		}
		output += e.Cause.Error()
	}
	return output
}

// Error is used by logs and the database; it retains the full diagnostic output.
func (e *Failure) Error() string {
	return e.UserMessage() + "\n原始错误：" + e.OriginalError()
}

func (e *Failure) Unwrap() error { return e.Cause }
func (e *Failure) Is(target error) bool {
	return target == ErrCookiesInvalid && e.Kind == FailureCookiesExpired ||
		target == ErrTooManyRequests && e.Kind == FailureRateLimit
}

// ShouldStopFeed prevents repeated attempts for problems shared by the feed.
// Content-specific failures leave other episodes eligible for download.
func ShouldStopFeed(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if ShouldStopFeed(cause) {
				return true
			}
		}
		return false
	}
	var failure *Failure
	return errors.Is(err, ErrCookiesInvalid) || errors.Is(err, ErrTooManyRequests) ||
		errors.As(err, &failure) && failure.StopFeed
}

func newFailure(kind FailureKind, reason, suggestion string, stop bool, output string, cause error) *Failure {
	return &Failure{Kind: kind, Reason: reason, Suggestion: suggestion, StopFeed: stop, Output: output, Cause: cause}
}

func downloadError(output string, cause error) error {
	if errors.Is(cause, context.Canceled) {
		return cause
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return newFailure(FailureTimeout, "yt-dlp 请求或下载超时", "检查容器网络并稍后重试；下载阶段持续超时时可适当增加 downloader.timeout", true, output, cause)
	}
	text := strings.ToLower(output)
	if strings.Contains(text, "youtube account cookies are no longer valid") {
		return newFailure(FailureCookiesExpired, "YouTube cookie 已失效或被轮换", "重新导出 cookie.txt，然后重启 Podsync 容器", true, output, cause)
	}
	// Normal stdout can contain episode titles and filenames, not error causes.
	var signals []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "error:") || strings.Contains(line, "challenge solving failed") || strings.Contains(line, "signature extraction failed") || strings.Contains(line, "nsig extraction failed") || strings.Contains(line, "no supported javascript runtime") {
			signals = append(signals, line)
		}
	}
	if len(signals) > 0 {
		text = strings.Join(signals, "\n")
	}
	has := func(patterns ...string) bool {
		for _, pattern := range patterns {
			if strings.Contains(text, pattern) {
				return true
			}
		}
		return false
	}
	kind, reason, suggestion, stop := FailureUnknown, "yt-dlp 执行失败，暂未识别具体原因", "查看原始错误；检查来源是否可访问，并更新 yt-dlp 后重试", false
	switch {
	case has("does not look like a netscape format cookies file", "invalid netscape format cookies file", "invalid cookies file", "failed to load cookies", "failed to decrypt cookies"):
		kind, reason, suggestion, stop = FailureCookiesFormat, "cookie 文件格式错误或无法读取", "重新导出 Netscape 格式的 cookie.txt，并检查文件路径、挂载和读取权限", true
	case has("http error 429", "429 too many requests", "too many requests", "this content isn't available, try again later"):
		kind, reason, suggestion, stop = FailureRateLimit, "来源平台限制了请求频率", "稍后重试，并降低更新频率或设置 yt-dlp 下载间隔", true
	case has("confirm you’re not a bot", "confirm you're not a bot", "confirm you are not a bot", "captcha"):
		kind, reason, suggestion, stop = FailureBotCheck, "YouTube 要求验证登录或触发了反爬检查，不能仅凭此判定 cookie 过期", "确认导出会话有效、账号可正常访问，并检查请求频率和容器出口网络后再重试", true
	case has("private video", "private playlist", "this video is private"):
		kind, reason, suggestion = FailurePrivate, "视频或播放列表为私有内容", "确认 cookie 对应账号有访问权限；无权限时跳过该内容"
	case has("members-only", "members only", "join this channel", "requires payment", "purchase this video"):
		kind, reason, suggestion = FailureMembersOnly, "内容需要频道会员或购买权限", "确认 cookie 对应账号已获得所需权限；无权限时跳过该内容"
	case has("confirm your age", "age-restricted", "age restricted", "inappropriate for some users"):
		kind, reason, suggestion = FailureAgeRestricted, "内容有年龄限制，当前会话无法访问", "使用具备访问权限的账号导出 cookie，并重启 Podsync 后重试"
	case has("not available in your country", "not available from your location", "geo-restricted", "geo restricted", "blocked in your country"):
		kind, reason, suggestion = FailureGeoRestricted, "内容在当前地区不可访问", "检查来源的地区限制；无法合法访问时跳过该内容"
	case has("login required", "sign in to", "authentication required", "log in to"):
		kind, reason, suggestion = FailureLogin, "内容需要登录，当前会话未获得访问权限", "确认账号可以访问该内容，配置有效 cookie 并重启 Podsync"
	case has("challenge solving failed", "signature extraction failed", "nsig extraction failed", "no supported javascript runtime", "unable to extract initial player response"):
		kind, reason, suggestion, stop = FailureJavaScript, "YouTube 播放器解析或 JavaScript 验证失败", "更新 yt-dlp 及匹配的 EJS 组件，并检查 Deno 是否可用；Docker 部署可重新构建镜像", true
	case has("requested format is not available", "requested format not available", "no video formats found", "no formats found"):
		kind, reason, suggestion = FailureFormat, "当前内容没有可用的请求音频或视频格式", "检查 format、quality 和 youtube_dl_args 的格式选择，并更新 yt-dlp 后重试"
	case has("timed out", "timeout", "read timeout"):
		kind, reason, suggestion, stop = FailureTimeout, "yt-dlp 网络请求超时", "检查容器网络和代理，稍后重试；必要时调整下载超时", true
	case has("http error 403", "403 forbidden"):
		kind, reason, suggestion = FailureForbidden, "来源服务器拒绝访问（HTTP 403），具体原因尚不确定", "检查内容权限、容器出口网络和 yt-dlp 版本；此错误不等同于 cookie 过期"
	case has("video unavailable", "video is unavailable", "video has been removed", "video does not exist", "not available due to a copyright claim", "removed for violating", "http error 404"):
		kind, reason, suggestion = FailureUnavailable, "视频已删除、被限制或暂不可用", "在来源页面确认状态；内容已不可访问时跳过该节目"
	case has("name or service not known", "temporary failure in name resolution", "nodename nor servname", "getaddrinfo failed", "connection refused", "network is unreachable", "connection reset", "certificate_verify_failed", "unable to download webpage", "unable to download api page"):
		kind, reason, suggestion, stop = FailureNetwork, "容器无法正常连接来源平台", "检查 DNS、网络、代理和证书配置，恢复连接后重试", true
	case has("ffmpeg not found", "ffmpeg and ffprobe not found", "ffprobe not found"):
		kind, reason, suggestion, stop = FailureDependency, "缺少音频转换所需的 ffmpeg 或 ffprobe", "安装依赖，或重新构建包含这些依赖的 Docker 镜像", true
	case has("postprocessing:", "conversion failed", "error while opening encoder"):
		kind, reason, suggestion = FailurePostprocess, "音频转换或后处理失败", "检查 ffmpeg 输出和原始媒体文件，确认转换参数有效后重试"
	case has("no space left on device", "disk full", "permission denied", "read-only file system"):
		kind, reason, suggestion, stop = FailureDisk, "下载临时目录空间不足或无法写入", "检查容器临时目录的磁盘空间、挂载和写入权限", true
	case has("no such option:", "unrecognized arguments:", "requires 1 argument", "option requires an argument"):
		kind, reason, suggestion, stop = FailureArguments, "yt-dlp 参数无效或与当前版本不匹配", "检查 youtube_dl_args，并对照当前 yt-dlp 版本修正参数", true
	case has("unsupported url", "no suitable extractor"):
		kind, reason, suggestion = FailureUnsupportedURL, "yt-dlp 不支持当前来源地址", "确认地址正确，并检查当前 yt-dlp 是否支持该来源"
	}
	return newFailure(kind, reason, suggestion, stop, output, cause)
}

func (dl *YTDLP) failure(output string, cause error) error {
	return dl.reportFailure(downloadError(output, cause))
}

func (dl *YTDLP) reportFailure(err error) error {
	var failure *Failure
	if errors.As(err, &failure) {
		failure.Output = dl.redactOutput(failure.Output)
	}
	return err
}
