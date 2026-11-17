package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"listingboard/internal/store"
	"listingboard/internal/worker"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	pause := 2 * time.Second
	if value := os.Getenv("STEP_PAUSE"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed < 0 {
			log.Fatalf("STEP_PAUSE must be a duration such as 2s, got %q", value)
		}
		pause = parsed
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	st, err := store.Wait(dsn, 30*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Printf("worker started, step pause %s", pause)
	worker.Run(ctx, st, dataDir, pause)
}
