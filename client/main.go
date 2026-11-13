package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Register before the first handshake so an early wake signal cannot terminate startup.
	probeSigCh := make(chan os.Signal, 1)
	signal.Notify(probeSigCh, syscall.SIGUSR1)
	defer signal.Stop(probeSigCh)
	if err := loadRuntimeConfig(runtimeConfigPath("/etc/mbox/client.json")); err != nil {
		log.Fatalf("CONFIG: %v", err)
	}

	t, err := newTunnel()
	if err != nil {
		log.Fatal(err)
	}

	ln, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("NET stage=PROCESS code=CORE_PID pid=%d", os.Getpid())
	log.Printf(
		"MBox client SOCKS listening on %s session=%s",
		listen,
		redactedSessionID(t.sid),
	)

	shutdown := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	// Android asks for one immediate authenticated probe on wake/Doze exit.
	go func() {
		for {
			select {
			case <-probeSigCh:
				t.pokeHealth()
			case <-t.done:
				return
			case <-shutdown:
				return
			}
		}
	}()

	go func() {
		select {
		case err := <-t.fatalErr:
			log.Printf("FATAL: %v", err)
			_ = ln.Close()
		case sig := <-sigCh:
			log.Printf("SHUTDOWN signal=%s", sig)
			t.closeRemote()
			close(shutdown)
			_ = ln.Close()
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-t.done:
				return
			case <-shutdown:
				return
			default:
				log.Printf("ACCEPT: %v", err)
				continue
			}
		}

		go t.handleLocal(conn)
	}
}
