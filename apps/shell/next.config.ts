import type { NextConfig } from "next";

const MEDICORE_URL = process.env.MEDICORE_URL ?? "http://localhost:4302";
const PHARMACY_URL = process.env.PHARMACY_URL ?? "http://localhost:4303";
const LAB_URL = process.env.LAB_URL ?? "http://localhost:4304";
const API_URL = process.env.API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  transpilePackages: ["@helivanta/ui", "@helivanta/api"],
  async rewrites() {
    return [
      // Zone stitching (spec D2): shell owns "/" and forwards zone paths.
      { source: "/medicore", destination: `${MEDICORE_URL}/medicore` },
      { source: "/medicore/:path*", destination: `${MEDICORE_URL}/medicore/:path*` },
      { source: "/pharmacy", destination: `${PHARMACY_URL}/pharmacy` },
      { source: "/pharmacy/:path*", destination: `${PHARMACY_URL}/pharmacy/:path*` },
      { source: "/lab", destination: `${LAB_URL}/lab` },
      { source: "/lab/:path*", destination: `${LAB_URL}/lab/:path*` },
      // Same-origin API (spec D6): browser calls /api/*, backend serves /v1/*.
      { source: "/api/:path*", destination: `${API_URL}/:path*` },
    ];
  },
};

export default nextConfig;
