package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestListAlertsLimits(t *testing.T) {
	tests := map[string]struct {
		terminal  bool
		requested int
		want      int
	}{
		"unresolved, not given":   {false, 0, 0},
		"unresolved, given":       {false, 10, 10},
		"unresolved, above 200":   {false, 5000, 5000},
		"terminal, not given":     {true, 0, 50},
		"terminal, within range":  {true, 10, 10},
		"terminal, at the cap":    {true, 200, 200},
		"terminal, above the cap": {true, 5000, 200},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			alerts := &fakeAlerts{}

			_, err := application.NewListAlerts(alerts).
				Execute(context.Background(), application.ListAlertsQuery{Terminal: tt.terminal, Limit: tt.requested})
			require.NoError(t, err)

			assert.Equal(t, tt.want, alerts.limit)
		})
	}
}

func TestListAlertsPassesTheState(t *testing.T) {
	alerts := &fakeAlerts{}

	_, err := application.NewListAlerts(alerts).Execute(context.Background(), application.ListAlertsQuery{Terminal: true})
	require.NoError(t, err)

	assert.True(t, alerts.terminal)
}

type fakeAlerts struct {
	terminal bool
	limit    int
	stored   map[domain.AlertID]*domain.Alert
}

func (alerts *fakeAlerts) List(_ context.Context, terminal bool, limit int) ([]*domain.Alert, error) {
	alerts.terminal = terminal
	alerts.limit = limit

	return nil, nil
}

func (alerts *fakeAlerts) WithLockedAlert(_ context.Context, id domain.AlertID, fn func(alert *domain.Alert) error) error {
	alert, ok := alerts.stored[id]
	if ok == false {
		return application.ErrAlertNotFound
	}

	return fn(alert)
}
