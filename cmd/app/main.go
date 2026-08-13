package main

import (
	"creaves-console/actions"
)

func main() {
	app := actions.App()
	if err := app.Serve(); err != nil {
		panic(err)
	}
}
