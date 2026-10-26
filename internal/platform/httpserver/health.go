package httpserver

import (
	"context"
	"net/http"
	"time"
)

const readinessTimeout = 2 * time.Second

type Pinger interface {
	PingContext(ctx context.Context) error
}

// RegisterHealth adds the process health checks. They live here, not in the
// fleet module, because they describe the process, not the business.
//
// /healthz answers whenever the process is up. /readyz also checks that the
// database is reachable, so it fails while the service cannot do real work.
func RegisterHealth(mux *http.ServeMux, db Pinger) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeStatus(w, http.StatusOK, "ok")
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			writeStatus(w, http.StatusServiceUnavailable, "unavailable")
			return
		}

		writeStatus(w, http.StatusOK, "ok")
	})
}

func writeStatus(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
}
