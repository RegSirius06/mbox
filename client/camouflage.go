package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type timingConfig struct {
	IdleMinMS       int `json:"idle_min_ms"`
	IdleMaxMS       int `json:"idle_max_ms"`
	WakeMinMS       int `json:"wake_min_ms"`
	WakeMaxMS       int `json:"wake_max_ms"`
	ActiveJitterP   int `json:"active_jitter_percent"`
	ActiveMinMS     int `json:"active_min_ms"`
	ActiveMaxMS     int `json:"active_max_ms"`
	ActiveBackoffMS int `json:"active_backoff_max_ms"`
}

type recoveryConfig struct {
	FailureTimeoutMS int `json:"failure_timeout_ms"`
	RetryMinMS       int `json:"retry_min_ms"`
	RetryMaxMS       int `json:"retry_max_ms"`
}

type camouflageConfig struct {
	Timing                   timingConfig      `json:"timing"`
	Recovery                 recoveryConfig    `json:"recovery"`
	OpenPath                 string            `json:"open_path"`
	UpPath                   string            `json:"up_path"`
	DownPath                 string            `json:"down_path"`
	ClosePath                string            `json:"close_path"`
	ContentType              string            `json:"content_type"`
	RequestHeaders           map[string]string `json:"request_headers"`
	ResponseHeaders          map[string]string `json:"response_headers"`
	DownMaxBytes             int               `json:"down_max_bytes"`
	UpMaxBytes               int               `json:"up_max_bytes"`
	PaddingClasses           []int             `json:"padding_classes"`
	PaddingExtraClassPercent int               `json:"padding_extra_class_percent"`
	PaddingRandomMaxBytes    int               `json:"padding_random_max_bytes"`
	BatchMinBytes            int               `json:"batch_min_bytes"`
	NoiseChunkMinBytes       int               `json:"noise_chunk_min_bytes"`
	NoiseChunkMaxBytes       int               `json:"noise_chunk_max_bytes"`
}

var camouflage = camouflageConfig{
	Timing: timingConfig{
		IdleMinMS:       45000,
		IdleMaxMS:       90000,
		WakeMinMS:       100,
		WakeMaxMS:       220,
		ActiveJitterP:   35,
		ActiveMinMS:     120,
		ActiveMaxMS:     420,
		ActiveBackoffMS: 2200,
	},
	Recovery: recoveryConfig{
		FailureTimeoutMS: 30000,
		RetryMinMS:       300,
		RetryMaxMS:       5000,
	},
	OpenPath:    "/api/v1/session",
	UpPath:      "/api/v1/events/",
	DownPath:    "/api/v1/sync/",
	ClosePath:   "/api/v1/session/",
	ContentType: "application/json",
	RequestHeaders: map[string]string{
		"Accept": "application/json",
	},
	ResponseHeaders: map[string]string{
		"Cache-Control": "no-store",
	},
	DownMaxBytes: 262144,
	UpMaxBytes:   262144,
	PaddingClasses: []int{
		512, 1024, 2048, 4096, 8192, 16384,
		32768, 65536, 131072, 262144, 524288,
	},
	PaddingExtraClassPercent: 0,
	PaddingRandomMaxBytes:    16384,
	BatchMinBytes:            73728,
	NoiseChunkMinBytes:       16384,
	NoiseChunkMaxBytes:       61440,
}

func paddedPlaintextSize(real int) (int, error) {
	needed := real + 4
	if camouflage.PaddingRandomMaxBytes > 0 {
		if deterministicTransport {
			return needed + camouflage.PaddingRandomMaxBytes/2, nil
		}
		extra, err := randomIntInclusive(0, camouflage.PaddingRandomMaxBytes)
		if err != nil {
			return 0, err
		}
		return needed + extra, nil
	}

	// Compatibility mode for explicitly configured legacy padding classes.
	for i, class := range camouflage.PaddingClasses {
		if class < needed {
			continue
		}
		target := class
		if camouflage.PaddingExtraClassPercent > 0 && i+1 < len(camouflage.PaddingClasses) {
			var b [1]byte
			if _, err := rand.Read(b[:]); err != nil {
				return 0, err
			}
			if int(b[0])*100/256 < camouflage.PaddingExtraClassPercent {
				target = camouflage.PaddingClasses[i+1]
			}
		}
		return target, nil
	}
	return 0, fmt.Errorf("plaintext too large for padding policy: %d", real)
}

func randomBatchLimit(max int) int {
	min := camouflage.BatchMinBytes
	if min < maxFramePayload+9 {
		min = maxFramePayload + 9
	}
	if min <= 0 || min >= max {
		return max
	}
	if deterministicTransport {
		return (min + max) / 2
	}
	n, err := randomIntInclusive(min, max)
	if err != nil {
		return max
	}
	return n
}

func padTransportPlaintext(plain []byte) ([]byte, error) {
	target, err := paddedPlaintextSize(len(plain))
	if err != nil {
		return nil, err
	}

	out := make([]byte, target)

	binary.BigEndian.PutUint32(
		out[:4],
		uint32(len(plain)),
	)

	copy(out[4:], plain)

	padding := out[4+len(plain):]

	if len(padding) > 0 {
		if _, err := rand.Read(padding); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func unpadTransportPlaintext(padded []byte) ([]byte, error) {
	if len(padded) < 4 {
		return nil, fmt.Errorf("short padded plaintext")
	}

	n := uint64(
		binary.BigEndian.Uint32(padded[:4]),
	)

	if n > uint64(len(padded)-4) {
		return nil, fmt.Errorf(
			"invalid padded plaintext length",
		)
	}

	return padded[4 : 4+int(n)], nil
}

func loadCamouflageConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var cfg struct {
		Camouflage camouflageConfig `json:"camouflage"`
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	var raw struct {
		Camouflage map[string]json.RawMessage `json:"camouflage"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw.Camouflage["timing"]; ok {
		camouflage.Timing = cfg.Camouflage.Timing
		// Old beta profiles predate the stochastic active-poll fields.  They
		// remain valid; missing fields inherit beta.2 defaults.
		if camouflage.Timing.ActiveMinMS == 0 {
			camouflage.Timing.ActiveMinMS = 120
		}
		if camouflage.Timing.ActiveMaxMS == 0 {
			camouflage.Timing.ActiveMaxMS = 420
		}
		if camouflage.Timing.ActiveBackoffMS == 0 {
			camouflage.Timing.ActiveBackoffMS = 2200
		}
	}
	if _, ok := raw.Camouflage["recovery"]; ok {
		camouflage.Recovery = cfg.Camouflage.Recovery
	}

	if cfg.Camouflage.OpenPath != "" {
		camouflage.OpenPath = cfg.Camouflage.OpenPath
	}
	if cfg.Camouflage.UpPath != "" {
		camouflage.UpPath = cfg.Camouflage.UpPath
	}
	if cfg.Camouflage.DownPath != "" {
		camouflage.DownPath = cfg.Camouflage.DownPath
	}
	if cfg.Camouflage.ClosePath != "" {
		camouflage.ClosePath = cfg.Camouflage.ClosePath
	}
	if cfg.Camouflage.ContentType != "" {
		camouflage.ContentType = cfg.Camouflage.ContentType
	}
	if cfg.Camouflage.RequestHeaders != nil {
		camouflage.RequestHeaders = cfg.Camouflage.RequestHeaders
	}
	if cfg.Camouflage.ResponseHeaders != nil {
		camouflage.ResponseHeaders = cfg.Camouflage.ResponseHeaders
	}
	if cfg.Camouflage.DownMaxBytes > 0 {
		camouflage.DownMaxBytes = cfg.Camouflage.DownMaxBytes
	}
	if cfg.Camouflage.UpMaxBytes > 0 {
		camouflage.UpMaxBytes = cfg.Camouflage.UpMaxBytes
	}
	if len(cfg.Camouflage.PaddingClasses) > 0 {
		camouflage.PaddingClasses = cfg.Camouflage.PaddingClasses
	}
	if _, ok := raw.Camouflage["padding_extra_class_percent"]; ok {
		camouflage.PaddingExtraClassPercent = cfg.Camouflage.PaddingExtraClassPercent
	}
	if _, ok := raw.Camouflage["padding_random_max_bytes"]; ok {
		camouflage.PaddingRandomMaxBytes = cfg.Camouflage.PaddingRandomMaxBytes
	}
	if cfg.Camouflage.BatchMinBytes > 0 {
		camouflage.BatchMinBytes = cfg.Camouflage.BatchMinBytes
	}
	if cfg.Camouflage.NoiseChunkMinBytes > 0 {
		camouflage.NoiseChunkMinBytes = cfg.Camouflage.NoiseChunkMinBytes
	}
	if cfg.Camouflage.NoiseChunkMaxBytes > 0 {
		camouflage.NoiseChunkMaxBytes = cfg.Camouflage.NoiseChunkMaxBytes
	}

	paths := []string{
		camouflage.OpenPath,
		camouflage.UpPath,
		camouflage.DownPath,
		camouflage.ClosePath,
	}

	for _, p := range paths {
		if p == "" || p[0] != '/' {
			return fmt.Errorf("camouflage path must begin with /: %q", p)
		}
		if strings.ContainsAny(p, "?#") {
			return fmt.Errorf("invalid camouflage path: %q", p)
		}
	}

	if camouflage.OpenPath == camouflage.UpPath ||
		camouflage.OpenPath == camouflage.DownPath ||
		camouflage.UpPath == camouflage.DownPath {
		return fmt.Errorf("camouflage paths must be distinct")
	}

	if camouflage.DownMaxBytes < 1024 || camouflage.DownMaxBytes > 4*1024*1024 {
		return fmt.Errorf("down_max_bytes out of range: %d", camouflage.DownMaxBytes)
	}
	if camouflage.UpMaxBytes < maxFramePayload+9 || camouflage.UpMaxBytes > 1024*1024 {
		return fmt.Errorf("up_max_bytes out of range: %d", camouflage.UpMaxBytes)
	}

	if len(camouflage.PaddingClasses) == 0 {
		return fmt.Errorf("padding_classes must not be empty")
	}

	prev := 0
	for _, n := range camouflage.PaddingClasses {
		if n < 64 || n > 4*1024*1024 {
			return fmt.Errorf("invalid padding class: %d", n)
		}
		if n <= prev {
			return fmt.Errorf("padding_classes must be strictly increasing")
		}
		prev = n
	}

	if camouflage.PaddingRandomMaxBytes == 0 && camouflage.UpMaxBytes+4 > camouflage.PaddingClasses[len(camouflage.PaddingClasses)-1] {
		return fmt.Errorf("up_max_bytes exceeds largest padding class")
	}

	if camouflage.Timing.ActiveMinMS < 20 || camouflage.Timing.ActiveMinMS > camouflage.Timing.ActiveMaxMS || camouflage.Timing.ActiveMaxMS > 5000 {
		return fmt.Errorf("invalid active polling range")
	}
	if camouflage.Timing.ActiveBackoffMS < camouflage.Timing.ActiveMaxMS || camouflage.Timing.ActiveBackoffMS > 30000 {
		return fmt.Errorf("invalid active_backoff_max_ms")
	}

	if camouflage.PaddingExtraClassPercent < 0 ||
		camouflage.PaddingExtraClassPercent > 100 {
		return fmt.Errorf(
			"padding_extra_class_percent out of range: %d",
			camouflage.PaddingExtraClassPercent,
		)
	}
	if camouflage.PaddingRandomMaxBytes < 0 || camouflage.PaddingRandomMaxBytes > 256*1024 {
		return fmt.Errorf("padding_random_max_bytes out of range: %d", camouflage.PaddingRandomMaxBytes)
	}
	if camouflage.BatchMinBytes < maxFramePayload+9 || camouflage.BatchMinBytes > camouflage.UpMaxBytes || camouflage.BatchMinBytes > camouflage.DownMaxBytes {
		return fmt.Errorf("batch_min_bytes out of range: %d", camouflage.BatchMinBytes)
	}
	if camouflage.NoiseChunkMinBytes < 1024 || camouflage.NoiseChunkMinBytes > camouflage.NoiseChunkMaxBytes {
		return fmt.Errorf("noise_chunk_min_bytes out of range: %d", camouflage.NoiseChunkMinBytes)
	}
	if camouflage.NoiseChunkMaxBytes > 65519 {
		return fmt.Errorf("noise_chunk_max_bytes exceeds Noise transport limit: %d", camouflage.NoiseChunkMaxBytes)
	}

	if camouflage.Timing.IdleMinMS < 1000 ||
		camouflage.Timing.IdleMaxMS < camouflage.Timing.IdleMinMS {
		return fmt.Errorf(
			"invalid timing idle range: %d..%d ms",
			camouflage.Timing.IdleMinMS,
			camouflage.Timing.IdleMaxMS,
		)
	}

	if camouflage.Timing.WakeMinMS < 0 ||
		camouflage.Timing.WakeMaxMS < camouflage.Timing.WakeMinMS ||
		camouflage.Timing.WakeMaxMS > 5000 {
		return fmt.Errorf(
			"invalid timing wake range: %d..%d ms",
			camouflage.Timing.WakeMinMS,
			camouflage.Timing.WakeMaxMS,
		)
	}

	if camouflage.Timing.ActiveJitterP < 0 ||
		camouflage.Timing.ActiveJitterP > 100 {
		return fmt.Errorf(
			"active_jitter_percent out of range: %d",
			camouflage.Timing.ActiveJitterP,
		)
	}

	if camouflage.Recovery.FailureTimeoutMS < 5000 ||
		camouflage.Recovery.FailureTimeoutMS > 300000 {
		return fmt.Errorf(
			"recovery.failure_timeout_ms out of range: %d",
			camouflage.Recovery.FailureTimeoutMS,
		)
	}
	if camouflage.Recovery.RetryMinMS < 100 ||
		camouflage.Recovery.RetryMaxMS < camouflage.Recovery.RetryMinMS ||
		camouflage.Recovery.RetryMaxMS > 30000 {
		return fmt.Errorf(
			"invalid recovery retry range: %d..%d ms",
			camouflage.Recovery.RetryMinMS,
			camouflage.Recovery.RetryMaxMS,
		)
	}

	return nil
}
