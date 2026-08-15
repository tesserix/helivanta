# ADR-0002: Google Identity Platform, not Keycloak

- **Status:** **Superseded (2026-08-15) by [ADR-0006](0006-zitadel-not-gip.md).**
  HMS authentication uses Zitadel. Everything below is retained as the record of
  why GIP was chosen and is **no longer the design** — read it as history, not as
  instruction. In particular the `tenant_id` custom claim, the Firebase Admin SDK
  verification path, and the custom-token tenant switch it implies are all being
  replaced (#838).
- **Status when accepted:** Accepted (2026-08-04)
- **Context:** Foundational issues say "Keycloak/GIP" interchangeably. The
  org already runs per-product GIP tenants on tesseracthub-480811 with
  canonical onboarding scripts in tesserix-k8s (docs/identity/).
- **Decision:** All HMS authentication uses GIP. Frontends use the Firebase
  Web/native SDKs (emulator locally); the Go API verifies GIP ID tokens via
  the Firebase Admin SDK and requires a `tenant_id` custom claim. Keycloak
  is not deployed. Where issues name Keycloak, read GIP.
- **Consequences:** no self-hosted IdP to operate; MFA/OTP/passkeys come
  from GIP features; per-hospital GIP tenants follow the existing
  enable-tenant-google-idp.py flow. Follow-on: swap the raw-ID-token
  session cookie for Firebase session cookies before production.
