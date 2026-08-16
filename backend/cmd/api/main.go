package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/hms/internal/bootstrap"
	"github.com/tesserix/hms/internal/config"
	"github.com/tesserix/hms/internal/httpserver"
	"github.com/tesserix/hms/internal/modules/iam"
	"github.com/tesserix/hms/internal/modules/iam/loginclient"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/platform/requestid"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/logging"
	"github.com/tesserix/hms/pkg/ratelimit"
	"github.com/tesserix/hms/pkg/session"
	"github.com/tesserix/hms/pkg/tenantdb"
)

// sessionSigningKeyID is the `kid` HMS's session tokens carry until key
// rotation is designed (out of scope here, plan Task 2 / spec D5 — the
// header exists from the start so rotation is not precluded later).
// Fixed rather than derived from the key material itself: deriving it
// from the key would make "kid" a second, redundant fingerprint of the
// exact secret this whole package exists to keep out of logs and error
// messages.
const sessionSigningKeyID = "hms-session-v1"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	// Before anything else logs: until this runs, slog.Default() is the
	// unconfigured text handler on stderr and nothing is redacted.
	slog.SetDefault(logging.New(cfg.LogLevel))
	// Logged once at boot because HMS_ENV silently gates production safety
	// checks (see Config.IsDev) — a prod process accidentally started with
	// HMS_ENV=dev would otherwise disable them with no signal anywhere.
	slog.Info("resolved environment", "env", cfg.Env, "is_dev", cfg.IsDev())

	// Resolved before anything else that costs time or a network round
	// trip (DB, NATS, OpenFGA): a missing or malformed signing key is a
	// configuration defect the operator can fix in seconds, and it
	// should be the FIRST thing a bad deploy reports, not something
	// discovered after a DB connection and a set of migrations already
	// ran. See config.SessionSigningKeySeed's doc comment for why this
	// refuses rather than generating or defaulting a key — it is a
	// data/identity control (engineering-principles.md §3) and fails
	// CLOSED, unlike every other cfg.* value read so far.
	//
	// Task 2 only decoded and validated the key here, deliberately not
	// yet turned into a session.Signer/session.Verifier: nothing minted
	// or checked an HMS session until Task 4 of
	// docs/superpowers/plans/2026-08-15-zitadel-auth.md landed the login
	// endpoint and the request-path verifier that use it — see the
	// Signer/Verifier construction just below run() builds the OpenFGA
	// client, where both are now actually wired to something.
	sessionSeed, err := cfg.SessionSigningKeySeed()
	if err != nil {
		return err
	}
	// Resolved alongside the signing key, for the same reason: a missing
	// login-client PAT is a configuration defect, not a runtime one, and
	// it should surface before anything else costs time or a network
	// round trip. See config.RequireZitadelLoginClientToken's doc
	// comment for why this fails closed rather than booting with login
	// silently broken — the SAME direction SessionSigningKeySeed already
	// takes for a different secret, immediately above.
	zitadelLoginClientToken, err := cfg.RequireZitadelLoginClientToken()
	if err != nil {
		return err
	}
	// Same class of check, same reason to run it here: a non-positive
	// IDLE_TIMEOUT parses cleanly (so getenvDuration's mistyped-value
	// fallback never sees it) and then fails closed at the worst possible
	// granularity — every session minted already past its idle deadline,
	// so every clinician's every request is refused with session_idle.
	// See config.RequireIdleTimeout's doc comment for why a boot refusal
	// is the only place that defect can usefully surface, and why it is
	// deliberately not clamped to the default.
	idleTimeout, err := cfg.RequireIdleTimeout()
	if err != nil {
		return err
	}
	// Same class of check, same reason to run it here rather than on the
	// hot path: a hosted-login URL misconfigured to share an origin with
	// HMS's own frontend would loop every MFA-enrolled clinician forever
	// through a handoff that always sends them right back — see
	// config.RequireDistinctHostedLoginOrigin's doc comment for why
	// nothing short of a boot refusal makes that loop unrepresentable.
	if err := cfg.RequireDistinctHostedLoginOrigin(); err != nil {
		return err
	}
	sessionKey := ed25519.NewKeyFromSeed(sessionSeed)
	// Logging a fingerprint of the PUBLIC key (never the private key,
	// never the seed) confirms the key loaded, the same way "resolved
	// environment" confirms HMS_ENV without printing a secret. The
	// public key itself is not secret either, but a fingerprint keeps
	// this line short and avoids training anyone to expect a raw key
	// value in a log line.
	sessionKeyFingerprint := sha256.Sum256(sessionKey.Public().(ed25519.PublicKey))
	slog.Info("session signing key loaded",
		"kid", sessionSigningKeyID,
		"issuer", cfg.SessionIssuer,
		// .String(), not the bare time.Duration: slog has no special case
		// for time.Duration, so an unconverted value renders as a raw
		// nanosecond int64 (900000000000 for the 15-minute default) —
		// observed to false-positive-match pkg/logging's 12-digit Aadhaar
		// pattern and come out as "[REDACTED:aadhaar]" instead of a
		// readable TTL. .String() ("15m0s") is unambiguous, readable, and
		// has no digit run long enough to trip any redaction pattern.
		"ttl", cfg.SessionTTL.String(),
		"public_key_fingerprint", hex.EncodeToString(sessionKeyFingerprint[:8]),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := tenantdb.Open(cfg.AppDatabaseURL, cfg.AdminDatabaseURL)
	if err != nil {
		return err
	}

	// Constructed once, before the registry, and handed to BOTH
	// bootstrap.NewRegistry (which threads it into iam.New) and
	// authn.Middleware below: the module's sign-out/revoke handlers and
	// its broadcast consumer must invalidate the SAME cache instance the
	// authentication path reads on every request. Two separately
	// constructed checkers would compile and pass most tests while the
	// middleware's cache silently never gets invalidated (#781).
	revocationChecker := iam.NewRevocationChecker(db)

	registry, err := bootstrap.NewRegistry(revocationChecker)
	if err != nil {
		return err
	}

	migs := bootstrap.PlatformMigrations()
	for _, m := range registry.All() {
		migs = append(migs, m.Migrations()...)
	}
	if err := db.Migrate(ctx, migs); err != nil {
		return err
	}
	if bad, err := db.LintRLS(ctx); err != nil {
		return err
	} else if len(bad) > 0 {
		return fmt.Errorf("tables missing forced RLS: %v", bad)
	}

	bus, err := events.NewBus(cfg.NATSURL)
	if err != nil {
		return err
	}
	defer bus.Close()

	// zitadelVerifier verifies a Zitadel ID token through standard OIDC
	// (plan Task 3). Task 4 narrows where that verifier is used: it is
	// ONLY the login endpoint's job now (see loginHandlers below) to
	// look at a raw Zitadel token at all. Every ordinary /v1 request
	// verifies an HMS session instead (requestVerifier, built from
	// sessionVerifier just below) — that is what ends the interim state
	// recorded in the plan's "Interim-state note" after Task 3, where
	// Principal.TenantID could only ever be empty because nothing yet
	// minted a session carrying a real one.
	zitadelVerifier, err := authn.NewZitadelVerifier(ctx, cfg.ZitadelIssuerURL, cfg.ZitadelClientID)
	if err != nil {
		return err
	}

	// sessionSigner mints HMS sessions (login, and Task 5's tenant
	// switch); sessionVerifier checks them. Built from the SAME
	// sessionKey decoded and refused-to-boot-without above, and the SAME
	// sessionSigningKeyID logged there — a Signer and Verifier
	// constructed from two different keys or kids would silently refuse
	// every session this process itself mints, which is exactly the kind
	// of self-inflicted outage a single shared source of truth for both
	// avoids.
	sessionSigner, err := session.NewSigner(sessionKey, sessionSigningKeyID, cfg.SessionIssuer, cfg.SessionTTL)
	if err != nil {
		return err
	}
	sessionVerifier, err := session.NewVerifier(sessionKey.Public().(ed25519.PublicKey), sessionSigningKeyID, cfg.SessionIssuer)
	if err != nil {
		return err
	}
	// requestVerifier is what bootstrap.V1Chain mounts in front of every
	// ordinary /v1 route (below). It refuses a raw Zitadel ID token
	// outright — see authn.NewSessionVerifier's doc comment for why that
	// is structural (wrong algorithm and wrong issuer), not a check this
	// code has to remember to make.
	requestVerifier := authn.NewSessionVerifier(sessionVerifier)

	fga, err := authz.NewClient(ctx, cfg.OpenFGAURL, cfg.OpenFGAStore)
	if err != nil {
		return err
	}
	if err := platform.Reconcile(ctx, registry, db, fga); err != nil {
		return err
	}

	// gin.Recovery() does not route through slog — gin builds its own
	// log.New(gin.DefaultErrorWriter, ...) rather than using log.Default(),
	// so slog.SetDefault's redirection of the standard library's log package
	// never reaches it, and a handler panic writes straight to os.Stderr with
	// whatever identifier triggered it still in the clear. This package's
	// thesis is that everything the process emits is screened, so this is
	// the one place gin's own writer has to be wrapped explicitly to keep
	// that true.
	gin.DefaultErrorWriter = logging.NewRedactingWriter(gin.DefaultErrorWriter)

	srv := httpserver.New(
		[]httpserver.ReadyCheck{
			{Name: "postgres", Check: db.PingContext},
			{Name: "nats", Check: bus.Ping},
			{Name: "openfga", Check: fga.Ping},
		},
		requestid.Middleware(),
	)
	// sessionSecureCookie is the SAME value passed to iam.NewLoginHandlers
	// below (!cfg.IsDev()) — a tenant switch re-mints the identical
	// session cookie login mints, so the two must never disagree on
	// `secure`. Computed once, here, so there is exactly one place this
	// could get out of sync rather than two call sites independently
	// negating cfg.IsDev().
	sessionSecureCookie := !cfg.IsDev()
	deps := platform.Deps{
		DB:                  db,
		Bus:                 bus,
		Authz:               fga,
		Roles:               fga,
		SessionSigner:       sessionSigner,
		SessionTTL:          cfg.SessionTTL,
		SessionSecureCookie: sessionSecureCookie,
		Reconcile: func(ctx context.Context, tenantID string) error {
			return platform.ReconcileTenant(ctx, registry, fga, tenantID)
		},
	}

	// The chain itself lives in bootstrap.V1Chain, not inline here, so
	// internal/archtest can build its harness from the SAME function that
	// serves production traffic. Inline, the limiter could be deleted from
	// this list and every test would still pass, because the placement
	// test built its own equivalent chain. See V1Chain's comment for the
	// order and why it is load-bearing;
	// internal/archtest.TestThrottledRequestMakesNoOpenFGACall pins it.
	//
	// requestVerifier (an HMS session, never a raw Zitadel token) is what
	// gates every route in this group.
	limiter := ratelimit.NewMemory(10_000)
	api := platform.NewRouter(
		srv.Engine.Group("/v1", bootstrap.V1Chain(requestVerifier, revocationChecker, limiter, cfg, fga)...),
		fga,
	)

	// POST /v1/auth/login is mounted directly on the raw engine — NOT
	// through `api`/bootstrap.V1Chain above — because it is the endpoint
	// that CREATES an HMS session (plan Task 4, spec D1) and so cannot
	// itself require one: it verifies a caller-presented Zitadel ID
	// token, not the HMS session requestVerifier checks. It shares the
	// /v1 URL prefix for API-path consistency but is structurally outside
	// the authenticated chain — no authn.Middleware, no
	// authz.RequireMembership, no rate-limit bucket keyed by a principal
	// that does not exist yet — because gin routes registered directly on
	// the engine are independent of any *gin.RouterGroup built over it,
	// even one sharing the same path prefix.
	// limiter is the SAME instance V1Chain above uses — #841 reuses it
	// rather than building a second one, keyed under its own "login:"
	// prefix (see login.go) so it cannot bleed into the Principal bucket
	// authenticated routes read from. LoginRateLimitRule is
	// bootstrap-owned for the same reason RateLimitConfig is: one
	// construction path both production and internal/archtest read.
	//
	// Sessions is the SAME sessionVerifier requestVerifier wraps for the
	// /v1 chain, reused rather than built a second time: it is not an
	// authentication gate here (this endpoint runs before a session
	// exists) but the renewal-vs-genuine-login discriminator spec D3
	// turns on — see iam's idleDeadlineFor. A second Verifier built from
	// a different key or kid would silently make every renewal look like
	// a genuine login and re-open the idle window each time, which is
	// exactly the #848 failure that looks like success.
	loginHandlers := iam.NewLoginHandlers(iam.LoginDeps{
		Zitadel:      zitadelVerifier,
		Roles:        fga,
		Signer:       sessionSigner,
		Sessions:     sessionVerifier,
		TTL:          cfg.SessionTTL,
		IdleTimeout:  idleTimeout,
		SecureCookie: sessionSecureCookie,
		Limiter:      limiter,
		Limit:        bootstrap.LoginRateLimitRule(cfg),
	})

	// zitadelLoginClient speaks Zitadel's v2 login-client API
	// (internal/modules/iam/loginclient), authenticated with the PAT
	// resolved (and refused-to-boot-without) above. baseURL is
	// cfg.ZitadelIssuerURL — the SAME Zitadel origin zitadelVerifier's
	// OIDC discovery uses — because loginclient's endpoints
	// (/v2/sessions, /v2/oidc/auth_requests/…) live on Zitadel's core
	// API, not a separate host. http.DefaultClient matches every other
	// loginclient.New call site in this codebase (loginui_test.go's
	// newZitadelTestClient uses the test server's own equivalent);
	// loginclient.defaultTimeout bounds every call it makes, so this
	// does not need its own per-request timeout.
	zitadelLoginClient := loginclient.New(cfg.ZitadelIssuerURL, zitadelLoginClientToken, http.DefaultClient)
	// loginUIHandlers backs HMS's own login form (plan #854 Task 4):
	// three unauthenticated routes reading an auth request (GET
	// /v1/auth/login/request/:id), checking a password (POST
	// /v1/auth/login/password), and handing off to Zitadel's hosted UI
	// when HMS cannot complete the login itself (POST
	// /v1/auth/login/handoff/:id). All three reuse the SAME limiter
	// instance as loginHandlers and V1Chain above (#841's rule: this
	// file must construct exactly one ratelimit.Limiter, never a second)
	// — see NewLoginUIHandlers' own doc comment on why sharing the
	// limiter is still safe: each route keys its bucket under its own
	// prefix (login_auth_request:, login_password:, login_handoff:) —
	// distinct from each other AND from both LoginHandlers.Login's
	// "login:" bucket and ratelimit.Middleware's Principal bucket, so
	// none of the five can bleed into another's budget despite sharing
	// one Limiter. The reason the buckets are separate: a shared key
	// would let credential-guessing traffic exhaust the same budget as
	// merely LOADING the login form, so an attacker (or one clinician
	// mistyping their password repeatedly) could lock people out of the
	// sign-in page itself — a self-inflicted denial of service on the
	// sign-in path that buckets are distinct specifically to prevent.
	// The Rule itself is bootstrap.LoginRateLimitRule(cfg) too — these
	// endpoints do not yet warrant a budget shaped differently from
	// POST /v1/auth/login's (all are "one browser tab's worth of login
	// traffic"), so a second RATE_LIMIT_* knob would be configuration
	// nobody has a reason to set independently; revisit if that changes.
	loginUIHandlers := iam.NewLoginUIHandlers(zitadelLoginClient, cfg.ZitadelHostedLoginURL, limiter, bootstrap.LoginRateLimitRule(cfg))

	// Mounted through bootstrap so the bypass is declared in one
	// enumerable place and pinned by
	// archtest.TestEveryEngineRouteIsDeclaredOrAllowlisted — a route on the
	// raw engine otherwise escapes platform.Router entirely.
	// authRequest/password/handoff are now the real loginUIHandlers
	// methods (Task 5) — MountUnauthenticated's nil guard is what made
	// `go run ./cmd/api` refuse to boot on this branch before this
	// commit, per its own doc comment; this is the fix.
	bootstrap.MountUnauthenticated(srv.Engine, loginHandlers.Login,
		loginUIHandlers.AuthRequest, loginUIHandlers.Password, loginUIHandlers.Handoff)

	for _, m := range registry.All() {
		m.Routes(api, deps)
		if err := bus.StartConsumers(ctx, db, m.Consumers(deps)); err != nil {
			return err
		}
		if err := bus.StartBroadcasts(ctx, m.Broadcasts(deps)); err != nil {
			return err
		}
	}
	go bus.RunDispatcher(ctx, db)
	// A separate loop and ticker from the dispatcher's 500ms tick (#835
	// Task 2, spec D3): pruning belongs on its own hourly cadence, not
	// coupled to publish throughput.
	go bus.RunPruner(ctx, db)

	httpSrv := &http.Server{Addr: ":" + cfg.Port, Handler: srv.Engine, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	slog.Info("api listening", "port", cfg.Port)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
