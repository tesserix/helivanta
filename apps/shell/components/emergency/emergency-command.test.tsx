import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { useEmergencyCommand } from "./emergency-command";

afterEach(() => vi.unstubAllGlobals());
it("retains both path and body when a refreshed record changes during uncertainty", async () => {
  const writes: Array<{ url: string; body: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      writes.push({ url, body: String(init?.body) });
      return new Response(JSON.stringify({ error: "unavailable", message: "TEST uncertain" }), {
        status: 503,
      });
    }),
  );
  function Probe() {
    const [record, setRecord] = useState("original");
    const save = useEmergencyCommand<{ value: string }>(
      "base",
      () => `records/${record}`,
      "PUT",
      "Saved",
    );
    return (
      <>
        <button onClick={() => save.mutate({ value: record })}>Attempt</button>
        <button onClick={() => setRecord("advanced")}>Refresh record</button>
        {save.error && <p role="alert">{save.error.message}</p>}
      </>
    );
  }
  const { user } = renderWithProviders(<Probe />);
  await user.click(screen.getByRole("button", { name: "Attempt" }));
  await screen.findByRole("alert");
  await user.click(screen.getByRole("button", { name: "Refresh record" }));
  await user.click(screen.getByRole("button", { name: "Attempt" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
});
