package update

import (
	"net/url"
	"regexp"
	"strings"
)

// Match URL tokens in diagnostic prose, without consuming quoting delimiters.
var diagnosticURL = regexp.MustCompile(`(?i)(?:https?|ssh|git|file)://[^\s<>"'` + "`" + `]+`)

// RedactSourceText removes URL credentials and query/fragment data from output.
// source supplies secrets that Git may echo separately from the original URL.
// It leaves the original source untouched and returns safe diagnostic text.
func RedactSourceText(text, source string) string {
	secrets := []string{}
	if parsed, err := url.Parse(source); err == nil {
		if parsed.User != nil {
			secrets = append(secrets, parsed.User.Username())
			if password, ok := parsed.User.Password(); ok {
				secrets = append(secrets, password)
			}
		}
		for _, values := range parsed.Query() {
			secrets = append(secrets, values...)
		}
		if parsed.Fragment != "" {
			secrets = append(secrets, parsed.Fragment)
		}
	}
	text = diagnosticURL.ReplaceAllStringFunc(text, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "[redacted-url]"
		}
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.ForceQuery = false
		parsed.Fragment = ""
		parsed.RawFragment = ""
		return parsed.String()
	})
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			text = strings.ReplaceAll(text, form, "[redacted]")
		}
	}
	return text
}
