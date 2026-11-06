package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
)

const maxRequestBytes = 1 << 20

var ErrMissingBearerToken = errors.New("missing or invalid bearer token")

func DecodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}

// ClientIP returns the caller's IP. Forwarded headers are only trusted when
// the deployment explicitly opts in, because clients can spoof them otherwise.
func ClientIP(r *http.Request, trustProxy bool) net.IP {
	if trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first, _, _ := strings.Cut(forwarded, ",")
			if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
				return ip
			}
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			if ip := net.ParseIP(realIP); ip != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

func BearerToken(r *http.Request) (string, error) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", ErrMissingBearerToken
	}
	return parts[1], nil
}
