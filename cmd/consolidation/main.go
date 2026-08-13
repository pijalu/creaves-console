package main

import (
	"creaves-console/actions"
	"log"
)

func main() {
	app := actions.App()
	if err := app.Serve(); err != nil {
		log.Fatal(err)
	}
}
