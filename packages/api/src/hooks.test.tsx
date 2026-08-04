/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />

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

function MutationProbe() {
  const m = useApiMutation(() => Promise.reject(new Error("nope")));
  return <button onClick={() => m.mutate(undefined)}>go</button>;
}

afterEach(() => vi.unstubAllGlobals());

describe("useApiQuery", () => {
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
});
