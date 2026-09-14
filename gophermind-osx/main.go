package main

import (
	"fmt"
	"os"
)

func main() {
	app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gophermind-osx:", err)
		os.Exit(1)
	}
	app.Show()
	app.Run()
	app.Close()
}
