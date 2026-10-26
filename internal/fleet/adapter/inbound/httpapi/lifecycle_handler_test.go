package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/inbound/httpapi"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestDeactivateAndActivateReturnTheRobot(t *testing.T) {
	inactive := testState(t, "robot-101", "site-north")
	inactive.Robot.Deactivate()
	active := testState(t, "robot-101", "site-north")

	tests := map[string]struct {
		path          string
		config        func(*fakeChangeLifecycle) httpapi.Config
		state         application.RobotState
		wantLifecycle string
	}{
		"deactivate": {
			path:          "/api/robots/robot-101/deactivate",
			config:        func(fake *fakeChangeLifecycle) httpapi.Config { return httpapi.Config{DeactivateRobot: fake} },
			state:         inactive,
			wantLifecycle: "INACTIVE",
		},
		"activate": {
			path:          "/api/robots/robot-101/activate",
			config:        func(fake *fakeChangeLifecycle) httpapi.Config { return httpapi.Config{ActivateRobot: fake} },
			state:         active,
			wantLifecycle: "ACTIVE",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			fake := &fakeChangeLifecycle{state: tt.state}

			response := serve(tt.config(fake), http.MethodPost, tt.path, "")

			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, "robot-101", fake.robotID)
			var body map[string]any
			decodeBody(t, response.Body.Bytes(), &body)
			assert.Equal(t, tt.wantLifecycle, body["lifecycle"])
		})
	}
}

func TestChangeLifecycleOfAnUnknownRobot(t *testing.T) {
	fake := &fakeChangeLifecycle{err: application.ErrRobotNotFound}

	for path, config := range map[string]httpapi.Config{
		"/api/robots/robot-404/deactivate": {DeactivateRobot: fake},
		"/api/robots/robot-404/activate":   {ActivateRobot: fake},
	} {
		t.Run(path, func(t *testing.T) {
			response := serve(config, http.MethodPost, path, "")

			assert.Equal(t, http.StatusNotFound, response.Code)
			assert.Equal(t, "ROBOT_NOT_FOUND", errorCode(t, response))
		})
	}
}

func TestTelemetryForAnInactiveRobotIsAConflict(t *testing.T) {
	response := postTelemetry(&fakeRecordTelemetry{err: domain.ErrRobotInactive}, validTelemetryBody)

	assert.Equal(t, http.StatusConflict, response.Code)
	assert.Equal(t, "ROBOT_INACTIVE", errorCode(t, response))
}

type fakeChangeLifecycle struct {
	state   application.RobotState
	err     error
	robotID string
}

func (useCase *fakeChangeLifecycle) Execute(_ context.Context, robotID string) (application.RobotState, error) {
	useCase.robotID = robotID

	return useCase.state, useCase.err
}
