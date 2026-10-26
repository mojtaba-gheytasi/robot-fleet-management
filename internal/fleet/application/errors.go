package application

import "errors"

var (
	ErrRobotNotFound             = errors.New("robot not found")
	ErrSiteNotFound              = errors.New("site not found")
	ErrRobotRegistrationConflict = errors.New("robot already registered with different data")

	// ErrRobotAlreadyExists passes from the repository to the use case only,
	// which decides whether it is a retry or a conflict.
	ErrRobotAlreadyExists = errors.New("robot already exists")
)
