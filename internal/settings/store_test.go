package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUpgradesLegacyLoopbackRedirectDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	data := `{
  "version": 1,
  "user_timezone": "Europe/London",
  "providers": {
    "oura": {
      "client_id": "oura-client",
      "client_secret": "oura-secret",
      "redirect_uri": "http://127.0.0.1:18080/oauth/oura/callback",
      "default_scopes": "email personal daily",
      "notes": "local"
    }
  }
}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store := NewStore(path)
	value, err := store.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}

	var oura ProviderConfig
	for _, provider := range value.Providers {
		if provider.Provider == "oura" {
			oura = provider
			break
		}
	}

	if got, want := oura.RedirectURI, "http://localhost:18080/oauth/oura/callback"; got != want {
		t.Fatalf("expected upgraded redirect_uri %q, got %q", want, got)
	}
}

func TestLoadPreservesCustomRedirectURI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	data := `{
  "version": 1,
  "user_timezone": "Europe/London",
  "providers": {
    "oura": {
      "client_id": "oura-client",
      "client_secret": "oura-secret",
      "redirect_uri": "http://devbox.local:18080/oauth/oura/callback",
      "default_scopes": "email personal daily",
      "notes": "local"
    }
  }
}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store := NewStore(path)
	value, err := store.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}

	var oura ProviderConfig
	for _, provider := range value.Providers {
		if provider.Provider == "oura" {
			oura = provider
			break
		}
	}

	if got, want := oura.RedirectURI, "http://devbox.local:18080/oauth/oura/callback"; got != want {
		t.Fatalf("expected custom redirect_uri %q, got %q", want, got)
	}
}
