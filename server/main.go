package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	configPath := runtimeConfigPath("/etc/mbox/server.json")
	if err := loadRuntimeConfig(configPath); err != nil {
		log.Fatalf("CONFIG: %v", err)
	}

	preAuthLimiter = newHandshakeLimiter(runtimeLimits.MaxConcurrentHandshakes, runtimeLimits.MaxHandshakeAttemptsPerSec)
	warnFileDescriptorLimit()
	startAuthReloadLoop(configPath)
	mux := http.NewServeMux()
	registerHTTPHandlers(mux)
	startRuntimeStatusLoop()
	startSessionExpiryLoop()
	startCoverEngine(coverCfg)

	srv := &http.Server{
		Addr: listen, Handler: metricsMiddleware(originAuthMiddleware(mux)), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 64 * 1024,
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatalf("LISTEN: %v", err)
	}
	serverReady.Store(true)
	log.Printf("MBox server listening on %s -> %s", listen, socksAddr)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case err := <-serveErr:
		serverReady.Store(false)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("SERVE: %v", err)
		}
		return
	case sig := <-sigCh:
		log.Printf("SHUTDOWN signal=%s entering drain", sig)
	}

	// Security invariant: once drain begins no new authenticated session may be
	// admitted, while existing ownership/accounting is explicitly released.
	serverDraining.Store(true)
	serverReady.Store(false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = srv.Shutdown(ctx)
	cancel()
	closeAllSessions()
	if !waitForResourcesReleased(5 * time.Second) {
		log.Printf("SHUTDOWN resources_not_released sessions/streams/queued remain")
	}
	writeRuntimeStatus()
	log.Printf("SHUTDOWN complete")
}
