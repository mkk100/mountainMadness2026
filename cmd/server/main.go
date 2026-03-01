package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"ratemylifedecision/internal/config"
	"ratemylifedecision/internal/database"
	"ratemylifedecision/internal/httpapi"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()

	openaiAPIKey := os.Getenv("OPENAI_API_KEY")
	if openaiAPIKey == "" {
		log.Println("warning: OPENAI_API_KEY not set, decision categorization will use default category")
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.New(db, openaiAPIKey),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("server listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server stopped with error: %v", err)
	}
}
