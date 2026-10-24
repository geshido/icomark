package main

import (
	"context"
	"os"

	"github.com/geshido/icomark/internal/app"
	"github.com/geshido/icomark/internal/backend/herdr"
)

func main() {
	os.Exit(app.New("htabs", herdr.New()).Run(context.Background(), os.Args[1:]))
}
