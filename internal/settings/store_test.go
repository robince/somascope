package settings

import (
	"context"
	"path/filepath"
	"testing"

	appstore "github.com/robince/somascope/internal/store"
)

func TestLoadReturnsSQLiteDefaultsWhenUnset(t *testing.T) {
	app, err := appstore.Open(context.Background(), filepath.Join(t.TempDir(), "somascope.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer app.Close()

	store := NewStore(app)
	value, err := store.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}

	if value.UserTimezone == "" {
		t.Fatalf("expected default timezone")
	}
	if len(value.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(value.Providers))
	}

	var oura ProviderConfig
	for _, provider := range value.Providers {
		if provider.Provider == "oura" {
			oura = provider
			break
		}
	}

	if oura.RedirectURI != "http://localhost:18080/oauth/oura/callback" {
		t.Fatalf("unexpected Oura default redirect URI: %q", oura.RedirectURI)
	}
	if oura.DefaultScopes != "" {
		t.Fatalf("expected empty Oura default scopes, got %q", oura.DefaultScopes)
	}
	if oura.Configured {
		t.Fatalf("expected default Oura provider to be unconfigured")
	}
}

func TestUpdatePersistsProviderCredentialsInSQLite(t *testing.T) {
	app, err := appstore.Open(context.Background(), filepath.Join(t.TempDir(), "somascope.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer app.Close()

	store := NewStore(app)
	updated, err := store.Update(Settings{
		UserTimezone: "Europe/London",
		Providers: []ProviderConfig{
			{
				Provider:      "google_health",
				ClientID:      "",
				ClientSecret:  "",
				RedirectURI:   "http://localhost:18080/oauth/google_health/callback",
				DefaultScopes: "https://www.googleapis.com/auth/googlehealth.activity_and_fitness.readonly",
				Notes:         "Google Health notes",
			},
			{
				Provider:      "oura",
				ClientID:      "oura-client",
				ClientSecret:  "oura-secret",
				RedirectURI:   "http://localhost:18080/oauth/oura/callback",
				DefaultScopes: "",
				Notes:         "Oura notes",
			},
		},
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}

	if updated.UserTimezone != "Europe/London" {
		t.Fatalf("expected saved timezone, got %q", updated.UserTimezone)
	}

	var publicOura ProviderConfig
	for _, provider := range updated.Providers {
		if provider.Provider == "oura" {
			publicOura = provider
			break
		}
	}
	if !publicOura.Configured {
		t.Fatalf("expected Oura to be configured after update")
	}
	if publicOura.ClientSecret != "" {
		t.Fatalf("expected public settings response to hide client secret")
	}
	var publicGoogleHealth ProviderConfig
	for _, provider := range updated.Providers {
		if provider.Provider == "google_health" {
			publicGoogleHealth = provider
			break
		}
	}
	if publicGoogleHealth.DefaultScopes != "https://www.googleapis.com/auth/googlehealth.activity_and_fitness.readonly" {
		t.Fatalf("expected narrowed Google Health scopes to be preserved, got %q", publicGoogleHealth.DefaultScopes)
	}

	privateOura, err := store.Provider("oura")
	if err != nil {
		t.Fatalf("load private provider settings: %v", err)
	}
	if privateOura.ClientSecret != "oura-secret" {
		t.Fatalf("expected sqlite-backed client secret, got %q", privateOura.ClientSecret)
	}
}

func TestProviderNormalizesLegacyLoopbackRedirect(t *testing.T) {
	app, err := appstore.Open(context.Background(), filepath.Join(t.TempDir(), "somascope.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer app.Close()

	if err := app.UpsertProviderCredential(context.Background(), appstore.ProviderCredential{
		Provider:      "oura",
		ClientID:      "oura-client",
		ClientSecret:  "oura-secret",
		RedirectURI:   "http://127.0.0.1:18080/oauth/oura/callback",
		DefaultScopes: "",
		Notes:         "local",
	}); err != nil {
		t.Fatalf("seed provider credential: %v", err)
	}

	store := NewStore(app)
	provider, err := store.Provider("oura")
	if err != nil {
		t.Fatalf("provider settings: %v", err)
	}

	if provider.RedirectURI != "http://localhost:18080/oauth/oura/callback" {
		t.Fatalf("expected normalized redirect_uri, got %q", provider.RedirectURI)
	}
}

func TestLoadNormalizesLegacyOuraScopeString(t *testing.T) {
	app, err := appstore.Open(context.Background(), filepath.Join(t.TempDir(), "somascope.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer app.Close()

	if err := app.UpsertProviderCredential(context.Background(), appstore.ProviderCredential{
		Provider:      "oura",
		ClientID:      "oura-client",
		ClientSecret:  "oura-secret",
		RedirectURI:   "http://localhost:18080/oauth/oura/callback",
		DefaultScopes: legacyOuraDefaultScopes,
		Notes:         "local",
	}); err != nil {
		t.Fatalf("seed provider credential: %v", err)
	}

	store := NewStore(app)
	value, err := store.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}

	for _, provider := range value.Providers {
		if provider.Provider != "oura" {
			continue
		}
		if provider.DefaultScopes != "" {
			t.Fatalf("expected legacy Oura scope string to normalize to empty, got %q", provider.DefaultScopes)
		}
		return
	}

	t.Fatalf("expected Oura provider in settings load")
}

func TestLoadMigratesLegacyFitbitCredentials(t *testing.T) {
	app, err := appstore.Open(context.Background(), filepath.Join(t.TempDir(), "somascope.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer app.Close()

	if err := app.UpsertProviderCredential(context.Background(), appstore.ProviderCredential{
		Provider:      "fitbit",
		ClientID:      "legacy-fitbit-client",
		ClientSecret:  "legacy-fitbit-secret",
		RedirectURI:   "http://localhost:18080/oauth/fitbit/callback",
		DefaultScopes: "activity heartrate sleep profile",
		Notes:         "old fitbit app",
	}); err != nil {
		t.Fatalf("seed fitbit credential: %v", err)
	}

	store := NewStore(app)
	value, err := store.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}

	var googleHealth ProviderConfig
	for _, provider := range value.Providers {
		if provider.Provider == "google_health" {
			googleHealth = provider
		}
		if provider.Provider == "fitbit" {
			t.Fatalf("expected leftover fitbit provider to stay out of settings payload")
		}
	}
	if googleHealth.Configured {
		t.Fatalf("expected google_health to remain unconfigured")
	}
	if googleHealth.ClientID != "" {
		t.Fatalf("expected empty google_health client id, got %q", googleHealth.ClientID)
	}
	if googleHealth.RedirectURI != "http://localhost:18080/oauth/google_health/callback" {
		t.Fatalf("expected migrated redirect, got %q", googleHealth.RedirectURI)
	}

	private, err := store.Provider("google_health")
	if err != nil {
		t.Fatalf("load private google_health settings: %v", err)
	}
	if private.ClientSecret != "" {
		t.Fatalf("expected empty google_health secret, got %q", private.ClientSecret)
	}
	if private.Notes != "old fitbit app" {
		t.Fatalf("expected legacy notes to be preserved, got %q", private.Notes)
	}
}

func TestLoadDoesNotOverwritePartialGoogleHealthSettingsDuringFitbitMigration(t *testing.T) {
	app, err := appstore.Open(context.Background(), filepath.Join(t.TempDir(), "somascope.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer app.Close()

	for _, credential := range []appstore.ProviderCredential{
		{Provider: "fitbit", ClientID: "legacy-fitbit-client", ClientSecret: "legacy-fitbit-secret", RedirectURI: "http://localhost:18080/oauth/fitbit/callback"},
		{Provider: "google_health", ClientID: "google-client", RedirectURI: "http://localhost:18080/oauth/google_health/callback", DefaultScopes: "custom.readonly", Notes: "custom notes"},
	} {
		if err := app.UpsertProviderCredential(context.Background(), credential); err != nil {
			t.Fatalf("seed %s credential: %v", credential.Provider, err)
		}
	}

	store := NewStore(app)
	if _, err := store.Load(); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	googleHealth, err := store.Provider("google_health")
	if err != nil {
		t.Fatalf("load Google Health settings: %v", err)
	}
	if googleHealth.ClientID != "google-client" || googleHealth.DefaultScopes != "custom.readonly" || googleHealth.Notes != "custom notes" {
		t.Fatalf("partial Google Health settings were overwritten: %#v", googleHealth)
	}
}
