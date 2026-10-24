package main

import (
	"context"
	"os"

	"github.com/geshido/icomark/internal/app"
	"github.com/geshido/icomark/internal/backend/agterm"
)

func main() {
	os.Exit(app.New("atabs", agterm.New()).Run(context.Background(), os.Args[1:]))
}
