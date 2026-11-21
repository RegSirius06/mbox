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
	if err := configureRuntimeStatusPath(); err != nil {
		log.Fatalf("RUNTIME STATUS: %v", err)
	}
	configPath := runtimeConfigPath("/etc/mbox/server.json")
	setEgressConfigDirectory(configPath)
	if err := loadRuntimeConfig(configPath); err != nil {
		log.Fatalf("CONFIG: %v", err)
	}

	preAuthLimiter = newHandshakeLimiter(runtimeLimits.MaxConcurrentHandshakes, runtimeLimits.MaxHandshakeAttemptsPerSec)
	warnFileDescriptorLimit()
	startAuthReloadLoop(configPath)
	if credentialDirPath != "" {
		if _, err := startCredentialAdminSocket(credentialReloadSock); err != nil {
			log.Fatalf("CREDENTIAL ADMIN: %v", err)
		}
		defer stopCredentialAdminSocket()
	}
	mux := http.NewServeMux()
	registerHTTPHandlers(mux)
	startRuntimeStatusLoop()
	startSessionExpiryLoop()
	startCoverEngine(coverCfg)
	// Experimental datagram lane is opt-in and not part of HTTP/REG.RU.
	if path := os.Getenv("MBOX_DATAGRAM_ORIGIN_UNIX"); path != "" && enableUDPLabServer {
		laneListener, err := startOriginDatagrams(path)
		if err != nil {
			log.Print("DATAGRAM code=ORIGIN_LISTENER_UNAVAILABLE")
		} else {
			defer laneListener.Close()
			log.Print("DATAGRAM code=ORIGIN_LISTENING")
		}
	}

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
	stopCredentialAdminSocket()
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
