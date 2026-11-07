// mbox-native-quic: optional HTTP/3 transport, NOT a replacement for REG.RU HTTPS.
// This stage carries the existing authenticated MBox HTTP records over native
// QUIC UDP streams. QUIC DATAGRAM is not enabled in this stage.
// On Android, outbound UDP sockets must be explicitly bound via the FD broker.
package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/quic-go/quic-go/http3"
)

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: mbox-native-quic serve|client [flags]")
	}
	switch args[0] {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		bind := fs.String("bind", "127.0.0.1:7443", "QUIC UDP listener (public bind requires explicit configuration)")
		backend := fs.String("origin", "http://127.0.0.1:18449", "loopback MBox origin")
		serverConfig := fs.String("server-config", "/etc/mbox/server.json", "MBox origin config for allowed paths")
		secretFile := fs.String("secret-file", "/etc/mbox/origin-hmac.key", "private HMAC key file")
		cert := fs.String("cert", "", "TLS certificate PEM")
		key := fs.String("key", "", "TLS private key PEM")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *cert == "" || *key == "" {
			return errors.New("--cert and --key are required")
		}
		addr, err := loopbackURL(*backend)
		if err != nil {
			return err
		}
		paths, err := readPaths(*serverConfig)
		if err != nil {
			return err
		}
		secret, err := readSecret(*secretFile)
		if err != nil {
			return err
		}
		pair, err := tls.LoadX509KeyPair(*cert, *key)
		if err != nil {
			return err
		}
		cfg := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}}
		s := &http3.Server{Addr: *bind, TLSConfig: cfg, Handler: makeServerHandler(addr, paths, secret, localHTTPClient()), MaxHeaderBytes: 32 << 10, IdleTimeout: 45 * time.Second}
		log.Printf("MBox QUIC UDP listening on %s (authenticated origin forwarding)", *bind)
		return s.ListenAndServe()
	case "client":
		fs := flag.NewFlagSet("client", flag.ContinueOnError)
		bind := fs.String("bind", "127.0.0.1:18451", "loopback HTTP proxy for MBox client")
		unixListen := fs.String("unix-listen", "", "app-private Unix HTTP socket (Android)")
		protectPath := fs.String("protect-socket", "", "Android protected-UDP socket bridge")
		protectToken := fs.String("protect-token", "", "socket protection session token")
		remote := fs.String("remote", "", "required https://host:port to MBox QUIC VPS")
		ca := fs.String("ca", "", "optional trusted CA / self-signed root PEM")
		serverConfig := fs.String("server-config", "", "MBox server JSON for allowlisted paths")
		pathsFlag := fs.String("paths", "/api/v1/session,/api/v1/events/,/api/v1/sync/,/api/v1/session/", "comma-separated open,up,down,close paths (omit if --server-config used)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *unixListen == "" {
			if err := localListen(*bind); err != nil {
				return err
			}
		}
		remoteURL, err := remoteURL(*remote)
		if err != nil {
			return err
		}
		if runtime.GOOS == "android" {
			if *unixListen == "" || *protectPath == "" || len(*protectToken) < 32 {
				return errors.New("Android QUIC requires Unix HTTP and protected UDP")
			}
			if err := requireNumericRemoteOnAndroid(remoteURL.Host); err != nil {
				return err
			}
		}
		var paths originPaths
		if *serverConfig != "" {
			paths, err = readPaths(*serverConfig)
			if err != nil {
				return err
			}
		} else {
			list := strings.Split(*pathsFlag, ",")
			if len(list) != 4 {
				return errors.New("--paths requires four paths")
			}
			paths = originPaths{list[0], list[1], list[2], list[3]}
			if !strings.HasPrefix(paths.open, "/") || !strings.HasSuffix(paths.up, "/") || !strings.HasSuffix(paths.down, "/") || !strings.HasSuffix(paths.close, "/") {
				return errors.New("invalid --paths")
			}
		}
		c, transport, err := quicHTTPClientWithProtection(*ca, *protectPath, *protectToken)
		if err != nil {
			return err
		}
		defer transport.Close()
		log.Printf("MBox QUIC client on %s -> %s (HTTPS path remains independent)", *bind, remoteURL.Host)
		if *unixListen != "" {
			return serveLocalHTTPUnix(*unixListen, makeClientHandler(remoteURL, paths, c))
		}
		return serveLocalHTTP(*bind, makeClientHandler(remoteURL, paths, c))
	default:
		return fmt.Errorf("unknown mode %q", args[0])
	}
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
