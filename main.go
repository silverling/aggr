package main

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg := loadConfig()

	db, err := sql.Open("sqlite", cfg.DatabasePath)
	if err != nil {
		logger.Error("open sqlite database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	db.SetConnMaxIdleTime(5 * time.Minute)

	server, err := newServer(cfg, db, logger)
	if err != nil {
		logger.Error("initialize server", "error", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Info("starting aggr",
		"addr", cfg.Addr,
		"db", cfg.DatabasePath,
		"environment", cfg.Environment,
		"web_dev_url", cfg.WebDevURL,
	)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("run http server", "error", err)
		os.Exit(1)
	}
}

func loadConfig() config {
	environment := getenv("AGGR_ENV", "prod")
	webDevURL := strings.TrimSpace(os.Getenv("AGGR_WEB_DEV_URL"))
	if environment == "dev" && webDevURL == "" {
		webDevURL = "http://127.0.0.1:5173"
	}

	return config{
		Addr:         getenv("AGGR_ADDR", ":8080"),
		DatabasePath: getenv("AGGR_DB_PATH", "aggr.db"),
		Environment:  environment,
		WebDevURL:    webDevURL,
	}
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
