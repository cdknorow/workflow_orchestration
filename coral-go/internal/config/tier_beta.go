//go:build beta && !dev

package config

// Build tier: Beta
// Built with: go build -tags beta
// Requires EULA, skips license, enforces demo limits (3 teams / 12 agents).
var (
	TierSkipEULA    = false
	TierSkipLicense = true
	TierDemoLimits  = true
	TierName        = "beta"
	TierMaxTeams    = 3
	TierMaxAgents   = 12

	// Test store (Lemon Squeezy sandbox)
	StoreTrialURL = "https://store.coralai.ai/checkout/buy/59b4153c-d389-44f4-8a03-cf840261844e"
	StoreProURL   = "https://store.coralai.ai/checkout/buy/44df39dc-9891-4094-8b77-f73c1d2596ae"
	StorePromo    = ""
)
