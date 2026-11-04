package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const maxControlQueryBytes = 512

// validateControlQuery rejects ambiguous query forms before session lookup,
// timestamp refresh, ACK processing or close. A frontend may change RawQuery,
// but must never be able to exploit a different interpretation between layers.
// The existing Android client emits one canonical value per parameter.
func validateControlQuery(r *http.Request, purpose string) error {
	if len(r.URL.RawQuery) > maxControlQueryBytes {
		return fmt.Errorf("control query too large")
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return fmt.Errorf("invalid control query: %w", err)
	}
	allowed := map[string]bool{"cmac": true}
	if purpose == "UP" || purpose == "DOWN" {
		allowed["seq"] = true
		allowed["ack"] = true
		if purpose == "DOWN" {
			allowed["max"] = true
		}
	} else if purpose != "CLOSE" {
		return fmt.Errorf("invalid control purpose")
	}
	for key, value := range values {
		if !allowed[key] || len(value) != 1 || value[0] == "" {
			return fmt.Errorf("unknown, duplicated or empty control parameter")
		}
	}
	if purpose == "UP" || purpose == "DOWN" {
		for _, key := range []string{"seq", "ack"} {
			value := values.Get(key)
			n, err := strconv.ParseUint(value, 10, 64)
			if err != nil || strconv.FormatUint(n, 10) != value {
				return fmt.Errorf("noncanonical %s", key)
			}
		}
		if purpose == "DOWN" && values.Has("max") {
			v := values.Get("max")
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil || strconv.FormatUint(n, 10) != v {
				return fmt.Errorf("noncanonical max")
			}
		}
	}
	return nil
}
