package domain

import (
	"fmt"
	"math"
)

type Position struct {
	x float64
	y float64
}

func NewPosition(x, y float64) (Position, error) {
	if isFinite(x) == false || isFinite(y) == false {
		return Position{}, fmt.Errorf("%w: position must be finite numbers", ErrInvalidObservation)
	}

	return Position{x: x, y: y}, nil
}

func (position Position) X() float64 {
	return position.x
}

func (position Position) Y() float64 {
	return position.y
}

func isFinite(value float64) bool {
	return math.IsNaN(value) == false && math.IsInf(value, 0) == false
}
