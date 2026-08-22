package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

func oauthState() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func writeOAuthHTML(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>%s</title><style>body{font-family:ui-sans-serif,system-ui,sans-serif;background:#f4f0e6;color:#203025;padding:40px}main{max-width:640px;margin:0 auto;border:1px solid #d8d1bf;border-radius:18px;padding:24px;background:#fffdf7}h1{margin:0 0 12px;font-size:1.5rem}p{line-height:1.6;color:#455348}</style></head><body><main><h1>%s</h1><p>%s</p></main></body></html>`,
		template.HTMLEscapeString(title),
		template.HTMLEscapeString(title),
		body,
	)
}

func appRootFromRedirect(redirectURI string) string {
	parsed, err := url.Parse(strings.TrimSpace(redirectURI))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Path = "/"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func firstValidReturnTo(values ...string) string {
	for _, value := range values {
		if validated := validateLocalReturnTo(value); validated != "" {
			return validated
		}
	}
	return ""
}

func validateLocalReturnTo(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "http" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "127.0.0.1" {
		return ""
	}
	return parsed.String()
}

func addQueryValues(rawURL string, values map[string]string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	for key, value := range values {
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
