package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"
)

func readPrivate(path string) (*ecdh.PrivateKey, error) {
	rawText, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(rawText)))
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("%s must contain a 32-byte hex X25519 private key", path)
	}
	return ecdh.X25519().NewPrivateKey(raw)
}

func writeKey(path string, raw []byte, mode os.FileMode) error {
	return os.WriteFile(path, []byte(hex.EncodeToString(raw)+"\n"), mode)
}

func main() {
	mode := flag.String("mode", "generate", "generate or derive")
	privatePath := flag.String("private", "", "private key path")
	publicPath := flag.String("public", "", "public key path")
	flag.Parse()

	if *privatePath == "" || *publicPath == "" {
		fmt.Fprintln(os.Stderr, "-private and -public are required")
		os.Exit(2)
	}

	var privateKey *ecdh.PrivateKey
	var err error

	switch *mode {
	case "generate":
		privateKey, err = ecdh.X25519().GenerateKey(rand.Reader)
		if err == nil {
			err = writeKey(*privatePath, privateKey.Bytes(), 0600)
		}
	case "derive":
		privateKey, err = readPrivate(*privatePath)
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}

	if err == nil {
		err = writeKey(*publicPath, privateKey.PublicKey().Bytes(), 0644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
