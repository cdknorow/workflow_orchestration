//go:build dropboxers && !dev && !beta

package config

// Build tier: Dropboxers
// Built with: go build -tags dropboxers
// Requires EULA, skips license, enforces demo limits (3 teams / 12 agents).
var (
	TierSkipEULA    = true
	TierSkipLicense = true
	TierDemoLimits  = true
	TierName        = "dropboxers"
	TierMaxTeams    = 3
	TierMaxAgents   = 12

	// Test store (Lemon Squeezy sandbox)
	StoreTrialURL = "https://store.coralai.ai/checkout/buy/59b4153c-d389-44f4-8a03-cf840261844e"
	StoreProURL   = "https://store.coralai.ai/checkout/buy/44df39dc-9891-4094-8b77-f73c1d2596ae"
	StorePromo    = ""
)
