package domain

import "fmt"

type OperatingMode string

const (
	OperatingModeIdle     OperatingMode = "IDLE"
	OperatingModeWorking  OperatingMode = "WORKING"
	OperatingModeCharging OperatingMode = "CHARGING"
	OperatingModeError    OperatingMode = "ERROR"
)

func NewOperatingMode(value string) (OperatingMode, error) {
	mode := OperatingMode(value)
	if mode.isValid() == false {
		return "", fmt.Errorf(
			"%w: operating mode must be IDLE, WORKING, CHARGING or ERROR",
			ErrInvalidObservation,
		)
	}

	return mode, nil
}

func (mode OperatingMode) isValid() bool {
	switch mode {
	case OperatingModeIdle, OperatingModeWorking, OperatingModeCharging, OperatingModeError:
		return true
	default:
		return false
	}
}
