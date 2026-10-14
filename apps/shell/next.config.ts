import type { NextConfig } from "next";

import { SECURITY_HEADERS } from "./lib/security-headers";

const API_URL = process.env.API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  transpilePackages: ["@helivanta/ui", "@helivanta/api"],
  poweredByHeader: false,
  async headers() {
    return [{ source: "/:path*", headers: [...SECURITY_HEADERS] }];
  },
  async rewrites() {
    return [
      // Same-origin API (spec D6): browser calls /api/*, backend serves /v1/*.
      { source: "/api/:path*", destination: `${API_URL}/:path*` },
    ];
  },
};

export default nextConfig;
