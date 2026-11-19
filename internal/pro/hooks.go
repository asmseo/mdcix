// Package pro holds open-core extension hooks.
//
// The MIT core always runs without a Pro license. Implementations of these
// interfaces may be supplied by a future Pro build; the core never requires them.
package pro

// Features reports whether a named Pro capability is available.
// Core code may call Enabled; a nil Features (or Disabled) always returns false.
type Features interface {
	Enabled(name string) bool
}

// License describes an optional Pro entitlement.
// Placeholders only — no billing, validation, or network calls in the MVP.
type License interface {
	// Valid reports whether a Pro license is present and usable.
	Valid() bool
	// Plan returns a human-readable plan name (empty when none).
	Plan() string
}

// Disabled is the default Features implementation used by the open core.
type Disabled struct{}

// Enabled always returns false.
func (Disabled) Enabled(string) bool { return false }

// None is the default License implementation (no Pro entitlement).
type None struct{}

// Valid always returns false.
func (None) Valid() bool { return false }

// Plan always returns an empty string.
func (None) Plan() string { return "" }

// Known Pro feature names (documented placeholders; not implemented in core).
const (
	FeatureAdvancedCache   = "advanced_cache"
	FeatureDomainRules     = "domain_rules"
	FeatureDashboard       = "dashboard"
	FeatureTeamAuth        = "team_auth"
	FeatureChromeTransport = "chrome_transport"
	FeatureMCP             = "mcp"
)
