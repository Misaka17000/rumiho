package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"rumiho/internal/rumiho"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := rumiho.Healthcheck(); err != nil {
			os.Exit(1)
		}
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	app, err := rumiho.New(rumiho.OptionsFromEnv())
	if err != nil {
		log.Fatal(err)
	}
	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
