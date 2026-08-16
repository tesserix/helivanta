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

	// ZitadelLoginClientToken is the raw value of
	// ZITADEL_LOGIN_CLIENT_TOKEN — an instance-level Zitadel Personal
	// Access Token for a machine user holding the IAM_LOGIN_CLIENT role
	// (docker-compose.dev.yml's zitadel service,
	// FirstInstance.Org.LoginClient; plan #854 Task 4/5). This is the
	// SINGLE MOST PRIVILEGED credential HMS holds: IAM_LOGIN_CLIENT is
	// scoped to the whole Zitadel instance, not to HMS's own project, so
	// a holder can read and FINALIZE an OIDC auth request for ANY app on
	// the instance — including other Tesserix products that share it.
	// loginclient.Client uses it to call Zitadel's v2 login-client API
	// (AuthRequest, CreatePasswordSession, and — ONLY through
	// CompleteIfSufficient's fail-closed sufficiency check,
	// loginclient/sufficiency.go — finalize). It must never appear in a
	// log line, an error message, or an HTTP response: nothing in
	// internal/modules/iam/loginui.go or loginclient ever formats this
	// value into anything a caller or a log sink can read.
	//
	// Deliberately NOT decoded, defaulted, or generated here, for the
	// exact same reason as SessionSigningKey immediately below: Load()
	// cannot fail, and a credential this privileged must be able to
	// refuse process construction rather than boot with an absent or
	// placeholder value that quietly makes every login request 503. It
	// is left as the empty string when unset, and it is
	// RequireZitadelLoginClientToken's job — not Load's — to turn
	// "empty" into a boot refusal (see zitadellogin.go).
	//
	// PRODUCTION NOTE: #45 (secrets management) must cover how this PAT
	// is provisioned, rotated, and delivered to the production process
	// in a way that never touches source control or a shared dev
	// default — dev/zitadel/secrets/login-client.pat (Makefile's
	// dev-api target) is a local-only convenience with none of those
	// properties.
	ZitadelLoginClientToken string
	// ZitadelHostedLoginURL is Zitadel's own hosted login origin+path
	// (e.g. http://localhost:20080/ui/v2/login) — the target
	// LoginUIHandlers.Handoff (and a Password call that resolves to
	// OutcomeHandoff) redirect the browser to when HMS's own login form
	// cannot complete a sign-in itself (an enrolled second factor Zitadel
	// requires but HMS does not yet collect). Task 1's finding
	// (docs/superpowers/plans/2026-08-16-hms-login-client.md) is that
	// Zitadel APPENDS its own "/login" segment to whatever baseUri is
	// configured, so this must be an origin+path with NO query string of
	// its own — see loginui.go's handoffURL. Safe to default: it is a
	// well-known Zitadel URL, not a secret, and a wrong value fails
	// loudly (a 404 from Zitadel) rather than opening a hole.
	ZitadelHostedLoginURL string
	// HMSWebOrigin is the origin (scheme + host, no path) HMS's OWN
	// frontend is served from — the same origin scripts/lib/zitadel.mjs
	// configures as Zitadel's per-app `loginVersion.loginV2.baseUri` for
	// hms-web (spec D1), and the origin `/login?authRequest=…` renders
	// on. It exists SOLELY so RequireDistinctHostedLoginOrigin
	// (hostedlogin.go) has something to compare ZitadelHostedLoginURL
	// against at boot — nothing on the request path reads it, because
	// every real request already reaches this API through the frontend's
	// own same-origin `/api` rewrite (docs/standards/frontend.md §3) and
	// never needs to be told its own origin back. Safe to default: like
	// ZitadelHostedLoginURL, it is a well-known, non-secret URL, and an
	// operator who gets it wrong in production either fails the boot
	// guard immediately (if it collides with the hosted-login origin) or
	// changes nothing at all (if it does not — the value is otherwise
	// inert).
	HMSWebOrigin string

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
	// RateLimitLoginPerMin bounds POST /v1/auth/login, keyed on the
	// verified Zitadel subject (#841) — a separate knob from
	// RateLimitPrincipalPerMin because login runs entirely outside
	// bootstrap.V1Chain (there is no authn.Principal yet for
	// ratelimit.Middleware to key on) and is sized off renewal traffic,
	// not general API traffic — see
	// docs/superpowers/specs/2026-08-16-login-rate-limit-design.md D2 for
	// the arithmetic behind the default.
	RateLimitLoginPerMin int
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

		// os.Getenv, not getenv(): mirrors SessionSigningKey immediately
		// below — this PAT must never have a default (see
		// ZitadelLoginClientToken's doc comment on exactly why).
		ZitadelLoginClientToken: os.Getenv("ZITADEL_LOGIN_CLIENT_TOKEN"),
		ZitadelHostedLoginURL:   getenv("ZITADEL_HOSTED_LOGIN_URL", "http://localhost:20080/ui/v2/login"),
		// Default matches scripts/lib/zitadel.mjs's DEV_REDIRECT_URI
		// origin — the same dev-stack value Zitadel's per-app login base
		// URI is provisioned with (spec D1), so a fresh clone's defaults
		// agree with each other without either side having to read the
		// other's config.
		HMSWebOrigin: getenv("HMS_WEB_ORIGIN", "http://localhost:4301"),

		// os.Getenv, not getenv(): getenv's whole purpose is supplying a
		// default for an unset variable, and a signing key must never
		// have one (see SessionSigningKeySeed's doc comment).
		SessionSigningKey: os.Getenv("SESSION_SIGNING_KEY"),
		SessionIssuer:     getenv("SESSION_ISSUER", "https://hms.local"),
		SessionTTL:        getenvDuration("SESSION_TTL", 15*time.Minute),

		RateLimitTenantPerMin:    getenvInt("RATE_LIMIT_TENANT_PER_MIN", 600),
		RateLimitPrincipalPerMin: getenvInt("RATE_LIMIT_PRINCIPAL_PER_MIN", 120),
		RateLimitLoginPerMin:     getenvInt("RATE_LIMIT_LOGIN_PER_MIN", 20),
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
