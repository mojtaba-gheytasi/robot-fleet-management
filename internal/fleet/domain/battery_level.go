package domain

import (
	"fmt"
	"math"
)

// BatteryLevel is a percentage with at most two decimals, held as whole
// hundredths so two readings compare exactly. A reading with more decimals is
// rejected rather than rounded: rounding would let two different payloads
// look identical.
type BatteryLevel struct {
	hundredths int
}

func NewBatteryLevel(percent float64) (BatteryLevel, error) {
	if math.IsNaN(percent) || percent < 0 || percent > 100 {
		return BatteryLevel{}, fmt.Errorf("%w: battery must be between 0 and 100", ErrInvalidObservation)
	}

	// Floats cannot hold most decimals exactly (18.55 * 100 is 1854.9999...),
	// so "two decimals" means "within a tiny distance of a whole hundredth".
	scaled := percent * 100
	hundredths := math.Round(scaled)
	if math.Abs(scaled-hundredths) > 1e-6 {
		return BatteryLevel{}, fmt.Errorf("%w: battery has more than two decimals", ErrInvalidObservation)
	}

	return BatteryLevel{hundredths: int(hundredths)}, nil
}

func (level BatteryLevel) Percent() float64 {
	return float64(level.hundredths) / 100
}

// String formats the level with exactly two decimals, e.g. "18.50".
func (level BatteryLevel) String() string {
	return fmt.Sprintf("%d.%02d", level.hundredths/100, level.hundredths%100)
}
