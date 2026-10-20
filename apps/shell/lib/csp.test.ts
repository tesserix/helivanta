import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { SONNER_STYLE_HASH, contentSecurityPolicy, createNonce } from "./csp";

describe("contentSecurityPolicy", () => {
  it("builds the production policy", () => {
    expect(contentSecurityPolicy("abc", { dev: false, issuerOrigin: "https://auth.example" })).toBe(
      [
        "default-src 'self'",
        "script-src 'self' 'nonce-abc' 'strict-dynamic'",
        `style-src 'self' 'nonce-abc' ${SONNER_STYLE_HASH}`,
        "style-src-attr 'unsafe-inline'",
        "img-src 'self' data: blob: https:",
        "font-src 'self'",
        "connect-src 'self' https://auth.example",
        "object-src 'none'",
        "base-uri 'self'",
        "form-action 'self'",
        "frame-ancestors 'none'",
        "report-uri /api/csp-report",
      ].join("; "),
    );
  });

  it("adds unsafe-eval only in development", () => {
    const scriptSrc = (dev: boolean) =>
      contentSecurityPolicy("abc", { dev, issuerOrigin: "https://auth.example" })
        .split("; ")
        .find((directive) => directive.startsWith("script-src "));
    expect(scriptSrc(true)).toContain("'unsafe-eval'");
    expect(scriptSrc(false)).not.toContain("'unsafe-eval'");
  });

  // sonner injects this stylesheet at module load with no nonce support, so
  // the policy allows it by hash. An upgrade that changes the CSS fails here
  // rather than silently unstyling every toast.
  it("allows sonner's injected stylesheet by hash", () => {
    const fromApi = createRequire(resolve(__dirname, "../../../packages/api/package.json"));
    const source = readFileSync(join(dirname(fromApi.resolve("sonner")), "index.mjs"), "utf8");
    const literals = [...source.matchAll(/__insertCSS\("((?:[^"\\]|\\.)*)"\)/g)];
    expect(literals).toHaveLength(1);
    const css = JSON.parse(`"${literals[0]![1]}"`) as string;
    const hash = createHash("sha256").update(css).digest("base64");
    expect(SONNER_STYLE_HASH).toBe(`'sha256-${hash}'`);
  });

  it("mints a distinct 16-byte nonce", () => {
    const [a, b] = [createNonce(), createNonce()];
    expect(a).not.toBe(b);
    for (const nonce of [a, b]) expect(Buffer.from(nonce, "base64")).toHaveLength(16);
  });
});
