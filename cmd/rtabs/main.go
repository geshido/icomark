package main

import (
	"context"
	"os"

	"github.com/geshido/icomark/internal/app"
	"github.com/geshido/icomark/internal/backend/rex"
)

func main() {
	os.Exit(app.New("rtabs", rex.New()).Run(context.Background(), os.Args[1:]))
}
