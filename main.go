package main

import (
	"os"

	"github.com/RCooLeR/jinko-exporter/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args))
}
