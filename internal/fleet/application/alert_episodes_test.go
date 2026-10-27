package application_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestATransientLowEpisodeIsKeptInAlertHistory(t *testing.T) {
	robots := activeRobot(t)

	report(t, robots, 1, 60)
	report(t, robots, 2, 18)
	report(t, robots, 3, 30)

	alerts := robots.alertsOf("robot-101")
	require.Len(t, alerts, 1)
	assert.Equal(t, domain.AlertStatusResolved, alerts[0].Status())
	assert.Equal(t, receivedAt(2), alerts[0].OpenedAt(), "opened when the low reading was received")
	endedAt, _ := alerts[0].EndedAt()
	assert.Equal(t, receivedAt(3), endedAt, "resolved when the recovery was received")
}

func TestASecondLowEpisodeOpensANewAlert(t *testing.T) {
	robots := activeRobot(t)
	report(t, robots, 1, 18)
	acknowledgedAt := receivedAt(1).Add(time.Second)
	require.NoError(t, robots.alerts[0].Acknowledge(acknowledgedAt))

	report(t, robots, 2, 30)
	report(t, robots, 3, 15)

	alerts := robots.alertsOf("robot-101")
	require.Len(t, alerts, 2)
	assert.Equal(t, domain.AlertStatusResolved, alerts[0].Status())
	first, _ := alerts[0].AcknowledgedAt()
	assert.Equal(t, acknowledgedAt, first, "the first episode keeps its acknowledgement")
	assert.Equal(t, domain.AlertStatusOpen, alerts[1].Status(), "the new episode is not acknowledged")
	_, acknowledged := alerts[1].AcknowledgedAt()
	assert.False(t, acknowledged)
	assert.NotEqual(t, alerts[0].ID(), alerts[1].ID())
}

func TestLowReadingsInOneEpisodeKeepOneAlert(t *testing.T) {
	robots := activeRobot(t)

	for second, percent := range []float64{18, 15, 22, 24.99, 10} {
		report(t, robots, second+1, percent)
	}

	alerts := robots.alertsOf("robot-101")
	require.Len(t, alerts, 1)
	assert.Equal(t, domain.AlertStatusOpen, alerts[0].Status())
	assert.Equal(t, receivedAt(1), alerts[0].OpenedAt())
}

func TestNormalReadingsOpenNoAlert(t *testing.T) {
	robots := activeRobot(t)

	report(t, robots, 1, 60)
	report(t, robots, 2, 20)

	assert.Empty(t, robots.alerts)
}

func TestASupersededLowReadingOpensNoAlert(t *testing.T) {
	robots := activeRobot(t)
	report(t, robots, 2, 60)

	result := report(t, robots, 1, 10)

	assert.Equal(t, application.TelemetryOutcomeSuperseded, result.Outcome)
	assert.Equal(t, domain.BatteryHealthNormal, robots.stored["robot-101"].BatteryHealth())
	assert.Empty(t, robots.alerts)
}

func TestAResentLowReadingOpensNoSecondAlert(t *testing.T) {
	robots := activeRobot(t)
	report(t, robots, 1, 18)

	result := report(t, robots, 1, 18)

	assert.Equal(t, application.TelemetryOutcomeDuplicate, result.Outcome)
	require.Len(t, robots.alerts, 1)
	assert.Equal(t, domain.AlertStatusOpen, robots.alerts[0].Status())
}

func TestARejectedReadingChangesNoAlert(t *testing.T) {
	robots := activeRobot(t)
	report(t, robots, 1, 18)

	command := batteryTelemetry(2, 30)
	command.ObservedAt = receivedAt(2).Add(time.Minute)
	_, err := recordTelemetry(robots, receivedAt(2), command)

	require.ErrorIs(t, err, domain.ErrClockSkewExceeded)
	require.Len(t, robots.alerts, 1)
	assert.Equal(t, domain.AlertStatusOpen, robots.alerts[0].Status())
}

func TestAfterReactivationTheNextAppliedReadingDecides(t *testing.T) {
	tests := map[string]struct {
		freshPercent   float64
		wantNewAlert   bool
		wantHealthNext domain.BatteryHealth
	}{
		"still low": {18, true, domain.BatteryHealthLow},
		"recovered": {30, false, domain.BatteryHealthNormal},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			robots := activeRobot(t)
			report(t, robots, 1, 15)
			_, err := application.NewDeactivateRobot(robots, clockAt(receivedAt(2))).Execute(context.Background(), "robot-101")
			require.NoError(t, err)
			closed := robots.alerts[0]
			require.Equal(t, domain.AlertStatusClosed, closed.Status())

			_, err = application.NewActivateRobot(robots, clockAt(receivedAt(3))).Execute(context.Background(), "robot-101")
			require.NoError(t, err)
			assert.Len(t, robots.alerts, 1, "reactivation alone opens no alert")
			assert.Equal(t, domain.BatteryHealthLow, robots.stored["robot-101"].BatteryHealth())

			report(t, robots, 4, tt.freshPercent)

			assert.Equal(t, tt.wantHealthNext, robots.stored["robot-101"].BatteryHealth())
			assert.Same(t, closed, robots.alerts[0], "the closed alert is not touched")
			if tt.wantNewAlert {
				require.Len(t, robots.alerts, 2)
				assert.Equal(t, domain.AlertStatusOpen, robots.alerts[1].Status())
				assert.Equal(t, receivedAt(4), robots.alerts[1].OpenedAt())
			} else {
				assert.Len(t, robots.alerts, 1)
			}
		})
	}
}

func activeRobot(t *testing.T) *fakeRobots {
	t.Helper()

	robots := newFakeRobots()
	robots.store(t, "robot-101", "site-north", domain.LifecycleActive, fixedNow)

	return robots
}

// report sends a reading observed at fixedNow+second and received a minute after that.
func report(t *testing.T, robots *fakeRobots, second int, percent float64) application.RecordTelemetryResult {
	t.Helper()

	result, err := recordTelemetry(robots, receivedAt(second), batteryTelemetry(second, percent))
	require.NoError(t, err)

	return result
}

func batteryTelemetry(second int, percent float64) application.RecordTelemetryCommand {
	command := validTelemetry()
	command.ObservationID = fmt.Sprintf("obs-%d", second)
	command.ObservedAt = fixedNow.Add(time.Duration(second) * time.Second)
	command.BatteryPercent = percent

	return command
}

func receivedAt(second int) time.Time {
	return fixedNow.Add(time.Minute + time.Duration(second)*time.Second)
}
