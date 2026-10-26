package application

import (
	"context"
	"fmt"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type ListRobotsQuery struct {
	// SiteID limits the list to one site; empty means every site.
	SiteID string
}

type ListRobots struct {
	robots RobotRepository
	clock  Clock
}

func NewListRobots(robots RobotRepository, clock Clock) *ListRobots {
	return &ListRobots{robots: robots, clock: clock}
}

func (query *ListRobots) Execute(ctx context.Context, request ListRobotsQuery) ([]RobotState, error) {
	var siteID *domain.SiteID
	if request.SiteID != "" {
		filter := domain.SiteID(request.SiteID)
		siteID = &filter
	}

	robots, err := query.robots.List(ctx, siteID)
	if err != nil {
		return nil, fmt.Errorf("list robots: %w", err)
	}

	// One time for the whole list, so every robot is judged at the same moment.
	now := query.clock()
	states := make([]RobotState, 0, len(robots))
	for _, robot := range robots {
		states = append(states, robotStateAt(robot, now))
	}

	return states, nil
}
