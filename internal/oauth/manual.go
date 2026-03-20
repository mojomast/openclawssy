package oauth

import (
	"errors"
	"net/url"
	"strings"
)

// ParseManualInput extracts an authorization code from user-pasted input.
// It accepts either:
//   - A bare authorization code string (no "://" present), or
//   - A full callback URL containing a "code" query parameter.
//
// Returns the code and state (state may be empty), or an error if the input
// is empty or a URL is present but contains no "code" parameter.
func ParseManualInput(input string) (code string, state string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errors.New("oauth/manual: input is empty")
	}

	// If it looks like a URL, parse it and extract the code param.
	if strings.Contains(input, "://") {
		u, parseErr := url.Parse(input)
		if parseErr != nil {
			return "", "", errors.New("oauth/manual: invalid URL")
		}
		q := u.Query()
		code = q.Get("code")
		if code == "" {
			return "", "", errors.New("oauth/manual: URL contains no 'code' parameter")
		}
		state = q.Get("state")
		return code, state, nil
	}

	// Bare code string.
	return input, "", nil
}
