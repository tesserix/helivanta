import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { type EmergencyDispatch } from "@helivanta/api";
import { chooseOption } from "../../test/select";
import { EmergencyReceivingDispatch } from "./emergency-receiving-dispatch";
const id = "019a1234-5678-7000-8000-000000000001";
const row = { id, base_id: id, version: 6, state: "on_scene" } as EmergencyDispatch;
afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});
it("does not read hospital links or receiving requests without dispatch permission", async () => {
  const fetcher = vi.fn(
    async () => new Response(JSON.stringify({ data: [], subject: "TEST-reader" })),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<EmergencyReceivingDispatch baseID={id} row={row} />);
  await waitFor(() => expect(fetcher).toHaveBeenCalled());
  expect(fetcher.mock.calls).toHaveLength(1);
});
it("freezes a destination request and its response deadline across an uncertain retry", async () => {
  const writes: Array<{ url: string; body: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({ data: ["emergency.dispatch.manage"], subject: "TEST-dispatcher" }),
        );
      if (init?.method === "PUT") {
        writes.push({ url, body: String(init.body) });
        return new Response(
          JSON.stringify({ error: "unavailable", message: "TEST pre-alert uncertainty" }),
          { status: 503 },
        );
      }
      return new Response(
        JSON.stringify({
          data: url.includes("receiving-links")
            ? [
                {
                  id,
                  status: "approved",
                  destination_name: "Synthetic Hospital",
                  facility_id: id,
                  version: 2,
                  valid_until: "2030-01-01T00:00:00Z",
                },
              ]
            : [],
          page: { has_more: false, next_cursor: null },
        }),
      );
    }),
  );
  const { user } = renderWithProviders(<EmergencyReceivingDispatch baseID={id} row={row} />);
  const partner = await screen.findByRole("combobox", { name: "Approved receiving partner" });
  await waitFor(() => expect(partner).toBeEnabled());
  await chooseOption(user, partner, "Synthetic Hospital");
  await user.type(
    screen.getByLabelText("Pre-alert communication reference"),
    "TEST-PREALERT-VOICE",
  );
  await user.click(screen.getByRole("button", { name: "Send hospital pre-alert" }));
  await screen.findByText(/TEST pre-alert uncertainty/);
  expect(screen.getByLabelText("Pre-alert communication reference")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry unchanged pre-alert" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(JSON.parse(writes[0].body)).toEqual({
    version: 6,
    link_id: id,
    response_seconds: 120,
    evidence_reference: "TEST-PREALERT-VOICE",
  });
});
