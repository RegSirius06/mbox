package main

import (
	controlmeta "../common/controlmeta"
	wire "../common/wire"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

func (t *tunnel) closeRemote() {
	if t == nil || t.sid == "" || camouflage.ClosePath == "" || t.http == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	closeURL, parseErr := url.Parse(base + camouflage.ClosePath + t.sid)
	if parseErr != nil {
		return
	}
	q := closeURL.Query()
	q.Set("cmac", controlmeta.Tag(t.crypto.controlKey, http.MethodPost, "CLOSE", 0, 0, ""))
	closeURL.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		closeURL.String(),
		nil,
	)
	if err != nil {
		if networkDebug {
			log.Printf("SESSION CLOSE best-effort error=%q", safeErr(err))
		} else {
			log.Printf("SESSION CLOSE best-effort failed")
		}
		return
	}
	for k, v := range camouflage.RequestHeaders {
		req.Header.Set(k, v)
	}

	resp, err := t.http.Do(req)
	if err != nil {
		if networkDebug {
			log.Printf("SESSION CLOSE best-effort error=%q", safeErr(err))
		} else {
			log.Printf("SESSION CLOSE best-effort failed")
		}
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		log.Printf("SESSION CLOSE %s", redactedSessionID(t.sid))
		return
	}
	log.Printf("SESSION CLOSE HTTP %d", resp.StatusCode)
}

func (t *tunnel) requestRaw(method, rawURL string, body []byte, traceOpen bool) ([]byte, int, error) {
	var r io.Reader

	if body != nil {
		r = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, rawURL, r)
	if err != nil {
		return nil, 0, err
	}

	for k, v := range camouflage.RequestHeaders {
		req.Header.Set(k, v)
	}

	if body != nil {
		req.Header.Set("Content-Type", camouflage.ContentType)
	}

	if traceOpen {
		req = withOpenTrace(req)
	}
	resp, err := t.http.Do(req)
	if err != nil {
		if traceOpen {
			log.Printf("NET stage=HTTP code=%s error=%q", bridgeErrorCode(err), safeErr(err))
		}
		return nil, 0, err
	}
	defer resp.Body.Close()

	limit := responseBodyLimit()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		if traceOpen {
			log.Printf("NET stage=HTTP code=ERR_HTTP_BODY error=%q", safeErr(err))
		}
		return nil, resp.StatusCode, err
	}
	if int64(len(data)) > limit {
		if traceOpen {
			log.Printf("NET stage=HTTP code=ERR_HTTP_BODY_TOO_LARGE limit=%d", limit)
		}
		return nil, resp.StatusCode, fmt.Errorf("HTTP response body exceeds %d bytes", limit)
	}
	if traceOpen {
		alpn := "none"
		tlsVersion := "none"
		stack := resp.Header.Get("X-MBox-Network-Stack")
		if stack == "" {
			stack = "go"
		}
		if resp.TLS != nil {
			alpn = emptyAs(resp.TLS.NegotiatedProtocol, "none")
			tlsVersion = tlsVersionName(resp.TLS.Version)
		} else if bridgedALPN := resp.Header.Get("X-MBox-Negotiated-Protocol"); bridgedALPN != "" {
			alpn = bridgedALPN
			tlsVersion = "cronet"
		}
		log.Printf("NET stage=HTTP code=HTTP_OK stack=%s proto=%s status=%d tls=%s alpn=%s", stack, resp.Proto, resp.StatusCode, tlsVersion, alpn)
	}
	return data, resp.StatusCode, nil
}

func (t *tunnel) request(method, url string, body []byte) ([]byte, int, error) {
	wireBody := body

	if body != nil {
		var err error
		wireBody, err = wire.EncodeHTTPEnvelope(body)
		if err != nil {
			return nil, 0, err
		}
	}

	data, status, err := t.requestRaw(method, url, wireBody, false)
	if err != nil {
		return nil, status, err
	}

	// All successful MBox responses carrying a body use the envelope.
	if status == http.StatusOK && len(data) > 0 {
		data, err = wire.DecodeHTTPEnvelope(data)
		if err != nil {
			return nil, status, err
		}
	}

	return data, status, nil
}

func (t *tunnel) requestOpen(method, url string, body []byte) ([]byte, int, error) {
	wireBody := body
	if body != nil {
		var err error
		wireBody, err = wire.EncodeHTTPEnvelope(body)
		if err != nil {
			return nil, 0, err
		}
	}
	data, status, err := t.requestRaw(method, url, wireBody, true)
	if err != nil {
		return nil, status, err
	}
	if status == http.StatusOK && len(data) > 0 {
		data, err = wire.DecodeHTTPEnvelope(data)
		if err != nil {
			return nil, status, err
		}
	}
	return data, status, nil
}
