package domain

import "errors"

var (
	ErrInvalidRobot        = errors.New("invalid robot")
	ErrInvalidObservation  = errors.New("invalid observation")
	ErrObservationConflict = errors.New("observation id already used with different content")
	ErrClockSkewExceeded   = errors.New("observation time is too far ahead of the server")
)
