import type { NextConfig } from "next";

const API_URL = process.env.API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  basePath: "/medicore",
  async rewrites() {
    // Only used when hitting :4302 directly; via the shell the same
    // /api/* path is rewritten by the shell itself.
    return [{ source: "/api/:path*", destination: `${API_URL}/:path*` }];
  },
};

export default nextConfig;
