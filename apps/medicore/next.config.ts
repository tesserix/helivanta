import type { NextConfig } from "next";

const API_URL = process.env.API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  basePath: "/medicore",
  async rewrites() {
    // Only used when hitting :4302 directly; via the shell the same
    // /api/* path is rewritten by the shell itself.
    // basePath: false keeps Next from auto-prefixing the source with
    // basePath — without it this rewrite mis-mounts at /medicore/api/*.
    return [
      {
        source: "/api/:path*",
        destination: `${API_URL}/:path*`,
        basePath: false,
      },
    ];
  },
};

export default nextConfig;
