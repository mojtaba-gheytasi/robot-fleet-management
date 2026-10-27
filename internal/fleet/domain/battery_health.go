package domain

type BatteryHealth string

const (
	BatteryHealthUnknown BatteryHealth = "UNKNOWN"
	BatteryHealthNormal  BatteryHealth = "NORMAL"
	BatteryHealthLow     BatteryHealth = "LOW"
)

const (
	lowBelowHundredths    = 2000
	recoveredAtHundredths = 2500
)

func (health BatteryHealth) after(level BatteryLevel) BatteryHealth {
	if health == BatteryHealthLow {
		if level.hundredths >= recoveredAtHundredths {
			return BatteryHealthNormal
		}

		return BatteryHealthLow
	}

	if level.hundredths < lowBelowHundredths {
		return BatteryHealthLow
	}

	return BatteryHealthNormal
}

// allows says whether health can follow a reading of level: below 20% it is always LOW,
// at 25% or more always NORMAL, and in between it depends on the previous health.
func (health BatteryHealth) allows(level BatteryLevel) bool {
	switch {
	case level.hundredths < lowBelowHundredths:
		return health == BatteryHealthLow
	case level.hundredths >= recoveredAtHundredths:
		return health == BatteryHealthNormal
	default:
		return health == BatteryHealthLow || health == BatteryHealthNormal
	}
}

func (health BatteryHealth) isValid() bool {
	return health == BatteryHealthUnknown || health == BatteryHealthNormal || health == BatteryHealthLow
}
