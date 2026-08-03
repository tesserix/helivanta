import type { NextConfig } from "next";

const MEDICORE_URL = process.env.MEDICORE_URL ?? "http://localhost:4302";
const API_URL = process.env.API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  async rewrites() {
    return [
      // Zone stitching (spec D2): shell owns "/" and forwards zone paths.
      { source: "/medicore", destination: `${MEDICORE_URL}/medicore` },
      { source: "/medicore/:path*", destination: `${MEDICORE_URL}/medicore/:path*` },
      // Same-origin API (spec D6): browser calls /api/*, backend serves /v1/*.
      { source: "/api/:path*", destination: `${API_URL}/:path*` },
    ];
  },
};

export default nextConfig;
