// The shell's script and style policy (#1113, spec
// docs/superpowers/specs/2026-10-20-nonce-csp-design.md). Built per request by
// middleware.ts; no "use client" and no Node-only imports, since middleware runs
// on the edge runtime.

export const CSP_REPORT_PATH = "/api/csp-report";

// sha256 of the stylesheet sonner inserts at module load (sonner 2.0.8). It has
// no nonce support; csp.test.ts recomputes this from the installed package.
export const SONNER_STYLE_HASH = "'sha256-StEaX+se6YS7pqjzrzMIA0KaX9zF/8zAhvQXZAe5epY='";

export function createNonce(): string {
  return btoa(String.fromCharCode(...crypto.getRandomValues(new Uint8Array(16))));
}

export function contentSecurityPolicy(
  nonce: string,
  { dev, issuerOrigin }: { dev: boolean; issuerOrigin: string },
): string {
  return [
    "default-src 'self'",
    // React needs eval in development only, to rebuild server error stacks.
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'${dev ? " 'unsafe-eval'" : ""}`,
    `style-src 'self' 'nonce-${nonce}' ${SONNER_STYLE_HASH}`,
    // Server-rendered style="" attributes cannot carry a nonce; an injected
    // attribute styles only its own element, unlike an injected <style>.
    "style-src-attr 'unsafe-inline'",
    // Tenant logos are arbitrary https URLs (validLogo in the workspace module).
    "img-src 'self' data: blob: https:",
    "font-src 'self'",
    // oidc-client-ts calls Zitadel's discovery and token endpoints from the browser.
    `connect-src 'self' ${issuerOrigin}`,
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
    `report-uri ${CSP_REPORT_PATH}`,
  ].join("; ");
}
