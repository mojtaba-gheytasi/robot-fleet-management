package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServerGivesEveryRequestADeadline(t *testing.T) {
	var deadline time.Time
	var hasDeadline bool
	server := New(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		deadline, hasDeadline = r.Context().Deadline()
	}))

	started := time.Now()
	server.Handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/robots", nil))
	finished := time.Now()

	if hasDeadline == false {
		t.Fatal("the request context has no deadline")
	}
	if deadline.Before(started.Add(requestTimeout)) || deadline.After(finished.Add(requestTimeout)) {
		t.Fatalf("deadline %s is not %s after the request started", deadline, requestTimeout)
	}
	if requestTimeout >= writeTimeout {
		t.Fatalf("requestTimeout %s must be shorter than writeTimeout %s", requestTimeout, writeTimeout)
	}
}
