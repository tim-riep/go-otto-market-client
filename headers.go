package ottomarket

import (
	"net/http"
	"time"
)

// requestTimestampLayout is ISO 8601 in UTC with milliseconds, e.g.
// 2026-11-01T22:08:15.123Z.
const requestTimestampLayout = "2006-01-02T15:04:05.000Z07:00"

// setCommonHeaders sets the headers OTTO expects from every client:
//
//   - X-Request-Timestamp: the time the request is sent (ISO 8601).
//   - Accept: the media types the operation responds with, unless the
//     caller set Accept already.
//
// Content-Type is set by the generated code for requests with a body.
func setCommonHeaders(r *http.Request, now time.Time) {
	r.Header.Set("X-Request-Timestamp", now.UTC().Format(requestTimestampLayout))
	if r.Header.Get("Accept") == "" {
		r.Header.Set("Accept", acceptFor(r.Method, r.URL.Path))
	}
}

// acceptFor looks up the Accept header for a request in the generated
// acceptRoutes table and falls back to JSON.
func acceptFor(method, path string) string {
	for _, rt := range acceptRoutes {
		if rt.method == method && rt.pattern.MatchString(path) {
			return rt.accept
		}
	}
	return "application/json"
}
