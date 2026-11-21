package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// This deliberately does not call an MBox session endpoint. The existing
// gateway rejects HEAD before forwarding, so this works with beta.4.3 and
// cannot supersede the active HTTPS session. It is NOT origin authentication.
func probeHTTP3(ctx context.Context, client *http.Client, endpoint *url.URL) error {
	dest := *endpoint
	dest.Path, dest.RawPath, dest.RawQuery, dest.Fragment = "/", "", "", ""
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, dest.String(), nil)
	if err != nil {
		return errors.New("QUIC probe request invalid")
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("QUIC probe transport failed")
	}
	defer response.Body.Close()
	if response.ProtoMajor != 3 || response.StatusCode != http.StatusNotFound {
		return errors.New("QUIC probe unexpected gateway response")
	}
	return nil
}
