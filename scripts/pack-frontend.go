//go:build ignore

package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"yaare/internal/buildassets"
)

func main() {
	source := flag.String("input", "frontend/dist", "built frontend directory")
	target := flag.String("output", "internal/adapters/frontendassets/packed/frontend.zip", "desktop resource archive")
	flag.Parse()
	result, err := buildassets.Pack(*source, *target)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		log.Fatal(err)
	}
}
