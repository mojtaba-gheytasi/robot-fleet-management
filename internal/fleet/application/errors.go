package application

import "errors"

var (
	ErrRobotNotFound             = errors.New("robot not found")
	ErrSiteNotFound              = errors.New("site not found")
	ErrRobotRegistrationConflict = errors.New("robot already registered with different data")

	ErrRobotAlreadyExists = errors.New("robot already exists")
)
