package notify

import (
	"errors"
	"strings"
)

// Notification formatting depends on a display contract, not an extractor.
type detailedError interface {
	UserMessage() string
	OriginalError() string
}

type displayError struct{ message, original string }

func (e displayError) Error() string         { return e.message }
func (e displayError) UserMessage() string   { return e.message }
func (e displayError) OriginalError() string { return e.original }

func errorText(err error) (message, original string) {
	if err == nil {
		return "", ""
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var messages, originals []string
		for _, cause := range joined.Unwrap() {
			m, o := errorText(cause)
			if m != "" {
				messages = append(messages, m)
			}
			if o != "" {
				originals = append(originals, o)
			}
		}
		return strings.Join(messages, "\n"), strings.Join(originals, "\n")
	}
	var detailed detailedError
	if errors.As(err, &detailed) {
		return detailed.UserMessage(), detailed.OriginalError()
	}
	return err.Error(), ""
}

// Prefer the terminal error to download progress or verbose traceback prefixes.
// Full diagnostics remain in the application's logs and database.
func errorExcerpt(original string) string {
	lines := strings.Split(strings.TrimSpace(original), "\n")
	var diagnostics []string
	for _, line := range lines {
		normalized := strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(normalized, "error:") || strings.HasPrefix(normalized, "warning:") {
			diagnostics = append(diagnostics, line)
		}
	}
	if len(diagnostics) > 0 {
		lines = diagnostics
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, "\n")
}
