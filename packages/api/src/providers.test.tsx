import { render, screen } from "@testing-library/react";
import { act, useEffect } from "react";
import { QueryClient, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AppProviders } from "./providers";
import { sessionChangeSource } from "./session-change";

beforeEach(() => vi.stubGlobal("location", { replace: vi.fn() }));
afterEach(() => vi.unstubAllGlobals());

it("rechecks private workspace state when a page is restored from browser history", () => {
  render(
    <AppProviders>
      <p>Synthetic restored staff record</p>
    </AppProviders>,
  );
  act(() => window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: true })));
  expect(screen.queryByText("Synthetic restored staff record")).not.toBeInTheDocument();
  expect(window.location.replace).toHaveBeenCalledExactlyOnceWith("/login");
});

it("preserves public form input while dropping old session queries", () => {
  let cachedClient: QueryClient | undefined;
  function PublicForm() {
    const client = useQueryClient();
    useEffect(() => {
      cachedClient = client;
      client.setQueryData(["old-session"], "synthetic private data");
    }, [client]);
    return <input aria-label="Public form input" defaultValue="Synthetic draft" />;
  }
  render(
    <AppProviders redirectOnSessionChange={false}>
      <PublicForm />
    </AppProviders>,
  );
  act(() =>
    window.dispatchEvent(
      new StorageEvent("storage", {
        key: "hms.session-change.v1",
        newValue: "another change",
        storageArea: window.localStorage,
      }),
    ),
  );
  expect(screen.getByLabelText("Public form input")).toHaveValue("Synthetic draft");
  expect(cachedClient?.getQueryData(["old-session"])).toBeUndefined();
  expect(window.location.replace).not.toHaveBeenCalled();
});

it("hides the workspace over BroadcastChannel when browser storage is unavailable", () => {
  const receivers: EventTarget[] = [];
  const close = vi.fn();
  class Channel extends EventTarget {
    constructor() {
      super();
      receivers.push(this);
    }
    close = close;
  }
  vi.stubGlobal("BroadcastChannel", Channel);
  const storage = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
    throw new Error("storage disabled");
  });
  try {
    const { unmount } = render(
      <AppProviders>
        <p>Synthetic confidential record</p>
      </AppProviders>,
    );
    act(() =>
      receivers[0]?.dispatchEvent(
        new MessageEvent("message", {
          data: { type: "changed", source: sessionChangeSource() },
        }),
      ),
    );
    expect(screen.getByText("Synthetic confidential record")).toBeInTheDocument();
    act(() =>
      receivers[0]?.dispatchEvent(
        new MessageEvent("message", {
          data: { type: "changed", source: "11111111-1111-4111-8111-111111111111" },
        }),
      ),
    );
    expect(screen.queryByText("Synthetic confidential record")).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Your session changed in another tab");
    expect(window.location.replace).toHaveBeenCalledExactlyOnceWith("/login");
    unmount();
    expect(close).toHaveBeenCalledOnce();
  } finally {
    storage.mockRestore();
    vi.unstubAllGlobals();
  }
});

it("hides private workspace data and clears queries when another tab changes session", () => {
  let cachedClient: QueryClient | undefined;
  function Workspace() {
    const client = useQueryClient();
    useEffect(() => {
      cachedClient = client;
      client.setQueryData(["synthetic-staff"], { name: "Synthetic private enrollment" });
    }, [client]);
    return <p>Synthetic private enrollment</p>;
  }
  render(
    <AppProviders>
      <Workspace />
    </AppProviders>,
  );
  expect(cachedClient?.getQueryData(["synthetic-staff"])).toEqual({
    name: "Synthetic private enrollment",
  });
  act(() => {
    window.dispatchEvent(
      new StorageEvent("storage", {
        key: "hms.session-change.v1",
        newValue: "synthetic-session-change",
        storageArea: window.localStorage,
      }),
    );
  });
  expect(screen.queryByText("Synthetic private enrollment")).not.toBeInTheDocument();
  expect(cachedClient?.getQueryData(["synthetic-staff"])).toBeUndefined();
  expect(screen.getByRole("status")).toHaveTextContent("Your session changed in another tab");
});

// sonner's Toaster only renders its [data-sonner-toaster] element once at
// least one toast exists (see sonner/dist/index.mjs — `if
// (!filteredToasts.length) return null`), and the toast store notifies
// subscribers on a timer tick rather than synchronously, so each assertion
// fires a toast and lets a macrotask flush before inspecting the DOM.
async function fireToastAndFlush() {
  await act(async () => {
    toast("theme check");
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

describe("AppProviders toaster", () => {
  afterEach(() => {
    document.documentElement.dataset.theme = "";
  });

  // The app is light only (#1255, spec D1): a stale dark attribute must not
  // turn the toasts dark.
  it("renders the Toaster light even under a dark data-theme", async () => {
    document.documentElement.dataset.theme = "dark";
    render(<AppProviders>{null}</AppProviders>);
    await fireToastAndFlush();
    const toaster = document.querySelector("[data-sonner-toaster]");
    expect(toaster).toHaveAttribute("data-sonner-theme", "light");
  });
});
