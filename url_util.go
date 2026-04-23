package main

import "strings"

// ensureHTTPScheme prepends "https://" to s unless it already begins with a
// case-insensitive "http://" or "https://" scheme. A prefix check like "http"
// would accept bogus inputs such as "httpexample.com".
func ensureHTTPScheme(s string) string {
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return s
	}
	return "https://" + s
}
