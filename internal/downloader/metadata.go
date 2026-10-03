package downloader

import "strings"

// Playlist extraction needs the same session and request settings as downloads,
// but must not inherit output, conversion or episode filtering options.
func metadataArgs(args []string) []string {
	var result []string
	for i := 0; i < len(args); i++ {
		name, _, equals := strings.Cut(args[i], "=")
		switch name {
		case "--cookies", "--cookies-from-browser", "--proxy", "--user-agent",
			"--referer", "--add-headers", "--extractor-args", "--js-runtimes",
			"--remote-components", "--sleep-requests", "--socket-timeout",
			"--source-address", "--impersonate":
			result = append(result, args[i])
			if !equals && i+1 < len(args) {
				i++
				result = append(result, args[i])
			}
		case "--no-cookies", "--no-cookies-from-browser", "--no-check-certificates",
			"--force-ipv4", "--force-ipv6", "--no-js-runtimes", "--no-remote-components":
			result = append(result, args[i])
		}
	}
	return result
}
