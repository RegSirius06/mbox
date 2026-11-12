package main

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Transport struct {
	URL       string `json:"url"`
	QUICURL   string `json:"quic_url,omitempty"`
	QUICCAPEM string `json:"quic_ca_pem,omitempty"`
}

type LinkCrypto struct {
	PSK             string `json:"psk"`
	ServerPublicKey string `json:"server_public_key"`
}

type LinkConfig struct {
	Version    int             `json:"v"`
	Name       string          `json:"name,omitempty"`
	ClientID   string          `json:"client_id,omitempty"`
	Transport  Transport       `json:"transport"`
	Crypto     LinkCrypto      `json:"crypto"`
	Camouflage json.RawMessage `json:"camouflage"`
}

type DiskTransport struct {
	Listen    string `json:"listen"`
	URL       string `json:"url"`
	QUICURL   string `json:"quic_url,omitempty"`
	QUICCAPEM string `json:"quic_ca_pem,omitempty"`
}

type DiskCrypto struct {
	ClientID            string `json:"client_id,omitempty"`
	PSKFile             string `json:"psk_file"`
	ServerPublicKeyFile string `json:"server_public_key_file"`
}

type DiskConfig struct {
	Camouflage json.RawMessage `json:"camouflage"`
	Transport  DiskTransport   `json:"transport"`
	Crypto     DiskCrypto      `json:"crypto"`
}

func readHex32(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	s := strings.TrimSpace(string(b))

	raw, err := hex.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("%s: invalid hex: %w", path, err)
	}

	if len(raw) != 32 {
		return "", fmt.Errorf("%s: expected 32 bytes, got %d", path, len(raw))
	}

	return strings.ToLower(s), nil
}

func validClientID(id string) bool {
	if id == "" {
		return true
	}
	if len(id) > 63 {
		return false
	}
	for i := 0; i < len(id); i++ {
		ch := id[i]
		if !((ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '.' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func validBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("invalid transport.url")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("transport.url must not contain userinfo, query, or fragment")
	}
	if strings.HasSuffix(raw, "/") {
		return errors.New("transport.url must not end with /")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		if host == "127.0.0.1" || host == "::1" || strings.EqualFold(host, "localhost") {
			return nil
		}
	}
	return errors.New("transport.url must use https (http is allowed only for loopback tests)")
}

func validateLink(c *LinkConfig) error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported MBox link version: %d", c.Version)
	}

	if !validClientID(c.ClientID) {
		return errors.New("invalid client_id")
	}

	if c.Transport.URL == "" {
		return errors.New("missing transport.url")
	}

	if err := validBaseURL(c.Transport.URL); err != nil {
		return err
	}

	if (c.Transport.QUICURL == "") != (c.Transport.QUICCAPEM == "") {
		return errors.New("quic_url and quic_ca_pem must appear together")
	}
	if c.Transport.QUICURL != "" {
		u, err := url.Parse(c.Transport.QUICURL)
		if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
			(u.Path != "" && u.Path != "/") || u.Port() == "" || net.ParseIP(u.Hostname()) == nil ||
			net.ParseIP(u.Hostname()).To4() == nil {
			return errors.New("QUIC URL must be https://numeric-IPv4:port")
		}
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return errors.New("QUIC port out of range")
		}
		if len(c.Transport.QUICCAPEM) > 16384 {
			return errors.New("QUIC certificate PEM too large")
		}
		block, _ := pem.Decode([]byte(c.Transport.QUICCAPEM))
		if block == nil || block.Type != "CERTIFICATE" {
			return errors.New("invalid QUIC certificate PEM")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
			return errors.New("invalid or expired QUIC certificate")
		}
		if err := cert.VerifyHostname(u.Hostname()); err != nil {
			return errors.New("QUIC certificate does not match endpoint IP")
		}
	}

	for name, value := range map[string]string{
		"psk":               c.Crypto.PSK,
		"server_public_key": c.Crypto.ServerPublicKey,
	} {
		raw, err := hex.DecodeString(value)
		if err != nil || len(raw) != 32 {
			return fmt.Errorf("%s must be 32-byte hex", name)
		}
	}

	if len(c.Camouflage) == 0 {
		return errors.New("missing camouflage profile")
	}

	return nil
}

func encodeLink(c LinkConfig) (string, error) {
	if err := validateLink(&c); err != nil {
		return "", err
	}

	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}

	payload := base64.RawURLEncoding.EncodeToString(raw)

	link := "mbox://" + payload

	if c.Name != "" {
		link += "#" + url.QueryEscape(c.Name)
	}

	return link, nil
}

func decodeLink(s string) (*LinkConfig, error) {
	s = strings.TrimSpace(s)

	if !strings.HasPrefix(s, "mbox://") {
		return nil, errors.New("not an mbox:// link")
	}

	s = strings.TrimPrefix(s, "mbox://")

	var fragment string
	if i := strings.IndexByte(s, '#'); i >= 0 {
		fragment = s[i+1:]
		s = s[:i]
	}

	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid base64url payload: %w", err)
	}

	var c LinkConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("invalid link JSON: %w", err)
	}

	if c.Name == "" && fragment != "" {
		if x, err := url.QueryUnescape(fragment); err == nil {
			c.Name = x
		}
	}

	if err := validateLink(&c); err != nil {
		return nil, err
	}

	return &c, nil
}

func doExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)

	configPath := fs.String("config", "", "client.json")
	pskPath := fs.String("psk", "", "PSK file")
	pubPath := fs.String("pub", "", "server public key file")
	name := fs.String("name", "MBox", "profile name")

	fs.Parse(args)

	if *configPath == "" || *pskPath == "" || *pubPath == "" {
		return errors.New("export requires -config, -psk and -pub")
	}

	raw, err := os.ReadFile(*configPath)
	if err != nil {
		return err
	}

	var disk DiskConfig
	if err := json.Unmarshal(raw, &disk); err != nil {
		return err
	}

	psk, err := readHex32(*pskPath)
	if err != nil {
		return err
	}

	pub, err := readHex32(*pubPath)
	if err != nil {
		return err
	}

	c := LinkConfig{
		Version:  1,
		Name:     *name,
		ClientID: disk.Crypto.ClientID,
		Transport: Transport{
			URL:       disk.Transport.URL,
			QUICURL:   disk.Transport.QUICURL,
			QUICCAPEM: disk.Transport.QUICCAPEM,
		},
		Crypto: LinkCrypto{
			PSK:             psk,
			ServerPublicKey: pub,
		},
		Camouflage: disk.Camouflage,
	}

	link, err := encodeLink(c)
	if err != nil {
		return err
	}

	fmt.Println(link)
	return nil
}

func doImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)

	linkArg := fs.String("link", "", "mbox:// link; if omitted read stdin")
	outDir := fs.String("out", "", "output directory")
	listen := fs.String("listen", "127.0.0.1:10890", "local SOCKS listen")

	fs.Parse(args)

	if *outDir == "" {
		return errors.New("import requires -out")
	}

	link := *linkArg
	if link == "" {
		b, err := os.ReadFile("/dev/stdin")
		if err != nil {
			return err
		}
		link = strings.TrimSpace(string(b))
	}

	c, err := decodeLink(link)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(*outDir, 0700); err != nil {
		return err
	}

	pskPath := filepath.Join(*outDir, "mbox.psk")
	pubPath := filepath.Join(*outDir, "mbox-server.pub")
	cfgPath := filepath.Join(*outDir, "client.json")

	if err := os.WriteFile(pskPath, []byte(c.Crypto.PSK+"\n"), 0600); err != nil {
		return err
	}

	if err := os.WriteFile(pubPath, []byte(c.Crypto.ServerPublicKey+"\n"), 0600); err != nil {
		return err
	}

	disk := DiskConfig{
		Camouflage: c.Camouflage,
		Transport: DiskTransport{
			Listen:    *listen,
			URL:       c.Transport.URL,
			QUICURL:   c.Transport.QUICURL,
			QUICCAPEM: c.Transport.QUICCAPEM,
		},
		Crypto: DiskCrypto{
			ClientID:            c.ClientID,
			PSKFile:             pskPath,
			ServerPublicKeyFile: pubPath,
		},
	}

	raw, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	if err := os.WriteFile(cfgPath, raw, 0600); err != nil {
		return err
	}

	fmt.Printf("name=%s\n", c.Name)
	fmt.Printf("config=%s\n", cfgPath)
	fmt.Printf("listen=%s\n", *listen)

	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mbox-link <export|import> ...")
		os.Exit(2)
	}

	var err error

	switch os.Args[1] {
	case "export":
		err = doExport(os.Args[2:])
	case "import":
		err = doImport(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
