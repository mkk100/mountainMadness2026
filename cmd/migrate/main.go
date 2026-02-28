package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"ratemylifedecision/internal/config"
	"ratemylifedecision/internal/database"
	"ratemylifedecision/internal/migrate"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "up" {
		log.Fatalf("usage: go run ./cmd/migrate up")
	}

	cfg := config.Load()
	ctx := context.Background()

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect db: %v", err)
	}
	defer db.Close()

	if err := migrate.Up(ctx, db, "migrations"); err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	fmt.Println("migrations applied successfully")
}
