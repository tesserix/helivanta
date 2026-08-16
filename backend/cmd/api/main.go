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
	loginHandlers := iam.NewLoginHandlers(zitadelVerifier, fga, sessionSigner, cfg.SessionTTL, sessionSecureCookie, limiter, bootstrap.LoginRateLimitRule(cfg))
	// Mounted through bootstrap so the bypass is declared in one
	// enumerable place and pinned by
	// archtest.TestEveryEngineRouteIsDeclaredOrAllowlisted — a route on the
	// raw engine otherwise escapes platform.Router entirely.
	// authRequest/password/handoff are nil here because Task 5, not this
	// commit, constructs loginclient.Client and iam.LoginUIHandlers (it
	// needs the login-client PAT, plumbed through config in that same
	// task). MountUnauthenticated PANICS on a nil handler rather than
	// silently skipping the route (see its own doc comment on why) — so
	// until Task 5 lands, `go run ./cmd/api` fails at boot rather than
	// serving /v1/auth/login/* incompletely. That is the intended,
	// visible state of an in-flight branch between these two tasks, not
	// a bug: a route this repo already promises is reachable
	// (bootstrap.UnauthenticatedRoutes) must not be able to silently NOT
	// be one.
	bootstrap.MountUnauthenticated(srv.Engine, loginHandlers.Login, nil, nil, nil)

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
