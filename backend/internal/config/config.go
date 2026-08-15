package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Env              string
	Port             string
	LogLevel         string
	AppDatabaseURL   string
	AdminDatabaseURL string
	NATSURL          string
	OpenFGAURL       string
	OpenFGAStore     string

	// ZitadelIssuerURL and ZitadelClientID configure the standard-OIDC
	// verifier in pkg/authn/zitadel.go (spike
	// docs/superpowers/spikes/2026-08-15-zitadel-spike.md P0-2/P0-4):
	// issuer for discovery, clientID to pin the audience check. Both
	// default to the local dev stack's Zitadel (spike/zitadel-838, port
	// 20080) — a wrong default here fails verification loudly (every
	// real token's issuer/audience will mismatch) rather than opening a
	// hole, so a getenv default is safe: unlike SessionSigningKey below,
	// there is no signature-bypass mode a wrong-but-present value can
	// trigger here.
	ZitadelIssuerURL string
	ZitadelClientID  string

	// SessionSigningKey is the raw, still-encoded value of
	// SESSION_SIGNING_KEY — a base64 Ed25519 seed. Deliberately NOT
	// decoded or defaulted here: Load() has no way to fail, and a
	// signing key is exactly the thing that must be able to refuse
	// construction (see SessionSigningKeySeed and §3 below). It is left
	// as the empty string when unset, same as any other unset env var,
	// and it is SessionSigningKeySeed's job — not Load's — to turn
	// "empty" into a boot refusal.
	SessionSigningKey string
	// SessionIssuer is the `iss` claim HMS's own session tokens carry
	// and the Verifier checks against. Safe to default: it is a label,
	// not a secret, and an operator who cares can override it.
	SessionIssuer string
	// SessionTTL bounds how long an HMS session is honoured before it
	// must be renewed, and — post-#838 (spec D4/D4a) — renewal is the
	// login exchange re-run with a fresh Zitadel token, re-checking
	// membership every time. So this ONE number is the upper bound on
	// BOTH: how long a user deactivated upstream in Zitadel keeps
	// working, and how stale an OpenFGA membership grant/revoke can be
	// before it is re-observed.
	//
	// 15 minutes, chosen (#838 Task 5) by weighing that bound against
	// renewal traffic: renewal is a silent, browser-driven OIDC round
	// trip against Zitadel plus one local Ed25519 verify and one FGA
	// membership check — none of it against the project-wide quota the
	// old GIP mint threatened (see bootstrap.RateLimitConfig's doc
	// comment) — so a short TTL costs a few silent requests per hour per
	// active session, not a shared resource. Against that cheap cost, 15
	// minutes is short enough that a clinician deactivated mid-shift, or
	// a member whose role is pulled for a safety reason, loses access
	// within a quarter hour rather than surviving to their next full
	// re-login — appropriate for a system whose failure mode is
	// clinical, not merely inconvenient (engineering-principles.md's
	// framing). It is not shortened further only because a TTL near the
	// low end of what a human page-to-page session naturally spans (a
	// few minutes) would turn ordinary navigation into visible renewal
	// latency; 15 minutes stays well clear of that without meaningfully
	// widening the deactivation window in absolute terms.
	SessionTTL time.Duration

	// Rate limits are env-configurable, unlike the pagination page-size
	// constants: a page size bounds a query, but a rate limit bounds
	// capacity, and capacity genuinely differs between a laptop running
	// the e2e suite and a hospital in production.
	RateLimitTenantPerMin    int
	RateLimitPrincipalPerMin int
}

func Load() Config {
	return Config{
		Env:              getenv("HMS_ENV", "production"),
		Port:             getenv("PORT", "8080"),
		LogLevel:         getenv("LOG_LEVEL", "info"),
		AppDatabaseURL:   getenv("APP_DATABASE_URL", "postgres://hms_app:hms_app@localhost:5432/hms?sslmode=disable"),
		AdminDatabaseURL: getenv("ADMIN_DATABASE_URL", "postgres://hms:hms@localhost:5432/hms?sslmode=disable"),
		NATSURL:          getenv("NATS_URL", "nats://localhost:4222"),
		OpenFGAURL:       getenv("OPENFGA_URL", "http://localhost:8090"),
		OpenFGAStore:     getenv("OPENFGA_STORE", "hms"),

		ZitadelIssuerURL: getenv("ZITADEL_ISSUER_URL", "http://localhost:20080"),
		ZitadelClientID:  getenv("ZITADEL_CLIENT_ID", ""),

		// os.Getenv, not getenv(): getenv's whole purpose is supplying a
		// default for an unset variable, and a signing key must never
		// have one (see SessionSigningKeySeed's doc comment).
		SessionSigningKey: os.Getenv("SESSION_SIGNING_KEY"),
		SessionIssuer:     getenv("SESSION_ISSUER", "https://hms.local"),
		SessionTTL:        getenvDuration("SESSION_TTL", 15*time.Minute),

		RateLimitTenantPerMin:    getenvInt("RATE_LIMIT_TENANT_PER_MIN", 600),
		RateLimitPrincipalPerMin: getenvInt("RATE_LIMIT_PRINCIPAL_PER_MIN", 120),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// getenvInt returns def when k is unset OR unparseable. A rate limit is a
// capacity control, not a data or identity control: per
// docs/standards/engineering-principles.md §3, capacity controls fail
// OPEN, not closed. A mistyped RATE_LIMIT_* value must not be able to
// stop a hospital's API from booting — the same direction LOG_LEVEL
// already takes in pkg/logging.NewWithWriter for the same reason. The
// fallback is logged so the mistype is visible rather than silently
// eaten.
func getenvInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("invalid integer env var; using default", "key", k, "value", v, "default", def)
		return def
	}
	return n
}

// getenvDuration returns def when k is unset OR unparseable, for the
// same reason getenvInt does: a session TTL is a capacity control (it
// trades renewal traffic against how long a stale grant keeps working),
// not the identity control the signing key itself is. A mistyped
// SESSION_TTL must not be able to stop the API from booting; the
// fallback is logged so the mistype is visible rather than silently
// eaten.
func getenvDuration(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		// def.String(), never the bare Duration: slog renders a
		// time.Duration as its nanosecond int64, and pkg/logging's PHI
		// matcher reads long digit runs as identifiers — 15m becomes
		// 900000000000, a 12-digit run, and is emitted as
		// "[REDACTED:aadhaar]"; 24h is 14 digits and comes out as
		// "[REDACTED:abha]". The whole point of this line is to make a
		// mistyped value visible, so a fallback the operator cannot read
		// is the one thing it must not be.
		slog.Warn("invalid duration env var; using default", "key", k, "value", v, "default", def.String())
		return def
	}
	return d
}

// IsDev reports whether this process is running in a developer
// environment. It defaults to false: the guards that consult it disable
// production safety checks, so an unset or misspelled HMS_ENV must fail
// closed rather than silently unlock them.
func (c Config) IsDev() bool { return c.Env == "dev" }
