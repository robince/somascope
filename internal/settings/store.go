package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"
)

type Settings struct {
	UserTimezone string           `json:"user_timezone"`
	Providers    []ProviderConfig `json:"providers"`
}

type ProviderConfig struct {
	Provider      string `json:"provider"`
	Configured    bool   `json:"configured"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret,omitempty"`
	RedirectURI   string `json:"redirect_uri"`
	DefaultScopes string `json:"default_scopes"`
	Notes         string `json:"notes"`
}

type Store struct {
	path string
}

type fileSettings struct {
	Version      int                    `json:"version"`
	UserTimezone string                 `json:"user_timezone"`
	Providers    map[string]storedCreds `json:"providers"`
}

type storedCreds struct {
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	RedirectURI   string `json:"redirect_uri"`
	DefaultScopes string `json:"default_scopes"`
	Notes         string `json:"notes"`
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Load() (Settings, error) {
	fileCfg, err := s.loadFile()
	if err != nil {
		return Settings{}, err
	}
	return publicSettings(fileCfg), nil
}

func (s *Store) Update(next Settings) (Settings, error) {
	current, err := s.loadFile()
	if err != nil {
		return Settings{}, err
	}

	current.UserTimezone = next.UserTimezone
	if current.UserTimezone == "" {
		current.UserTimezone = defaultTimezone()
	}

	for _, provider := range defaultProviders() {
		incoming, ok := findProvider(next.Providers, provider.Provider)
		if !ok {
			continue
		}

		stored := current.Providers[provider.Provider]
		stored.ClientID = incoming.ClientID
		if incoming.ClientSecret != "" {
			stored.ClientSecret = incoming.ClientSecret
		}
		stored.RedirectURI = incoming.RedirectURI
		stored.DefaultScopes = incoming.DefaultScopes
		stored.Notes = incoming.Notes
		current.Providers[provider.Provider] = normalizeStored(provider.Provider, stored)
	}

	if err := s.saveFile(current); err != nil {
		return Settings{}, err
	}

	return publicSettings(current), nil
}

func (s *Store) loadFile() (fileSettings, error) {
	defaults := defaultFileSettings()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return fileSettings{}, err
	}

	var parsed fileSettings
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fileSettings{}, err
	}

	if parsed.UserTimezone == "" {
		parsed.UserTimezone = defaults.UserTimezone
	}
	if parsed.Providers == nil {
		parsed.Providers = map[string]storedCreds{}
	}

	for _, provider := range defaultProviders() {
		stored := providerToStored(provider)
		if existing, ok := parsed.Providers[provider.Provider]; ok {
			stored.ClientID = existing.ClientID
			stored.ClientSecret = existing.ClientSecret
			if existing.RedirectURI != "" {
				stored.RedirectURI = existing.RedirectURI
			}
			if existing.DefaultScopes != "" {
				stored.DefaultScopes = existing.DefaultScopes
			}
			if existing.Notes != "" {
				stored.Notes = existing.Notes
			}
		}
		parsed.Providers[provider.Provider] = normalizeStored(provider.Provider, stored)
	}

	return parsed, nil
}

func (s *Store) saveFile(value fileSettings) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(s.path, data, 0o600)
}

func publicSettings(fileCfg fileSettings) Settings {
	out := Settings{
		UserTimezone: fileCfg.UserTimezone,
		Providers:    make([]ProviderConfig, 0, len(defaultProviders())),
	}

	for _, provider := range defaultProviders() {
		stored := fileCfg.Providers[provider.Provider]
		out.Providers = append(out.Providers, ProviderConfig{
			Provider:      provider.Provider,
			Configured:    providerConfigured(stored),
			ClientID:      stored.ClientID,
			ClientSecret:  "",
			RedirectURI:   stored.RedirectURI,
			DefaultScopes: stored.DefaultScopes,
			Notes:         stored.Notes,
		})
	}

	return out
}

func defaultFileSettings() fileSettings {
	providers := map[string]storedCreds{}
	for _, provider := range defaultProviders() {
		providers[provider.Provider] = providerToStored(provider)
	}

	return fileSettings{
		Version:      1,
		UserTimezone: defaultTimezone(),
		Providers:    providers,
	}
}

func defaultProviders() []ProviderConfig {
	return []ProviderConfig{
		{
			Provider:      "fitbit",
			RedirectURI:   "http://127.0.0.1:8080/oauth/fitbit/callback",
			DefaultScopes: "activity heartrate sleep profile",
			Notes:         "Bring your own Fitbit developer app credentials. Secrets stay local on this device.",
		},
		{
			Provider:      "oura",
			RedirectURI:   "http://127.0.0.1:8080/oauth/oura/callback",
			DefaultScopes: "daily heartrate personal email",
			Notes:         "Bring your own Oura developer app credentials. Secrets stay local on this device.",
		},
	}
}

func providerToStored(provider ProviderConfig) storedCreds {
	return storedCreds{
		ClientID:      provider.ClientID,
		ClientSecret:  provider.ClientSecret,
		RedirectURI:   provider.RedirectURI,
		DefaultScopes: provider.DefaultScopes,
		Notes:         provider.Notes,
	}
}

func providerConfigured(stored storedCreds) bool {
	return stored.ClientID != "" && stored.ClientSecret != "" && stored.RedirectURI != ""
}

func normalizeStored(provider string, stored storedCreds) storedCreds {
	for _, defaults := range defaultProviders() {
		if defaults.Provider != provider {
			continue
		}
		if stored.RedirectURI == "" {
			stored.RedirectURI = defaults.RedirectURI
		}
		if stored.DefaultScopes == "" {
			stored.DefaultScopes = defaults.DefaultScopes
		}
		if stored.Notes == "" {
			stored.Notes = defaults.Notes
		}
	}
	return stored
}

func findProvider(providers []ProviderConfig, name string) (ProviderConfig, bool) {
	index := slices.IndexFunc(providers, func(provider ProviderConfig) bool {
		return provider.Provider == name
	})
	if index < 0 {
		return ProviderConfig{}, false
	}
	return providers[index], true
}

func defaultTimezone() string {
	if zone := time.Now().Location().String(); zone != "" {
		return zone
	}
	return "UTC"
}
