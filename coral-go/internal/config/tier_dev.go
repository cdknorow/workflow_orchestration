//go:build dev && !beta && !dropboxers

package config

// Build tier: Dev
// Built with: go build -tags dev
// Skips EULA and license, no demo limits. For internal development only.
var (
	TierSkipEULA    = true
	TierSkipLicense = true
	TierDemoLimits  = false
	TierName        = "dev"
	TierMaxTeams    = 0
	TierMaxAgents   = 0

	// Test store (Lemon Squeezy sandbox)
	StoreTrialURL = "https://store.coralai.ai/checkout/buy/59b4153c-d389-44f4-8a03-cf840261844e"
	StoreProURL   = "https://store.coralai.ai/checkout/buy/44df39dc-9891-4094-8b77-f73c1d2596ae"
	StorePromo    = ""
)
