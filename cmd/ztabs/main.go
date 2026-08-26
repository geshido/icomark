package main

import (
	"context"
	"os"

	"github.com/geshido/icomark/internal/app"
	"github.com/geshido/icomark/internal/backend/zellij"
)

func main() {
	os.Exit(app.New("ztabs", zellij.New()).Run(context.Background(), os.Args[1:]))
}
