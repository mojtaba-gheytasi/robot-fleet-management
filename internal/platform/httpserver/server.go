package httpserver

import (
	"context"
	"net/http"
	"time"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second

	// Shorter than writeTimeout, so a request that runs out of time can still be answered.
	requestTimeout = 5 * time.Second
)

func New(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           withDeadline(handler, requestTimeout),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// withDeadline cancels the request's context after timeout. Database work uses that
// context, so a request waiting for a lock stops, rolls back and frees its connection.
func withDeadline(handler http.Handler, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
