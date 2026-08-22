package server

const (
	providerOura         = "oura"
	providerGoogleHealth = "google_health"
)

func knownProviders() []string {
	return []string{providerOura, providerGoogleHealth}
}

func isKnownProvider(name string) bool {
	for _, provider := range knownProviders() {
		if provider == name {
			return true
		}
	}
	return false
}

func providerDisplayName(provider string) string {
	switch provider {
	case providerGoogleHealth:
		return "Google Health"
	case providerOura:
		return "Oura"
	default:
		return provider
	}
}
