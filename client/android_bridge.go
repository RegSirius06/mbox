package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	androidBridgeRequestMagic   uint32 = 0x4d425231 // MBR1
	androidBridgeResponseMagic  uint32 = 0x4d425331 // MBS1
	androidBridgeMaxBody               = 8 * 1024 * 1024
	androidBridgeMaxHeaders            = 64
	androidBridgeMaxHeaderName         = 128
	androidBridgeMaxHeaderValue        = 4096
)

type androidBridgeError struct {
	Code string
	Msg  string
}

func (e *androidBridgeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Msg == "" {
		return e.Code
	}
	return e.Code + ": " + e.Msg
}

type androidBridgeTransport struct {
	addr   string
	token  string
	origin *url.URL
}

func androidBridgeFromEnv(origin *url.URL) (http.RoundTripper, bool, error) {
	addr := strings.TrimSpace(os.Getenv("MBOX_HTTP_BRIDGE_ADDR"))
	if addr == "" {
		return nil, false, nil
	}
	token := strings.TrimSpace(os.Getenv("MBOX_HTTP_BRIDGE_TOKEN"))
	if token == "" {
		return nil, false, fmt.Errorf("MBOX_HTTP_BRIDGE_TOKEN is required when MBOX_HTTP_BRIDGE_ADDR is set")
	}
	if len(token) > 512 {
		return nil, false, fmt.Errorf("MBOX_HTTP_BRIDGE_TOKEN is too long")
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return nil, false, fmt.Errorf("invalid MBOX_HTTP_BRIDGE_ADDR %q: %w", addr, err)
	}
	if origin == nil || origin.Scheme != "https" || origin.Host == "" {
		return nil, false, fmt.Errorf("Android Cronet bridge requires an HTTPS origin")
	}
	return &androidBridgeTransport{addr: addr, token: token, origin: origin}, true, nil
}

func (t *androidBridgeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, errors.New("nil HTTP request")
	}
	if !sameOrigin(t.origin, req.URL) {
		return nil, fmt.Errorf("Android bridge refuses cross-origin request to %s", req.URL.Host)
	}

	body, err := readBridgeRequestBody(req.Body)
	if err != nil {
		return nil, err
	}

	headers := make([][2]string, 0, len(req.Header))
	for name, values := range req.Header {
		if bridgeHopHeader(name) {
			continue
		}
		if len(name) == 0 || len(name) > androidBridgeMaxHeaderName {
			return nil, fmt.Errorf("bridge header name too long")
		}
		for _, value := range values {
			if len(value) > androidBridgeMaxHeaderValue {
				return nil, fmt.Errorf("bridge header value too long")
			}
			headers = append(headers, [2]string{name, value})
			if len(headers) > androidBridgeMaxHeaders {
				return nil, fmt.Errorf("too many bridge request headers")
			}
		}
	}

	dialer := &net.Dialer{Timeout: 2 * time.Second, KeepAlive: -1}
	conn, err := dialer.DialContext(req.Context(), "tcp", t.addr)
	if err != nil {
		return nil, &androidBridgeError{Code: "ERR_ANDROID_BRIDGE", Msg: err.Error()}
	}
	defer conn.Close()
	if deadline, ok := req.Context().Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	}

	bw := bufio.NewWriter(conn)
	if err := binary.Write(bw, binary.BigEndian, androidBridgeRequestMagic); err != nil {
		return nil, err
	}
	if err := bridgeWriteString16(bw, t.token); err != nil {
		return nil, err
	}
	if err := bridgeWriteString16(bw, req.Method); err != nil {
		return nil, err
	}
	requestURI := req.URL.EscapedPath()
	if requestURI == "" {
		requestURI = "/"
	}
	if req.URL.RawQuery != "" {
		requestURI += "?" + req.URL.RawQuery
	}
	if err := bridgeWriteString32(bw, requestURI); err != nil {
		return nil, err
	}
	if err := binary.Write(bw, binary.BigEndian, uint16(len(headers))); err != nil {
		return nil, err
	}
	for _, h := range headers {
		if err := bridgeWriteString16(bw, h[0]); err != nil {
			return nil, err
		}
		if err := bridgeWriteString16(bw, h[1]); err != nil {
			return nil, err
		}
	}
	if err := binary.Write(bw, binary.BigEndian, uint32(len(body))); err != nil {
		return nil, err
	}
	if _, err := bw.Write(body); err != nil {
		return nil, err
	}
	if err := bw.Flush(); err != nil {
		return nil, err
	}

	br := bufio.NewReader(conn)
	var magic uint32
	if err := binary.Read(br, binary.BigEndian, &magic); err != nil {
		return nil, &androidBridgeError{Code: "ERR_ANDROID_BRIDGE", Msg: err.Error()}
	}
	if magic != androidBridgeResponseMagic {
		return nil, &androidBridgeError{Code: "ERR_ANDROID_BRIDGE_PROTOCOL", Msg: "bad response magic"}
	}
	var status uint16
	if err := binary.Read(br, binary.BigEndian, &status); err != nil {
		return nil, err
	}
	errCode, err := bridgeReadString16(br, 128)
	if err != nil {
		return nil, err
	}
	errMsg, err := bridgeReadString16(br, 1024)
	if err != nil {
		return nil, err
	}
	proto, err := bridgeReadString16(br, 128)
	if err != nil {
		return nil, err
	}
	var bodyLen uint32
	if err := binary.Read(br, binary.BigEndian, &bodyLen); err != nil {
		return nil, err
	}
	if bodyLen > androidBridgeMaxBody {
		return nil, &androidBridgeError{Code: "ERR_HTTP_BODY_TOO_LARGE", Msg: strconv.FormatUint(uint64(bodyLen), 10)}
	}
	responseBody := make([]byte, int(bodyLen))
	if _, err := io.ReadFull(br, responseBody); err != nil {
		return nil, &androidBridgeError{Code: "ERR_HTTP_BODY", Msg: "truncated bridge response"}
	}
	if errCode != "" {
		return nil, &androidBridgeError{Code: errCode, Msg: errMsg}
	}
	if status < 100 || status > 599 {
		return nil, &androidBridgeError{Code: "ERR_ANDROID_BRIDGE_PROTOCOL", Msg: "invalid HTTP status"}
	}

	major, minor, protoString := bridgeHTTPProto(proto)
	header := make(http.Header)
	header.Set("X-MBox-Network-Stack", "cronet")
	header.Set("X-MBox-Negotiated-Protocol", proto)
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(int(status))),
		StatusCode:    int(status),
		Proto:         protoString,
		ProtoMajor:    major,
		ProtoMinor:    minor,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(responseBody)),
		ContentLength: int64(len(responseBody)),
		Request:       req,
	}, nil
}

func sameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func bridgeHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "host", "connection", "proxy-connection", "content-length", "transfer-encoding", "upgrade", "user-agent", "accept-encoding":
		return true
	default:
		return false
	}
}

func readBridgeRequestBody(r io.ReadCloser) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, androidBridgeMaxBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > androidBridgeMaxBody {
		return nil, fmt.Errorf("bridge request body exceeds %d bytes", androidBridgeMaxBody)
	}
	return data, nil
}

func bridgeWriteString16(w io.Writer, s string) error {
	if len(s) > 0xffff {
		return fmt.Errorf("bridge string too long")
	}
	if err := binary.Write(w, binary.BigEndian, uint16(len(s))); err != nil {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}

func bridgeWriteString32(w io.Writer, s string) error {
	if len(s) > androidBridgeMaxBody {
		return fmt.Errorf("bridge string too long")
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(s))); err != nil {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}

func bridgeReadString16(r io.Reader, max int) (string, error) {
	var n uint16
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return "", err
	}
	if int(n) > max {
		return "", fmt.Errorf("bridge string exceeds limit")
	}
	b := make([]byte, int(n))
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

func bridgeHTTPProto(proto string) (int, int, string) {
	p := strings.ToLower(proto)
	switch {
	case p == "h2" || strings.Contains(p, "spdy"):
		return 2, 0, "HTTP/2.0"
	case strings.HasPrefix(p, "h3") || strings.Contains(p, "quic"):
		return 3, 0, "HTTP/3.0"
	default:
		return 1, 1, "HTTP/1.1"
	}
}

func bridgeErrorCode(err error) string {
	var be *androidBridgeError
	if errors.As(err, &be) && be.Code != "" {
		return be.Code
	}
	return "ERR_HTTP_REQUEST"
}
