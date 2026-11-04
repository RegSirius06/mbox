package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestControlQueryStrictCanonicalParsing(t *testing.T) {
	cases := []struct {
		purpose, query string
		ok             bool
	}{
		{"DOWN", "seq=1&ack=0&max=4096&cmac=dummy", true},
		{"DOWN", "seq=1&ack=0", true}, // compatibility switch is checked after parsing
		{"UP", "seq=1&ack=0&cmac=dummy", true},
		{"CLOSE", "cmac=dummy", true},
		{"CLOSE", "", true}, // rejected later in strict mode
		{"UP", "seq=1&ack=0&max=4096", false},
		{"DOWN", "seq=1&ack=0&seq=2", false},
		{"DOWN", "%73eq=1&ack=0&seq=1", false},
		{"DOWN", "%73eq=1&ack=0", false},
		{"DOWN", "seq=%31&ack=0", false},
		{"DOWN", "seq=1&ack=0&cmac=x&cmac=y", false},
		{"DOWN", "seq=1&ack=0&max=", false},
		{"DOWN", "seq=1&ack=0&max=garbage", false},
		{"DOWN", "seq=1&ack=0&max=000123", false},
		{"CLOSE", "cmac=x&ack=0", false},
		{"CLOSE", "cmac=x&cmac=y", false},
		{"DOWN", "seq=01&ack=0", false},
		{"DOWN", "seq=1&ack=+0", false},
		{"DOWN", "seq=1&ack=-1", false},
		{"DOWN", "seq=1&ack=0&unused=0", false},
		{"DOWN", "seq=1&ack=0&invalid=%xx", false},
		{"DOWN", "seq=1&ack=0&cmac=" + strings.Repeat("x", 513), false},
	}
	for _, c := range cases {
		r := &http.Request{Method: http.MethodGet, URL: &url.URL{RawQuery: c.query}}
		err := validateControlQuery(r, c.purpose)
		if (err == nil) != c.ok {
			t.Errorf("purpose=%s query=%q ok=%t err=%v", c.purpose, c.query, c.ok, err)
		}
	}
}

func FuzzControlQueryParser(f *testing.F) {
	for _, seed := range []string{"seq=1&ack=0&cmac=abc", "seq=1&seq=2&ack=0", "seq=%00&ack=0", "%252f", "seq=1&ack=0&max=4096"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, query string) {
		if len(query) > 1024 {
			t.Skip()
		}
		r := &http.Request{Method: http.MethodGet, URL: &url.URL{RawQuery: query}}
		_ = validateControlQuery(r, "DOWN")
		_ = validateControlQuery(r, "UP")
		_ = validateControlQuery(r, "CLOSE")
	})
}
