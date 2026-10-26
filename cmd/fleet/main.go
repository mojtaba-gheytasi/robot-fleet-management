package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/bootstrap"
)

func main() {
	if err := bootstrap.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "fleet: %v\n", err)
		os.Exit(1)
	}
}
