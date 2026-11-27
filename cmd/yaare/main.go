package main

import (
	"log"

	"yaare/internal/app"
	"yaare/internal/desktop/lifecycle"
	_ "yaare/locales"
)

func main() {
	if err := lifecycle.Run(app.New()); err != nil {
		log.Fatal(err)
	}
}
