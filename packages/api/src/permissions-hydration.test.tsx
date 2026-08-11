import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import type { ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToString } from "react-dom/server";
import { hydrateRoot } from "react-dom/client";
import { Can } from "./permissions";
import { PERMISSIONS_CACHE_KEY } from "./permissions-cache";

// Zone apps prerender this tree on the server, where localStorage does not
// exist, so `usePermissions` reads the cache in a layout effect rather than
// during render. Every other suite in this package renders client-only via
// `renderWithProviders`, which cannot see the difference: moving the read
// into the render body would keep them all green while producing a
// hydration mismatch on every zone load in production. This suite is the
// one that notices — it server-renders with no cache reachable (as the real
// server has none), then hydrates with the cache populated, and fails if
// React reports a mismatch.

function seedCache(permissions: string[]): void {
  window.localStorage.setItem(
    PERMISSIONS_CACHE_KEY,
    JSON.stringify({
      subject: "doc",
      tenantId: "tenant-a",
      permissions,
      storedAt: Date.now(),
    }),
  );
}

// A marked, stable sibling so a cache-driven extra child is an unambiguous
// element mismatch rather than a whitespace/text nuance React may forgive.
function tree(client: QueryClient): ReactElement {
  return (
    <QueryClientProvider client={client}>
      <nav>
        <Can permission="pharmacy.dispense.fulfil">
          <span data-testid="gated">Dispense</span>
        </Can>
        <span data-testid="static">Dashboard</span>
      </nav>
    </QueryClientProvider>
  );
}

function isHydrationError(value: unknown): boolean {
  const text = value instanceof Error ? `${value.message}\n${value.stack ?? ""}` : String(value);
  return /hydrat/i.test(text);
}

describe("usePermissions server render then hydrate", () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
    // The request must never settle, so anything painted comes from the
    // cache and nothing races the hydration assertions.
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise(() => {})),
    );
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    window.localStorage.clear();
  });

  it("hydrates a server-rendered tree without a hydration mismatch when the cache is populated", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    // Server pass: no cache is reachable, exactly as in a Next prerender.
    const html = renderToString(tree(client));
    expect(html).not.toContain("Dispense");

    // Client pass: the browser does have the cached set. A render-time read
    // would make this render disagree with the HTML above.
    seedCache(["pharmacy.dispense.fulfil"]);

    const container = document.createElement("div");
    container.innerHTML = html;
    document.body.appendChild(container);

    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const recoverable: unknown[] = [];

    await act(async () => {
      hydrateRoot(container, tree(client), {
        onRecoverableError: (error: unknown) => recoverable.push(error),
      });
    });

    const consoleHydrationErrors = consoleError.mock.calls.filter((args) =>
      args.some(isHydrationError),
    );
    expect(recoverable.filter(isHydrationError)).toEqual([]);
    expect(consoleHydrationErrors).toEqual([]);

    // The cached set must still reach the DOM after hydration commits —
    // otherwise this suite could pass simply by never painting anything.
    expect(container.querySelector('[data-testid="gated"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="static"]')).not.toBeNull();
  });
});
