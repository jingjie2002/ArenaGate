package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/jingjie2002/ArenaGate/internal/config"
	"github.com/jingjie2002/ArenaGate/internal/coreclient"
	"github.com/jingjie2002/ArenaGate/internal/gateway"
)

func main() {
	cfg := config.FromEnv()
	core := coreclient.NewHTTPClient(cfg.CoreRankHTTP, 3*time.Second)
	gate := gateway.NewServer(cfg, core)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           gate.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("[ArenaGate] listening on %s, CoreRank=%s", cfg.Addr, cfg.CoreRankHTTP)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[ArenaGate] server failed: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("[ArenaGate] shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("[ArenaGate] graceful shutdown failed: %v", err)
	}
}
