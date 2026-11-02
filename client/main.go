package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
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

	log.Printf(
		"MBox client SOCKS listening on %s session=%s",
		listen,
		redactedSessionID(t.sid),
	)

	shutdown := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

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
