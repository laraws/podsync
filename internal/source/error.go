package source

import (
	"net/url"
	"strings"
)

// SDK transport errors may include credentials from the request URL.
// Preserve the underlying error for cancellation and provider error checks.
type credentialError struct {
	err        error
	credential string
}

func (e *credentialError) Error() string {
	message := strings.ReplaceAll(e.err.Error(), url.QueryEscape(e.credential), "[redacted]")
	return strings.ReplaceAll(message, e.credential, "[redacted]")
}

func (e *credentialError) Unwrap() error { return e.err }

func redactCredential(err error, credential string) error {
	if err == nil || credential == "" {
		return err
	}
	return &credentialError{err: err, credential: credential}
}
