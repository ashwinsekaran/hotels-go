// Package auth provides bearer-token middleware that wraps an http.Handler.
// It is applied ONCE at the wiring layer over the whole API router, so every
// business route is protected in a single place instead of each handler
// repeating the check. This file is resource- and module-agnostic.
package auth

import "net/http"

// Auth wraps next and admits a request only when its Authorization header equals
// "Bearer <token>"; otherwise it short-circuits with 401 Unauthorized and next
// is never called. The token comes from config (env) in main — do not hard-code
// a secret here. Mount infra endpoints (/metrics, /.well-known/*) OUTSIDE this
// wrapper so probes and scrapes stay unauthenticated.
func Auth(token string, next http.Handler) http.Handler {
	expected := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
