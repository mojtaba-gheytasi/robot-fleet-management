package httpapi

import (
	"errors"
	"net/http"
	"strconv"
)

var errInvalidLimit = errors.New("limit must be a positive integer")

// parseLimit returns 0 when no limit is given; the query applies its default.
func parseLimit(r *http.Request) (int, error) {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return 0, nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return 0, errInvalidLimit
	}

	return limit, nil
}
