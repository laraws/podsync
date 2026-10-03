package downloader

import (
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
)

var (
	diagnosticURL       = regexp.MustCompile(`https?://[^\s"'<>]+`)
	sensitiveKey        = regexp.MustCompile(`(?i)token|secret|password|signature|cookie|authorization|api.?key|^sig$|^key$`)
	sensitiveAssignment = regexp.MustCompile(`(?i)(\b(?:[\w-]*token|(?:api|access|secret)[_-]?key|password|secret)\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;&?#]+)`)
	sensitiveHeader     = regexp.MustCompile(`(?im)\b(authorization|cookie|set-cookie)\s*[:=][^\r\n]*`)
)

func cookieValues(data string) []string {
	var values []string
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) == 7 && len(fields[6]) > 5 {
			values = append(values, fields[6])
		}
	}
	return values
}

// Exported and refreshed jar values may appear in malformed-cookie diagnostics.
// This runs with updateLock held and never emits cookie file contents.
func (dl *YTDLP) redactOutput(output string) string {
	secrets := append([]string(nil), dl.cookieSecrets...)
	for _, path := range dl.cookieFiles {
		if data, err := os.ReadFile(path); err == nil {
			secrets = append(secrets, cookieValues(string(data))...)
		}
	}
	return redactDiagnostic(output, secrets)
}

func redactDiagnostic(output string, secrets []string) string {
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, value := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			output = strings.ReplaceAll(output, value, "[redacted]")
		}
	}
	output = diagnosticURL.ReplaceAllStringFunc(output, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "[redacted URL]"
		}
		if u.User != nil {
			u.User = url.User("[redacted]")
		}
		query := u.Query()
		for key := range query {
			if sensitiveKey.MatchString(key) {
				query.Set(key, "[redacted]")
			}
		}
		u.RawQuery = query.Encode()
		return u.String()
	})
	output = sensitiveAssignment.ReplaceAllString(output, "${1}[redacted]")
	return sensitiveHeader.ReplaceAllString(output, "$1: [redacted]")
}
