//go:build !dev && !beta

package config

// Build tier: Prod (default)
// Built with: go build (no tags)
// Requires EULA and license. Demo limits controlled by LS plan at runtime.
var (
	TierSkipEULA    = false
	TierSkipLicense = false
	TierDemoLimits  = false
	TierName        = "prod"
	TierMaxTeams    = 0
	TierMaxAgents   = 0

	// Live store (Lemon Squeezy)
	StoreTrialURL = "https://store.coralai.ai/checkout/buy/6e3facc8-9fe8-4126-9000-d1a006e1e71a"
	StoreProURL   = "https://store.coralai.ai/checkout/buy/1cf08999-ef06-466d-938c-b0f6ec4f92e6"
	StorePromo    = "A3MDI0NQ"
)
