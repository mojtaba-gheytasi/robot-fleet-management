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

func (health BatteryHealth) isValid() bool {
	return health == BatteryHealthUnknown || health == BatteryHealthNormal || health == BatteryHealthLow
}
