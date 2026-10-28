import type { NextConfig } from "next";

import { SECURITY_HEADERS } from "./lib/security-headers";

import { globalApiURL } from "./lib/global-api-url";

const API_URL = globalApiURL(process.env.API_URL, process.env.NODE_ENV === "production");

const nextConfig: NextConfig = {
  output: "standalone",
  transpilePackages: ["@helivanta/ui", "@helivanta/api", "@helivanta/emergency-access"],
  poweredByHeader: false,
  async headers() {
    return [
      { source: "/:path*", headers: [...SECURITY_HEADERS] },
      {
        source: "/api/emergency/access",
        headers: [{ key: "Referrer-Policy", value: "no-referrer" }],
      },
    ];
  },
  async rewrites() {
    return [
      // Same-origin API (spec D6): browser calls /api/*, Global API routes /v1/* to its owner.
      { source: "/api/:path*", destination: `${API_URL}/:path*` },
    ];
  },
};

export default nextConfig;
