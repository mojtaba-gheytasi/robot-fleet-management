package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakePinger struct{ err error }

func (p fakePinger) PingContext(context.Context) error { return p.err }

func TestHealth(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		pingErr  error
		wantCode int
		wantBody string
	}{
		{"liveness with database up", "/healthz", nil, http.StatusOK, `{"status":"ok"}`},
		{"liveness ignores the database", "/healthz", errors.New("down"), http.StatusOK, `{"status":"ok"}`},
		{"readiness with database up", "/readyz", nil, http.StatusOK, `{"status":"ok"}`},
		{"readiness with database down", "/readyz", errors.New("down"), http.StatusServiceUnavailable, `{"status":"unavailable"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			RegisterHealth(mux, fakePinger{err: tt.pingErr})

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if rec.Body.String() != tt.wantBody {
				t.Fatalf("body = %s, want %s", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestHealthRejectsOtherMethods(t *testing.T) {
	mux := http.NewServeMux()
	RegisterHealth(mux, fakePinger{})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
