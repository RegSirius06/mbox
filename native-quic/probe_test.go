package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type probeRoundTripper func(*http.Request) (*http.Response, error)

func (f probeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProbeHTTP3Contract(t *testing.T) {
	endpoint, err := url.Parse("https://127.0.0.1:7443/")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		major, code int
		fail        bool
		ok          bool
	}{
		{"h3-rejection", 3, 404, false, true},
		{"wrong-http", 2, 404, false, false},
		{"wrong-status", 3, 200, false, false},
		{"transport-error", 0, 0, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: probeRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodHead || r.URL.String() != endpoint.String() || r.Body != nil || len(r.Header) != 0 {
					t.Fatalf("probe sent session data or unexpected request: %v", r.URL)
				}
				if test.fail {
					return nil, errors.New("transport failed")
				}
				return &http.Response{StatusCode: test.code, ProtoMajor: test.major, Body: io.NopCloser(strings.NewReader(""))}, nil
			})}
			if err := probeHTTP3(context.Background(), client, endpoint); (err == nil) != test.ok {
				t.Fatalf("probe result = %v, want success %t", err, test.ok)
			}
		})
	}
}
