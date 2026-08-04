import type { NextConfig } from "next";

const API_URL = process.env.API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  basePath: "/lab",
  transpilePackages: ["@hms/ui"],
  async rewrites() {
    // Only used when hitting :4304 directly; via the shell the same
    // /api/* path is rewritten by the shell itself.
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
