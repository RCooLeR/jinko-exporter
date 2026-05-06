package main

import (
	"os"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args))
}
