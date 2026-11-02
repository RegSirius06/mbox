package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type camouflageConfig struct {
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

	return nil
}

func applyResponseHeaders(w http.ResponseWriter) {
	for k, v := range camouflage.ResponseHeaders {
		w.Header().Set(k, v)
	}
}

func camouflageNotFound(w http.ResponseWriter) {
	applyResponseHeaders(w)
	w.WriteHeader(http.StatusNotFound)
}
