#!/usr/bin/env node
/* eslint-disable no-console */
// Zero-dependency generator that stamps a standards-compliant zone app under
// apps/<name>, mirroring the shape of apps/pharmacy (package.json, next.config,
// tsconfig, eslint/vitest configs, AppProviders layout, boundary files, and a
// minimal example panel + test).
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dirname, "..");
const appsDir = join(root, "apps");

function fail(message) {
  console.error(`Error: ${message}`);
  process.exit(1);
}

function readArgs() {
  const name = process.argv[2];
  if (!name) fail("usage: pnpm new-zone <name>");
  if (!/^[a-z][a-z0-9-]*$/.test(name)) {
    fail(`invalid zone name "${name}" — must match /^[a-z][a-z0-9-]*$/`);
  }
  if (existsSync(join(appsDir, name))) {
    fail(`apps/${name} already exists`);
  }
  return name;
}

function nextPort() {
  let max = 0;
  for (const entry of readdirSync(appsDir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const pkgPath = join(appsDir, entry.name, "package.json");
    if (!existsSync(pkgPath)) continue;
    const pkg = JSON.parse(readFileSync(pkgPath, "utf8"));
    const devScript = pkg.scripts?.dev ?? "";
    const match = devScript.match(/-p (\d+)/);
    if (match) max = Math.max(max, Number(match[1]));
  }
  return max + 1;
}

function writeFile(path, content) {
  mkdirSync(join(path, ".."), { recursive: true });
  writeFileSync(path, content);
}

function packageJson(name, port) {
  return `{
  "name": "@hms/${name}",
  "version": "0.0.0",
  "private": true,
  "scripts": {
    "dev": "next dev -p ${port}",
    "build": "next build",
    "start": "next start -p ${port}",
    "type-check": "tsc --noEmit",
    "lint": "eslint .",
    "test": "vitest run"
  },
  "dependencies": {
    "@hms/api": "workspace:*",
    "@hms/ui": "workspace:*",
    "@tesserix/web": "^1.8.0",
    "lucide-react": "^0.469.0",
    "next": "^16.0.0",
    "react": "^19.0.0",
    "react-dom": "^19.0.0",
    "zod": "^3.24.1"
  },
  "devDependencies": {
    "@hms/config": "workspace:*",
    "@tailwindcss/postcss": "^4.1.0",
    "@testing-library/jest-dom": "^6.10.0",
    "@testing-library/react": "^16.1.0",
    "@testing-library/user-event": "^14.5.2",
    "@types/node": "^22",
    "@types/react": "^19",
    "@vitejs/plugin-react": "^4.3.4",
    "eslint": "^9.18.0",
    "tailwindcss": "^4.1.0",
    "typescript": "^5.7.0",
    "vitest": "^3.0.0"
  }
}
`;
}

function nextConfig(name) {
  return `import type { NextConfig } from "next";

const API_URL = process.env.API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  basePath: "/${name}",
  transpilePackages: ["@hms/ui", "@hms/api"],
  async rewrites() {
    // Only used when hitting the zone port directly; via the shell the same
    // /api/* path is rewritten by the shell itself.
    return [
      {
        source: "/api/:path*",
        destination: \`\${API_URL}/:path*\`,
        basePath: false,
      },
    ];
  },
};

export default nextConfig;
`;
}

function tsconfigJson() {
  return `{
  "extends": "@hms/config/tsconfig.base.json",
  "compilerOptions": {
    "plugins": [{ "name": "next" }],
    "paths": { "@/*": ["./*"] },
    "types": ["vitest/globals", "@testing-library/jest-dom/vitest"]
  },
  "include": ["next-env.d.ts", "**/*.ts", "**/*.tsx", ".next/types/**/*.ts"],
  "exclude": ["node_modules"]
}
`;
}

function postcssConfig() {
  return `export default { plugins: { "@tailwindcss/postcss": {} } };
`;
}

function eslintConfig() {
  return `import { hmsEslint } from "@hms/config/eslint";

export default hmsEslint(import.meta.dirname);
`;
}

function vitestConfig() {
  return `import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import { hmsVitest } from "@hms/config/vitest";

export default defineConfig({ plugins: [react()], ...hmsVitest() });
`;
}

function envExample() {
  return `# Server-side rewrite target for direct-hit /api/* (bypassing the shell)
API_URL=http://localhost:8080
`;
}

function globalsCss() {
  return `@import "tailwindcss";
@import "@tesserix/web/styles";
@import "@hms/ui/styles.css";

/* Tailwind v4 must scan the design system for emitted class names. */
@source "../node_modules/@tesserix/web/dist";

/* Scan the shared UI package for emitted class names. */
@source "../../../packages/ui/src";
`;
}

function layoutTsx() {
  return `import type { Metadata } from "next";
import { AppProviders } from "@hms/api";
import "./globals.css";

export const metadata: Metadata = { title: "HMS" };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" data-theme="default">
      <body>
        <AppProviders>{children}</AppProviders>
      </body>
    </html>
  );
}
`;
}

function errorTsx() {
  return `"use client";

import { Button } from "@tesserix/web";

export default function ErrorBoundary({
  error,
  reset,
}: {
  error: Error;
  reset: () => void;
}) {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-4 p-6 text-center">
      <h1 className="text-xl font-semibold text-foreground">Something went wrong</h1>
      <p className="max-w-md text-sm text-muted-foreground">
        {error.message || "An unexpected error occurred."}
      </p>
      <Button onClick={reset}>Try again</Button>
    </main>
  );
}
`;
}

function loadingTsx() {
  return `"use client";

import { Skeleton } from "@tesserix/web";

export default function Loading() {
  return (
    <main className="p-6">
      <Skeleton className="h-32 w-full max-w-2xl" />
    </main>
  );
}
`;
}

function notFoundTsx() {
  return `export default function NotFound() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-2 p-6 text-center">
      <h1 className="text-xl font-semibold text-foreground">Page not found</h1>
      <p className="text-sm text-muted-foreground">
        Check the address, or head back to the <a className="underline underline-offset-4" href="/">dashboard</a>.
      </p>
    </main>
  );
}
`;
}

function pageTsx(name) {
  return `import { HmsShell } from "@hms/ui";
import { ExamplePanel } from "@/components/example-panel";

export default function ${pascalCase(name)}Page() {
  return (
    <HmsShell active="/${name}">
      <ExamplePanel />
    </HmsShell>
  );
}
`;
}

function examplePanelTsx(name) {
  return `"use client";

import { Inbox } from "lucide-react";
import { useApiQuery } from "@hms/api";
import { EmptyState } from "@hms/ui";

type ExampleItem = { id: string; name: string };

export function ExamplePanel() {
  const items = useApiQuery<{ data: ExampleItem[] }>(["${name}-items"], "/${name}/items");

  return (
    <section className="max-w-2xl rounded-lg border bg-card">
      <div className="border-b px-5 py-4">
        <h2 className="text-sm font-semibold text-foreground">${pascalCase(name)}</h2>
        <p className="mt-0.5 text-sm text-muted-foreground">Replace this example panel with real content.</p>
      </div>
      <ul className="divide-y text-sm">
        {items.data?.data.length === 0 && (
          <li>
            <EmptyState icon={Inbox} title="Nothing here yet" hint="Data will appear here once available." />
          </li>
        )}
        {items.data?.data.map((item) => (
          <li key={item.id} className="px-5 py-3">
            {item.name}
          </li>
        ))}
      </ul>
    </section>
  );
}
`;
}

function examplePanelTestTsx() {
  return `import { screen } from "@testing-library/react";
import { describe, expect, it, vi, afterEach } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import { ExamplePanel } from "./example-panel";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => vi.unstubAllGlobals());

describe("ExamplePanel", () => {
  it("shows the empty state", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(200, { data: [] })));
    renderWithProviders(<ExamplePanel />);
    expect(await screen.findByText(/Nothing here yet/)).toBeInTheDocument();
  });
});
`;
}

function pascalCase(name) {
  return name
    .split("-")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join("");
}

function printFollowUps(name, port) {
  const envVar = name.toUpperCase().replace(/-/g, "_") + "_URL";
  console.warn(`Created apps/${name} on port ${port}. Manual follow-ups:
1. apps/shell/next.config.ts — add:
   const ${envVar} = process.env.${envVar} ?? "http://localhost:${port}";
   { source: "/${name}", destination: \`\${${envVar}}/${name}\` },
   { source: "/${name}/:path*", destination: \`\${${envVar}}/${name}/:path*\` },
2. packages/ui/src/zones.ts — add a Zone entry (icon + pages).
3. README/Makefile — mention the new port.
Then: pnpm install && pnpm turbo lint type-check test build --filter=@hms/${name}`);
}

function main() {
  const name = readArgs();
  const port = nextPort();
  const appDir = join(appsDir, name);

  writeFile(join(appDir, "package.json"), packageJson(name, port));
  writeFile(join(appDir, "next.config.ts"), nextConfig(name));
  writeFile(join(appDir, "tsconfig.json"), tsconfigJson());
  writeFile(join(appDir, "postcss.config.mjs"), postcssConfig());
  writeFile(join(appDir, "eslint.config.mjs"), eslintConfig());
  writeFile(join(appDir, "vitest.config.mts"), vitestConfig());
  writeFile(join(appDir, ".env.example"), envExample());

  writeFile(join(appDir, "app/globals.css"), globalsCss());
  writeFile(join(appDir, "app/layout.tsx"), layoutTsx());
  writeFile(join(appDir, "app/error.tsx"), errorTsx());
  writeFile(join(appDir, "app/loading.tsx"), loadingTsx());
  writeFile(join(appDir, "app/not-found.tsx"), notFoundTsx());
  writeFile(join(appDir, "app/page.tsx"), pageTsx(name));

  writeFile(join(appDir, "components/example-panel.tsx"), examplePanelTsx(name));
  writeFile(join(appDir, "components/example-panel.test.tsx"), examplePanelTestTsx());

  printFollowUps(name, port);
}

main();
