import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useApiMutation, useApiQuery } from "./hooks";
import { renderWithProviders } from "./testing";

function QueryProbe() {
  const q = useApiQuery<{ data: { id: string }[] }>(["probe"], "/probe");
  if (q.isPending) return <p>loading</p>;
  if (q.isError) return <p role="alert">{q.error.message}</p>;
  return <p>{q.data.data[0].id}</p>;
}

function ScopedQueryProbe() {
  useApiQuery(["scoped-probe", "TEST-tenant"], "/probe", {
    request: { headers: { "X-Helivanta-Expected-Tenant": "TEST-tenant" } },
  });
  return <p>Scoped request</p>;
}

function MutationProbe() {
  const m = useApiMutation(() => Promise.reject(new Error("nope")));
  return <button onClick={() => m.mutate(undefined)}>go</button>;
}

function SilentMutationProbe() {
  const m = useApiMutation(() => Promise.reject(new Error("shh")), { suppressErrorToast: true });
  return (
    <>
      <button onClick={() => m.mutate(undefined)}>go</button>
      {/* The caller's own error surface — proves suppressErrorToast does
          not disable error reporting altogether, only the automatic
          toast. */}
      {m.error ? <p role="alert">{m.error.message}</p> : null}
    </>
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("useApiQuery", () => {
  it("pins the expected tenant and aborts an in-flight request when unmounted", async () => {
    const fetcher = vi.fn(
      (_url: string, init: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init.signal?.addEventListener("abort", () =>
            reject(new DOMException("Aborted", "AbortError")),
          );
        }),
    );
    vi.stubGlobal("fetch", fetcher);
    const view = renderWithProviders(<ScopedQueryProbe />);
    await waitFor(() => expect(fetcher).toHaveBeenCalledOnce());
    const request = fetcher.mock.calls[0][1];
    expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe("TEST-tenant");
    expect(request.signal).toBeInstanceOf(AbortSignal);
    view.unmount();
    expect(request.signal?.aborted).toBe(true);
  });
  it("fetches through apiFetch and renders data", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ data: [{ id: "v-1" }] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    renderWithProviders(<QueryProbe />);
    await waitFor(() => expect(screen.getByText("v-1")).toBeInTheDocument());
  });
});

describe("useApiMutation", () => {
  it("surfaces failures as an error toast", async () => {
    const { user } = renderWithProviders(<MutationProbe />);
    await user.click(screen.getByRole("button", { name: "go" }));
    await waitFor(() => expect(document.body.textContent).toContain("nope"));
  });

  // Review finding: a caller with its own dedicated error surface (the
  // shell's login form) must be able to opt out of the automatic toast
  // without losing error reporting altogether.
  it("suppresses the toast when suppressErrorToast is set, without suppressing the error itself", async () => {
    const { user } = renderWithProviders(<SilentMutationProbe />);
    await user.click(screen.getByRole("button", { name: "go" }));

    // The mutation's own error state still surfaces (the caller's alert).
    expect(await screen.findByRole("alert")).toHaveTextContent("shh");
    // sonner's toast region never received the message — the only signal
    // of "shh" anywhere in the document is the caller's own alert.
    const occurrences = document.body.textContent?.split("shh").length ?? 1;
    expect(occurrences - 1).toBe(1);
  });
});

it("executes an explicit non-queued mutation immediately when offline", async () => {
  const { onlineManager } = await import("@tanstack/react-query");
  const action = vi.fn().mockRejectedValue(new Error("TEST offline failure"));
  function Probe() {
    const mutation = useApiMutation(action, { networkMode: "always", suppressErrorToast: true });
    return (
      <>
        <button onClick={() => mutation.mutate(undefined)}>Attempt now</button>
        {mutation.error && <p role="alert">{mutation.error.message}</p>}
      </>
    );
  }
  onlineManager.setOnline(false);
  try {
    const { user, unmount } = renderWithProviders(<Probe />);
    await user.click(screen.getByRole("button", { name: "Attempt now" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("TEST offline failure");
    expect(action).toHaveBeenCalledOnce();
    unmount();
  } finally {
    onlineManager.setOnline(true);
  }
});
