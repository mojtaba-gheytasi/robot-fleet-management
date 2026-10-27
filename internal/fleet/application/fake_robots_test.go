package application_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type fakeRobots struct {
	stored       map[domain.RobotID]*domain.Robot
	createErr    error
	createCalls  int
	listedSiteID *domain.SiteID
	added        []recordedObservation
	alerts       []*domain.Alert
	mutex        sync.Mutex
}

type recordedObservation struct {
	robotID     domain.RobotID
	observation domain.Observation
	receivedAt  time.Time
	outcome     domain.ObservationOutcome
}

func newFakeRobots() *fakeRobots {
	return &fakeRobots{stored: map[domain.RobotID]*domain.Robot{}}
}

func (robots *fakeRobots) store(
	t *testing.T,
	id domain.RobotID,
	siteID domain.SiteID,
	lifecycle domain.Lifecycle,
	registeredAt time.Time,
) {
	t.Helper()

	robot, err := domain.ReconstituteRobot(id, siteID, lifecycle, registeredAt, registeredAt, domain.BatteryHealthUnknown, nil)
	require.NoError(t, err)
	robots.stored[id] = robot
}

func (robots *fakeRobots) Create(_ context.Context, robot *domain.Robot) error {
	robots.createCalls++
	if robots.createErr != nil {
		return robots.createErr
	}
	if _, exists := robots.stored[robot.ID()]; exists {
		return application.ErrRobotAlreadyExists
	}

	robots.stored[robot.ID()] = robot

	return nil
}

func (robots *fakeRobots) FindByID(_ context.Context, id domain.RobotID) (*domain.Robot, error) {
	robot, exists := robots.stored[id]
	if exists == false {
		return nil, application.ErrRobotNotFound
	}

	return robot, nil
}

func (robots *fakeRobots) List(_ context.Context, siteID *domain.SiteID) ([]*domain.Robot, error) {
	robots.listedSiteID = siteID

	listed := make([]*domain.Robot, 0)
	for _, robot := range robots.stored {
		if siteID == nil || robot.SiteID() == *siteID {
			listed = append(listed, robot)
		}
	}
	sort.Slice(listed, func(i, j int) bool { return listed[i].ID() < listed[j].ID() })

	return listed, nil
}

func (robots *fakeRobots) WithLockedRobot(
	ctx context.Context,
	id domain.RobotID,
	fn func(robot *domain.Robot, tx application.RobotTx) error,
) error {
	robots.mutex.Lock()
	defer robots.mutex.Unlock()

	robot, err := robots.FindByID(ctx, id)
	if err != nil {
		return err
	}

	tx := &fakeRobotTx{robotID: id, robots: robots}
	if err := fn(robot, tx); err != nil {
		return err
	}

	robots.added = append(robots.added, tx.pending...)
	for _, saved := range tx.savedAlerts {
		robots.storeAlert(saved)
	}

	return nil
}

// storeAlert replaces the alert with the same ID, or adds it.
func (robots *fakeRobots) storeAlert(alert *domain.Alert) {
	for i, stored := range robots.alerts {
		if stored.ID() == alert.ID() {
			robots.alerts[i] = alert
			return
		}
	}

	robots.alerts = append(robots.alerts, alert)
}

func (robots *fakeRobots) alertsOf(id domain.RobotID) []*domain.Alert {
	alerts := make([]*domain.Alert, 0)
	for _, alert := range robots.alerts {
		if alert.RobotID() == id {
			alerts = append(alerts, alert)
		}
	}

	return alerts
}

type fakeRobotTx struct {
	robotID     domain.RobotID
	robots      *fakeRobots
	pending     []recordedObservation
	savedAlerts []*domain.Alert
}

func (log *fakeRobotTx) FindObservation(_ context.Context, id domain.ObservationID) (domain.Observation, bool, error) {
	for _, recorded := range log.robots.added {
		if recorded.robotID == log.robotID && recorded.observation.ID() == id {
			return recorded.observation, true, nil
		}
	}

	return domain.Observation{}, false, nil
}

func (log *fakeRobotTx) AddObservation(
	_ context.Context,
	observation domain.Observation,
	receivedAt time.Time,
	outcome domain.ObservationOutcome,
) error {
	log.pending = append(log.pending, recordedObservation{
		robotID:     log.robotID,
		observation: observation,
		receivedAt:  receivedAt,
		outcome:     outcome,
	})

	return nil
}

// FindUnresolvedAlert returns a copy, so a rolled-back change never reaches the stored alert.
func (log *fakeRobotTx) FindUnresolvedAlert(context.Context) (*domain.Alert, error) {
	for _, alert := range log.robots.alertsOf(log.robotID) {
		if alert.Status() == domain.AlertStatusOpen || alert.Status() == domain.AlertStatusAcknowledged {
			copied := *alert
			return &copied, nil
		}
	}

	return nil, nil
}

func (log *fakeRobotTx) AddAlert(_ context.Context, alert *domain.Alert) error {
	log.savedAlerts = append(log.savedAlerts, alert)

	return nil
}

func (log *fakeRobotTx) SaveAlert(_ context.Context, alert *domain.Alert) error {
	log.savedAlerts = append(log.savedAlerts, alert)

	return nil
}
