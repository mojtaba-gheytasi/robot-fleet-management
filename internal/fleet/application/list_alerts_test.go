package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestListAlertsBoundsTheLimit(t *testing.T) {
	tests := map[string]struct {
		requested int
		want      int
	}{
		"not given":     {0, 50},
		"within range":  {10, 10},
		"at the cap":    {200, 200},
		"above the cap": {5000, 200},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			alerts := &fakeAlerts{}

			_, err := application.NewListAlerts(alerts).Execute(context.Background(), application.ListAlertsQuery{Limit: tt.requested})
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
}

func (alerts *fakeAlerts) List(_ context.Context, terminal bool, limit int) ([]*domain.Alert, error) {
	alerts.terminal = terminal
	alerts.limit = limit

	return nil, nil
}
