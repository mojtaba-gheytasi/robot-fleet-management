package domain

import "fmt"

// OperatingMode is what the robot reports it is doing. It is shown, but does
// not drive any rule in this scope (HLT-08).
type OperatingMode string

const (
	OperatingModeIdle     OperatingMode = "IDLE"
	OperatingModeWorking  OperatingMode = "WORKING"
	OperatingModeCharging OperatingMode = "CHARGING"
	OperatingModeError    OperatingMode = "ERROR"
)

func NewOperatingMode(value string) (OperatingMode, error) {
	mode := OperatingMode(value)
	switch mode {
	case OperatingModeIdle, OperatingModeWorking, OperatingModeCharging, OperatingModeError:
		return mode, nil
	default:
		return "", fmt.Errorf(
			"%w: operating mode must be IDLE, WORKING, CHARGING or ERROR",
			ErrInvalidObservation,
		)
	}
}
