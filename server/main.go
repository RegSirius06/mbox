package main

import (
	"log"
	"net/http"
	"time"
)

func main() {
	configPath := runtimeConfigPath("/etc/mbox/server.json")
	if err := loadRuntimeConfig(configPath); err != nil {
		log.Fatalf("CONFIG: %v", err)
	}

	preAuthLimiter = newHandshakeLimiter(
		runtimeLimits.MaxConcurrentHandshakes,
		runtimeLimits.MaxHandshakeAttemptsPerSec,
	)
	warnFileDescriptorLimit()
	startAuthReloadLoop(configPath)
	registerHTTPHandlers()
	startRuntimeStatusLoop()
	startSessionExpiryLoop()
	startCoverEngine(coverCfg)

	log.Printf("MBox server listening on %s -> %s", listen, socksAddr)
	srv := &http.Server{
		Addr:              listen,
		Handler:           nil,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    64 * 1024,
	}
	log.Fatal(srv.ListenAndServe())
}
