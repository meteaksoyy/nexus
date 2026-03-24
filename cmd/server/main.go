package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/meteaksoyy/nexus/config"
	"github.com/meteaksoyy/nexus/internal/cache"
	"github.com/meteaksoyy/nexus/internal/db"
	"github.com/meteaksoyy/nexus/internal/gateway"
	"github.com/meteaksoyy/nexus/internal/ibkr"
	"github.com/meteaksoyy/nexus/internal/tracing"
)

func main() {
	cfg := config.Load()

	log := zerolog.New(os.Stdout).With().Timestamp().Logger()
	if cfg.Env == "development" {
		log = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	ctx := context.Background()

	// Tracing
	shutdownTracing, err := tracing.Init(ctx, cfg.OTELEndpoint)
	if err != nil {
		log.Fatal().Err(err).Msg("init tracing")
	}
	defer func() {
		tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(tctx)
	}()

	// Database
	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("connect postgres")
	}
	defer pool.Close()

	// Redis
	rdb, err := cache.NewClient(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("connect redis")
	}
	defer rdb.Close()

	// IBKR Client Portal Gateway
	ibkrClient := ibkr.New(cfg.IBKRGatewayURL, cfg.IBKRUsername, cfg.IBKRPassword, log)
	if err := ibkrClient.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("start ibkr client")
	}
	defer ibkrClient.Stop()

	// Router
	r := gateway.NewRouter(cfg, pool, rdb, ibkrClient, log)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Info().Str("port", cfg.Port).Str("env", cfg.Env).Msg("nexus starting")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	log.Info().Msg("shutdown signal received — draining connections")

	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown failed")
	}

	log.Info().Msg("server stopped")
}
