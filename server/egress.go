package main

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// The named exits terminate at distinct loopback SOCKS5 listeners on the RU VPS.
// The caller cannot choose an arbitrary destination or DNS name.
type egressEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Upstream string `json:"upstream"`
}

var egressIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,15}$`)
var egressConfigFile = "/etc/mbox/egress.json"

func validEgressID(id string) bool { return egressIDPattern.MatchString(id) }

func validateEgressAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return errors.New("invalid SOCKS5 destination")
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return errors.New("invalid SOCKS5 port")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("egress SOCKS5 destination must be loopback IP")
	}
	_, err = net.ResolveTCPAddr("tcp", addr)
	return err
}

func resolveSessionUpstream(route string) (string, error) {
	if route == "" {
		return socksAddr, nil
	} // Old beta.4.3 clients preserve their route.
	if !validEgressID(route) {
		return "", errors.New("invalid egress ID")
	}
	info, err := os.Lstat(egressConfigFile)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
		return "", errors.New("egress routes unavailable")
	}
	data, err := os.ReadFile(egressConfigFile)
	if err != nil {
		return "", errors.New("egress routes unavailable")
	}
	var file struct {
		Exits []egressEntry `json:"exits"`
	}
	if err = json.Unmarshal(data, &file); err != nil || len(file.Exits) > 12 {
		return "", errors.New("invalid egress routes")
	}
	found := ""
	seen := map[string]bool{}
	for _, entry := range file.Exits {
		if !validEgressID(entry.ID) || seen[entry.ID] || len(entry.Name) < 1 || len(entry.Name) > 48 || validateEgressAddr(entry.Upstream) != nil {
			return "", errors.New("invalid egress routes")
		}
		seen[entry.ID] = true
		if entry.ID == route {
			found = entry.Upstream
		}
	}
	if found == "" {
		return "", errors.New("requested egress is not configured")
	}
	return found, nil
}

func setEgressConfigDirectory(runtimePath string) {
	egressConfigFile = filepath.Join(filepath.Dir(runtimePath), "egress.json")
}
