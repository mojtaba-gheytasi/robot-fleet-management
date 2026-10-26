package domain

import "errors"

var (
	ErrInvalidRobot       = errors.New("invalid robot")
	ErrInvalidObservation = errors.New("invalid observation")
)
