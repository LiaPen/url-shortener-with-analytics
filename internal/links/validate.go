package links

import (
	"fmt"
	"net/url"
	"regexp"
)

// ValidationError is bad input: the HTTP layer turns it into 400 validation,
// and Message is what the client is told.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

var codePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{4,10}$`)

// reserved are paths the service already uses at the top level. A link with
// one of these codes could never be reached, because the router sends
// GET /health to the health check, not to the redirect.
var reserved = map[string]bool{
	"health":  true,
	"version": true,
	"reset":   true,
	"links":   true,
}

func IsReserved(code string) bool { return reserved[code] }

// ValidateURL accepts only absolute http/https URLs with a host.
func ValidateURL(raw string) error {
	if raw == "" {
		return &ValidationError{"url is required"}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return &ValidationError{fmt.Sprintf("url is not a valid URL: %v", err)}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return &ValidationError{"url must start with http:// or https://"}
	}
	if u.Host == "" {
		return &ValidationError{"url must have a host, e.g. https://example.org"}
	}
	return nil
}

// ValidateCode checks the format of a code chosen by the user.
func ValidateCode(code string) error {
	if !codePattern.MatchString(code) {
		return &ValidationError{"code must be 4 to 10 characters from A-Z a-z 0-9 - _"}
	}
	return nil
}
