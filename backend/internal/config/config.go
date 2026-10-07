package config

import (
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env              string
	Port             string
	LogLevel         string
	AppDatabaseURL   string
	AdminDatabaseURL string
	// SystemDatabaseURL names the BYPASSRLS role. Only cmd/api needs it —
	// it is the only binary that reconciles, drains the outbox or prunes
	// retention, which are the three operations that cross tenants (#894).
	SystemDatabaseURL string
	NATSURL           string
	OpenFGAURL        string
	OpenFGAStore      string

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
	// SINGLE MOST PRIVILEGED credential Helivanta holds: IAM_LOGIN_CLIENT is
	// scoped to the whole Zitadel instance, not to Helivanta's own project, so
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
	// (e.g. http://auth.tesserix.localhost:20080/ui/v2/login) — the target
	// LoginUIHandlers.Handoff (and a Password call that resolves to
	// OutcomeHandoff) redirect the browser to when Helivanta's own login form
	// cannot complete a sign-in itself (an enrolled second factor Zitadel
	// requires but Helivanta does not yet collect). Task 1's finding
	// (docs/superpowers/plans/2026-08-16-helivanta-login-client.md) is that
	// Zitadel APPENDS its own "/login" segment to whatever baseUri is
	// configured, so this must be an origin+path with NO query string of
	// its own — see loginui.go's handoffURL. Safe to default: it is a
	// well-known Zitadel URL, not a secret, and a wrong value fails
	// loudly (a 404 from Zitadel) rather than opening a hole.
	ZitadelHostedLoginURL string
	// HelivantaWebOrigin is the raw value of HELIVANTA_WEB_ORIGIN — the origin
	// (scheme + host, no path) Helivanta's OWN frontend is served from, the
	// same origin scripts/lib/zitadel.mjs configures as Zitadel's
	// per-app `loginVersion.loginV2.baseUri` for helivanta-web (spec D1), and
	// the origin `/login?authRequest=…` renders on. It exists SOLELY so
	// RequireDistinctHostedLoginOrigin (hostedlogin.go) has something to
	// compare ZitadelHostedLoginURL against at boot — nothing on the
	// request path reads it, because every real request already reaches
	// this API through the frontend's own same-origin `/api` rewrite
	// (docs/standards/frontend.md §3) and never needs to be told its own
	// origin back.
	//
	// Deliberately NOT defaulted here with getenv, unlike
	// ZitadelHostedLoginURL immediately above — an EARLIER version of
	// this field was, and that turned RequireDistinctHostedLoginOrigin
	// into exactly the kind of control this codebase does not accept:
	// one that looks present and does nothing under the conditions that
	// matter. A silent `getenv("HELIVANTA_WEB_ORIGIN", DevHelivantaWebOrigin)`
	// default means an unset variable in PRODUCTION compares the real
	// ZitadelHostedLoginURL against the DEV origin, finds no collision
	// (they are never equal), and boots — the exact loop this guard
	// exists to make unrepresentable stays fully possible, silently. See
	// RequireDistinctHostedLoginOrigin's doc comment (hostedlogin.go) for
	// where the dev default is applied instead: only inside
	// Config.IsDev(), the same guard DevSessionSigningKey needs and gets
	// from SessionSigningKeySeed for the identical reason.
	HelivantaWebOrigin string

	// SessionSigningKey is the raw, still-encoded value of
	// SESSION_SIGNING_KEY — a base64 Ed25519 seed. Deliberately NOT
	// decoded or defaulted here: Load() has no way to fail, and a
	// signing key is exactly the thing that must be able to refuse
	// construction (see SessionSigningKeySeed and §3 below). It is left
	// as the empty string when unset, same as any other unset env var,
	// and it is SessionSigningKeySeed's job — not Load's — to turn
	// "empty" into a boot refusal.
	SessionSigningKey string
	// SessionIssuer is the `iss` claim Helivanta's own session tokens carry
	// and the Verifier checks against. Safe to default: it is a label,
	// not a secret, and an operator who cares can override it.
	SessionIssuer string
	// SessionTTL bounds how long a Helivanta session is honoured before it
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
	//
	// A MISTYPED value falls back to DefaultSessionTTL (getenvDuration).
	// A value that parses but is below MinSessionTTL — including "0" and
	// negatives — is refused at boot by RequireSessionTTL
	// (sessionttl.go, #921). Read it through that accessor, never
	// straight off this field.
	SessionTTL time.Duration

	// IdleTimeout is how long a Helivanta session stays usable with no human
	// interaction at all (#848, spec D1/D2): the idle_deadline claim a
	// genuinely new login mints is time.Now() + IdleTimeout, and
	// authn.Middleware refuses every request at or past that instant.
	//
	// This is a CLINICAL WORKFLOW value, not a technical one. 15 minutes
	// is spec D1's judgement about a ward terminal: long enough for a
	// clinician to read a chart, take a call or talk to a patient without
	// being interrupted, short enough that a walk-away is caught well
	// inside a shift, before the next person at the terminal inherits the
	// session and every action they take is attributed to whoever walked
	// away. Shortening it is a decision about how much uninterrupted
	// reading a ward does, not about server load; lengthening it is a
	// decision about how long an unattended terminal stays signed in.
	//
	// It is DELIBERATELY independent of SessionTTL even though both are
	// currently 15 minutes, and the equality is a coincidence of two
	// separate judgements rather than a coupling (spec D1: "It happens to
	// equal SESSION_TTL, which is convenient but not a coupling"). D3
	// depends on the two clocks staying independent: SessionTTL is this
	// TOKEN's own lifetime and is moved forward by every silent renewal
	// (D4a, every 5 minutes), while IdleTimeout measures the HUMAN and
	// must survive those renewals untouched — see login.go's
	// idleDeadlineFor. Deriving one from the other, in either direction,
	// would let the renewal that legitimately extends `exp` also extend
	// the idle deadline, and an untouched tab would then renew itself
	// forever while the feature looked implemented.
	//
	// getenvDuration, so a MISTYPED IDLE_TIMEOUT ("fifteen") logs and
	// falls back to DefaultIdleTimeout rather than stopping a hospital's
	// API from booting — same direction SESSION_TTL takes. A
	// deliberately non-positive value ("0", "-5m") is a different case:
	// it parses cleanly, so that fallback never sees it, and it is
	// refused at boot by RequireIdleTimeout (idletimeout.go) rather than
	// left to fail closed as a hospital-wide sign-out. Read it through
	// that accessor, never straight off this field, on any path that
	// decides a deadline.
	IdleTimeout time.Duration

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
	// ratelimit.Middleware to key on). #916 moved renewal traffic off
	// this route onto POST /v1/auth/renew (RateLimitRenewPerMin below,
	// bootstrap/ratelimit.go's RenewRateLimitRule) — this knob is now
	// sized off genuine sign-in exchange clustering, not renewal traffic
	// — see
	// docs/superpowers/specs/2026-08-16-login-rate-limit-design.md D2 for
	// the arithmetic behind the default, and bootstrap/ratelimit.go's
	// LoginRateLimitRule for the current, restated reasoning.
	RateLimitLoginPerMin int
	// RateLimitActivityPerMin bounds POST /v1/auth/session/activity
	// (#848 Task 4), keyed on the authenticated subject — a separate
	// knob from RateLimitPrincipalPerMin for the same shape of reason
	// RateLimitLoginPerMin is: this endpoint's legitimate traffic (D4's
	// 60-second debounce, shared across tabs) has nothing to do with
	// ordinary API call volume, so a shared budget would let the two
	// throttle each other. See bootstrap.ActivityRateLimitRule for the
	// arithmetic behind the default.
	RateLimitActivityPerMin int
	// RateLimitFactorPerMin bounds POST /v1/auth/login/factor (#867 Task
	// 4), keyed on client IP (there is no verified subject yet — see
	// iam.allowedByLimiter). A separate knob from RateLimitLoginPerMin
	// for the same shape of reason that one is separate from
	// RateLimitPrincipalPerMin: this route's traffic shape (a single
	// six-digit code guess) has nothing to do with either general API
	// traffic or password-check traffic, and it is a PRIMARY
	// brute-force control, not a secondary one — see
	// bootstrap.FactorRateLimitRule for the arithmetic behind the
	// default.
	RateLimitFactorPerMin int
	// RateLimitRenewPerMin bounds POST /v1/auth/renew (#916 Task 2
	// Review Round 1, IMPORTANT 3), keyed on the authenticated subject.
	// A separate knob from RateLimitPrincipalPerMin because one call to
	// this route is one call to Zitadel's core API on the instance-wide
	// login-client PAT — a shared external resource this route uniquely
	// threatens, the exact criterion RateLimitLoginPerMin's own doc
	// comment (and the tenant-switch history bootstrap.ratelimit.go
	// records) uses to decide a route needs its own budget rather than
	// drawing from Principal. See bootstrap.RenewRateLimitRule for the
	// arithmetic behind the default.
	RateLimitRenewPerMin int

	// TrustedProxyCIDRs is the raw value of TRUSTED_PROXY_CIDRS, a
	// comma-separated list of CIDR blocks — e.g. the production pod
	// CIDR the Istio ingress gateway runs in — whose IMMEDIATE TCP peer
	// this process trusts to have set X-Forwarded-For/X-Real-IP
	// honestly (#867 Task 4 fix round 3, Finding C1).
	//
	// # Why this exists: every rate limit in this file is bypassable without it
	//
	// gin's default (httpserver.New wires this into
	// (*gin.Engine).SetTrustedProxies) trusts EVERY proxy — 0.0.0.0/0 —
	// which means gin.Context.ClientIP() honours a caller-supplied
	// X-Forwarded-For unconditionally. Every rate limiter in this
	// codebase that is keyed on client IP before a subject exists
	// (LoginRateLimitRule, FactorRateLimitRule, iam.allowedByLimiter's
	// three sibling routes) reads that same ClientIP() — so with gin's
	// default, a caller sends a fresh X-Forwarded-For value on every
	// request and gets a fresh full token-bucket burst every time,
	// bypassing the limiter entirely. FactorRateLimitRule's own doc
	// comment calls this limiter "what actually bounds the total number
	// of attempts an attacker gets" against a six-digit TOTP code — that
	// claim is false until the trusted-proxy boundary below is
	// configured correctly for wherever this process actually runs.
	//
	// # Fail CLOSED: unset or empty means TRUST NOTHING, never "trust everything"
	//
	// This is the opposite direction from getenvInt/getenvDuration
	// immediately below, deliberately: those are CAPACITY controls
	// (docs/standards/engineering-principles.md §3) that must fail open
	// so a mistyped value cannot take a hospital's API down, but an
	// UNSET or EMPTY TrustedProxyCIDRs is not a mistype of a capacity
	// number — it is "we do not know this process's network topology",
	// and gin's own insecure default (trust every proxy) is exactly the
	// wrong answer to that uncertainty. httpserver.New therefore calls
	// (*gin.Engine).SetTrustedProxies(nil) whenever this slice is empty
	// — NOT gin's default — which disables the X-Forwarded-For/
	// X-Real-IP mechanism entirely and makes ClientIP() return the raw
	// TCP RemoteAddr, unspoofable by any header. Failing closed here
	// costs COARSER rate-limit buckets (every caller behind an untrusted
	// reverse proxy collapses onto that proxy's own IP) — it never costs
	// "no bound at all", which is what trusting everyone by default
	// would risk.
	//
	// # Never hardcoded
	//
	// The production value (the GKE cluster's pod CIDR, where the Istio
	// ingress gateway that terminates external traffic runs) is an
	// operational fact about ONE deployment, not a constant this
	// package should know — a second environment (a different cluster,
	// a different CNI plugin, local dev behind a different proxy shape)
	// needs a different value, and a value baked into source would
	// silently stop matching reality the day infrastructure changes
	// without anyone touching this file. See bootstrap/ratelimit.go's
	// own doc comments for the same "operational fact belongs in
	// config, not a constant" reasoning applied to rate-limit numbers.
	TrustedProxyCIDRs []string
}

func Load() Config {
	return Config{
		Env:              getenv("HELIVANTA_ENV", "production"),
		Port:             getenv("PORT", "8080"),
		LogLevel:         getenv("LOG_LEVEL", "info"),
		AppDatabaseURL:   getenv("APP_DATABASE_URL", "postgres://hms_app:hms_app@localhost:5432/helivanta?sslmode=disable"),
		AdminDatabaseURL: getenv("ADMIN_DATABASE_URL", "postgres://helivanta:helivanta@localhost:5432/helivanta?sslmode=disable"),
		// The dev default names a role dev/init-db.sql creates with
		// BYPASSRLS. There is deliberately no fallback to
		// ADMIN_DATABASE_URL: that role is the schema owner, FORCE RLS
		// binds it, and every cross-tenant read would return zero rows
		// while reporting success — which is #894 exactly.
		SystemDatabaseURL: getenv("SYSTEM_DATABASE_URL", "postgres://helivanta_system:helivanta_system@localhost:5432/helivanta?sslmode=disable"),
		NATSURL:           getenv("NATS_URL", "nats://localhost:4222"),
		OpenFGAURL:        getenv("OPENFGA_URL", "http://localhost:8090"),
		OpenFGAStore:      getenv("OPENFGA_STORE", "helivanta"),

		ZitadelIssuerURL: getenv("ZITADEL_ISSUER_URL", "http://auth.tesserix.localhost:20080"),
		ZitadelClientID:  getenv("ZITADEL_CLIENT_ID", ""),

		// os.Getenv, not getenv(): mirrors SessionSigningKey immediately
		// below — this PAT must never have a default (see
		// ZitadelLoginClientToken's doc comment on exactly why).
		ZitadelLoginClientToken: os.Getenv("ZITADEL_LOGIN_CLIENT_TOKEN"),
		ZitadelHostedLoginURL:   getenv("ZITADEL_HOSTED_LOGIN_URL", "http://auth.tesserix.localhost:20080/ui/v2/login"),
		// os.Getenv, not getenv(): see HelivantaWebOrigin's doc comment just
		// above — RequireDistinctHostedLoginOrigin (hostedlogin.go), not
		// Load(), is where an unset value is resolved, and it resolves
		// differently in dev (DevHelivantaWebOrigin) than everywhere else
		// (a boot refusal), which a getenv() default here would make
		// impossible to tell apart from an operator's real value.
		HelivantaWebOrigin: os.Getenv("HELIVANTA_WEB_ORIGIN"),

		// os.Getenv, not getenv(): getenv's whole purpose is supplying a
		// default for an unset variable, and a signing key must never
		// have one (see SessionSigningKeySeed's doc comment).
		SessionSigningKey: os.Getenv("SESSION_SIGNING_KEY"),
		SessionIssuer:     getenv("SESSION_ISSUER", "https://helivanta.local"),
		SessionTTL:        getenvDuration("SESSION_TTL", DefaultSessionTTL),
		// Read from its OWN variable, never derived from SESSION_TTL —
		// see IdleTimeout's doc comment on why the two clocks must stay
		// independent even while they share a value.
		IdleTimeout: getenvDuration("IDLE_TIMEOUT", DefaultIdleTimeout),

		RateLimitTenantPerMin:    getenvInt("RATE_LIMIT_TENANT_PER_MIN", 600),
		RateLimitPrincipalPerMin: getenvInt("RATE_LIMIT_PRINCIPAL_PER_MIN", 120),
		RateLimitLoginPerMin:     getenvInt("RATE_LIMIT_LOGIN_PER_MIN", 20),
		RateLimitActivityPerMin:  getenvInt("RATE_LIMIT_ACTIVITY_PER_MIN", 10),
		RateLimitFactorPerMin:    getenvInt("RATE_LIMIT_FACTOR_PER_MIN", 10),
		RateLimitRenewPerMin:     getenvInt("RATE_LIMIT_RENEW_PER_MIN", 6),

		TrustedProxyCIDRs: getenvCIDRList("TRUSTED_PROXY_CIDRS"),
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

// getenvCIDRList parses k as a comma-separated list of CIDR blocks, for
// TrustedProxyCIDRs (#867 Task 4 fix round 3, Finding C1). Unlike
// getenvInt/getenvDuration above, there is no `def` parameter — an unset
// variable returns nil, and nil is not a fallback value here, it IS the
// fail-closed answer TrustedProxyCIDRs' own doc comment describes
// ("trust nothing"). Each entry is independently validated with
// net.ParseCIDR; an INDIVIDUAL malformed entry is dropped (logged) with
// the surviving valid entries still applied — rather than discarding the
// whole list — because a partially-wrong value (a typo in one of several
// CIDRs) should still leave the operator's other, correctly-typed
// entries in effect, and because the reverse (one bad entry silently
// disabling every trust boundary) would fail OPEN in exactly the case
// this function exists to keep closed. A list that is entirely garbage
// still ends up empty, which is still fail-closed.
func getenvCIDRList(k string) []string {
	raw := os.Getenv(k)
	if raw == "" {
		return nil
	}
	var cidrs []string
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(entry); err != nil {
			slog.Warn("invalid CIDR in TRUSTED_PROXY_CIDRS entry; dropping this entry only",
				"key", k, "value", entry, "err", err)
			continue
		}
		cidrs = append(cidrs, entry)
	}
	return cidrs
}

// IsDev reports whether this process is running in a developer
// environment. It defaults to false: the guards that consult it disable
// production safety checks, so an unset or misspelled HELIVANTA_ENV must fail
// closed rather than silently unlock them.
func (c Config) IsDev() bool { return c.Env == "dev" }
