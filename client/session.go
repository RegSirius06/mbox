package main

import (
	"fmt"
	"log"
	"net/http"
)

func newTunnel() (*tunnel, error) {
	t := &tunnel{
		streams: make(map[uint32]*localStream), udpFlows: make(map[uint32]*localUDPFlow), upQueues: make(map[uint32][][]byte), upPerStream: make(map[uint32]int), upSpace: make(chan struct{}, 1), wake: make(chan struct{}, 1), downWake: make(chan struct{}, 1), downDeliver: make(chan []byte, 64), done: make(chan struct{}), downPending: make(map[uint64][]byte), nextDown: 1, fatalErr: make(chan error, 1),
	}

	var err error
	t.http, err = newHTTPClient(base)
	if err != nil {
		return nil, fmt.Errorf("HTTP transport: %w", err)
	}

	openBody, noiseHS, openPSK, err := buildOpenRequest()
	if err != nil {
		return nil, err
	}

	data, status, err := t.requestOpen("POST", base+camouflage.OpenPath, openBody)
	if err != nil {
		return nil, err
	}

	// beta.2 servers reject non-empty Noise OPEN payloads. Retry once with
	// the legacy empty payload so a new Android client can still connect
	// during a rolling server upgrade.
	if status == http.StatusNotFound && deviceID != "" {
		log.Printf("NET stage=MBOX_OPEN code=OPEN_RETRY_LEGACY")
		openBody, noiseHS, openPSK, err = buildLegacyOpenRequest()
		if err != nil {
			return nil, err
		}
		data, status, err = t.requestOpen("POST", base+camouflage.OpenPath, openBody)
		if err != nil {
			return nil, err
		}
	}

	if status != http.StatusOK {
		log.Printf("NET stage=MBOX_OPEN code=ERR_HTTP_STATUS status=%d", status)
		return nil, fmt.Errorf("open HTTP %d", status)
	}
	log.Printf("NET stage=MBOX_OPEN code=MBOX_OPEN_OK")

	t.crypto, t.sid, err = finishClientHandshake(noiseHS, data, openPSK)
	if err != nil {
		log.Printf("NET stage=NOISE code=ERR_NOISE_AUTH error=%q", safeErr(err))
		return nil, fmt.Errorf("Noise handshake: %w", err)
	}
	log.Printf("NET stage=NOISE code=NOISE_OK")
	log.Printf("NET stage=TUNNEL code=TUNNEL_ESTABLISHED session=%s handshake=Noise_NKpsk0_25519_AESGCM_SHA256 record=v%d", redactedSessionID(t.sid), cryptoRecordVersion)

	go t.downDeliveryLoop()
	go t.uploader()
	go t.downloader()

	return t, nil
}

func (t *tunnel) fail(err error) {
	if err == nil {
		err = fmt.Errorf("tunnel stopped")
	}
	t.failOnce.Do(func() {
		t.fatalErr <- err
		close(t.done)
	})
}
