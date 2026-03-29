package settings

import (
	"context"
	"errors"
	"os"
	"slices"
	"time"

	appstore "github.com/robince/somascope/internal/store"
)

const userTimezoneKey = "settings:user_timezone"
const legacyOuraDefaultScopes = "email personal daily heartrate tag workout session spo2"

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
	app *appstore.Store
}

func NewStore(app *appstore.Store) *Store {
	return &Store{app: app}
}

func (s *Store) Provider(name string) (ProviderConfig, error) {
	if s.app == nil {
		return ProviderConfig{}, errors.New("settings store requires sqlite app store")
	}

	defaults, ok := findDefaultProvider(name)
	if !ok {
		return ProviderConfig{}, os.ErrNotExist
	}

	credential, err := s.app.ProviderCredentialByProvider(context.Background(), name)
	if errors.Is(err, appstore.ErrNotFound) {
		return defaults, nil
	}
	if err != nil {
		return ProviderConfig{}, err
	}

	credential = normalizeCredential(name, credential)
	out := defaults
	out.Configured = providerConfigured(credential)
	out.ClientID = credential.ClientID
	out.ClientSecret = credential.ClientSecret
	out.RedirectURI = credential.RedirectURI
	out.DefaultScopes = credential.DefaultScopes
	out.Notes = credential.Notes
	return out, nil
}

func (s *Store) Load() (Settings, error) {
	if s.app == nil {
		return Settings{}, errors.New("settings store requires sqlite app store")
	}

	ctx := context.Background()
	userTimezone, err := s.app.AppSetting(ctx, userTimezoneKey)
	if errors.Is(err, appstore.ErrNotFound) || userTimezone == "" {
		userTimezone = defaultTimezone()
	} else if err != nil {
		return Settings{}, err
	}

	credentials, err := s.app.ProviderCredentials(ctx)
	if err != nil {
		return Settings{}, err
	}

	byProvider := map[string]appstore.ProviderCredential{}
	for _, credential := range credentials {
		byProvider[credential.Provider] = credential
	}

	out := Settings{
		UserTimezone: userTimezone,
		Providers:    make([]ProviderConfig, 0, len(defaultProviders())),
	}
	for _, defaults := range defaultProviders() {
		credential, ok := byProvider[defaults.Provider]
		if !ok {
			out.Providers = append(out.Providers, defaults)
			continue
		}

		credential = normalizeCredential(defaults.Provider, credential)
		provider := defaults
		provider.Configured = providerConfigured(credential)
		provider.ClientID = credential.ClientID
		provider.ClientSecret = ""
		provider.RedirectURI = credential.RedirectURI
		provider.DefaultScopes = credential.DefaultScopes
		provider.Notes = credential.Notes
		out.Providers = append(out.Providers, provider)
	}

	return out, nil
}

func (s *Store) Update(next Settings) (Settings, error) {
	if s.app == nil {
		return Settings{}, errors.New("settings store requires sqlite app store")
	}

	ctx := context.Background()
	userTimezone := next.UserTimezone
	if userTimezone == "" {
		userTimezone = defaultTimezone()
	}
	if err := s.app.SetAppSetting(ctx, userTimezoneKey, userTimezone); err != nil {
		return Settings{}, err
	}

	for _, defaults := range defaultProviders() {
		incoming, ok := findProvider(next.Providers, defaults.Provider)
		if !ok {
			continue
		}

		current, err := s.app.ProviderCredentialByProvider(ctx, defaults.Provider)
		if errors.Is(err, appstore.ErrNotFound) {
			current = providerToCredential(defaults)
		} else if err != nil {
			return Settings{}, err
		}

		current.ClientID = incoming.ClientID
		if incoming.ClientSecret != "" {
			current.ClientSecret = incoming.ClientSecret
		}
		current.RedirectURI = incoming.RedirectURI
		current.DefaultScopes = incoming.DefaultScopes
		current.Notes = incoming.Notes
		current = normalizeCredential(defaults.Provider, current)

		if err := s.app.UpsertProviderCredential(ctx, current); err != nil {
			return Settings{}, err
		}
	}

	return s.Load()
}

func defaultProviders() []ProviderConfig {
	return []ProviderConfig{
		{
			Provider:      "fitbit",
			RedirectURI:   "http://localhost:18080/oauth/fitbit/callback",
			DefaultScopes: "activity heartrate sleep profile",
			Notes:         "Bring your own Fitbit developer app credentials. Secrets stay local on this device.",
		},
		{
			Provider:      "oura",
			RedirectURI:   "http://localhost:18080/oauth/oura/callback",
			DefaultScopes: "",
			Notes:         "Bring your own Oura developer app credentials. Secrets stay local on this device.",
		},
	}
}

func findDefaultProvider(name string) (ProviderConfig, bool) {
	return findProvider(defaultProviders(), name)
}

func providerToCredential(provider ProviderConfig) appstore.ProviderCredential {
	return appstore.ProviderCredential{
		Provider:      provider.Provider,
		ClientID:      provider.ClientID,
		ClientSecret:  provider.ClientSecret,
		RedirectURI:   provider.RedirectURI,
		DefaultScopes: provider.DefaultScopes,
		Notes:         provider.Notes,
	}
}

func providerConfigured(stored appstore.ProviderCredential) bool {
	return stored.ClientID != "" && stored.ClientSecret != "" && stored.RedirectURI != ""
}

func normalizeCredential(provider string, stored appstore.ProviderCredential) appstore.ProviderCredential {
	for _, defaults := range defaultProviders() {
		if defaults.Provider != provider {
			continue
		}
		if legacy := legacyDefaultRedirectURI(provider); stored.RedirectURI == legacy {
			stored.RedirectURI = defaults.RedirectURI
		}
		if stored.RedirectURI == "" {
			stored.RedirectURI = defaults.RedirectURI
		}
		if stored.DefaultScopes == legacyOuraDefaultScopes {
			stored.DefaultScopes = defaults.DefaultScopes
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

func legacyDefaultRedirectURI(provider string) string {
	switch provider {
	case "fitbit":
		return "http://127.0.0.1:18080/oauth/fitbit/callback"
	case "oura":
		return "http://127.0.0.1:18080/oauth/oura/callback"
	default:
		return ""
	}
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
