package alertcheck_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/inbound/alertcheck"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
)

func TestAlertCheckRunsAtStartAndOnEveryTick(t *testing.T) {
	reconcile := &fakeReconcile{}
	ctx, cancel := context.WithCancel(context.Background())
	done := runInBackground(ctx, alertcheck.New(reconcile, 10*time.Millisecond, discardLogger()))

	assert.Eventually(t, func() bool { return reconcile.calls.Load() >= 3 }, time.Second, time.Millisecond)

	cancel()
	assertStops(t, done)
}

func TestAlertCheckRunsImmediatelyWithoutWaitingForTheFirstTick(t *testing.T) {
	reconcile := &fakeReconcile{}
	ctx, cancel := context.WithCancel(context.Background())
	done := runInBackground(ctx, alertcheck.New(reconcile, time.Hour, discardLogger()))

	assert.Eventually(t, func() bool { return reconcile.calls.Load() == 1 }, time.Second, time.Millisecond)

	cancel()
	assertStops(t, done)
}

func TestAlertCheckKeepsRunningAfterAFailure(t *testing.T) {
	reconcile := &fakeReconcile{err: errors.New("database down")}
	ctx, cancel := context.WithCancel(context.Background())
	done := runInBackground(ctx, alertcheck.New(reconcile, 10*time.Millisecond, discardLogger()))

	assert.Eventually(t, func() bool { return reconcile.calls.Load() >= 3 }, time.Second, time.Millisecond)

	cancel()
	assertStops(t, done)
}

func runInBackground(ctx context.Context, check *alertcheck.AlertCheck) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		check.Run(ctx)
		close(done)
	}()

	return done
}

func assertStops(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the alert check did not stop after its context was cancelled")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeReconcile struct {
	calls atomic.Int32
	err   error
}

func (reconcile *fakeReconcile) Execute(context.Context) (application.AlertReconciliation, error) {
	reconcile.calls.Add(1)

	return application.AlertReconciliation{}, reconcile.err
}
