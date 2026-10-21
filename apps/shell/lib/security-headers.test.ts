import { describe, expect, it } from "vitest";

import nextConfig from "../next.config";

describe("shell security headers", () => {
  it("applies every security header to every path", async () => {
    const rules = await nextConfig.headers!();
    const all = rules.find((rule) => rule.source === "/:path*");
    const headers = Object.fromEntries((all?.headers ?? []).map((h) => [h.key, h.value]));

    expect(headers).toEqual({
      "Strict-Transport-Security": "max-age=31536000; includeSubDomains",
      "X-Content-Type-Options": "nosniff",
      "X-Frame-Options": "DENY",
      "Referrer-Policy": "strict-origin-when-cross-origin",
      "Permissions-Policy": "camera=(), microphone=(), geolocation=(), payment=()",
    });
    expect(nextConfig.poweredByHeader).toBe(false);
  });
});
