import "../../../test/select";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import RosterPolicy from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const staffID = "22222222-2222-4222-8222-222222222222";
const pinned = "33333333-3333-4333-8333-333333333333";
const current = "44444444-4444-4444-8444-444444444444";
const assignment = {
  staff_id: staffID,
  revision_id: "55555555-5555-4555-8555-555555555555",
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  weekly_template_revision_id: pinned,
  template_id: "66666666-6666-4666-8666-666666666666",
  template_code: "SYN-WEEK",
  template_name: "Synthetic Week",
  fixed: true,
  active: true,
  effective_on: "2026-10-15",
  reviewed_by_subject: "synthetic-owner",
  version: 1,
  created_at: "2026-10-15T00:00:00Z",
  updated_at: "2026-10-15T00:00:00Z",
};
const template = {
  id: assignment.template_id,
  revision_id: current,
  kind: "weekly_template",
  code: "SYN-WEEK",
  name: "Synthetic Week Revised",
  active: true,
  effective_on: "2026-10-16",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function mock({ allowed = true, configured = false } = {}) {
  const writes: { url: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      let status = 200;
      let body: unknown = page([]);
      if (url.includes("permissions")) body = { data: allowed ? ["workforce.roster.manage"] : [] };
      else if (url.endsWith("/hospitals"))
        body = page([
          {
            id: hospital,
            name: "Synthetic Hospital",
            access_level: allowed ? "manager" : "reader",
          },
        ]);
      else if (url.endsWith("/roster-settings"))
        [status, body] = configured
          ? [
              200,
              {
                revision_id: pinned,
                timezone: "Asia/Kolkata",
                week_start: 0,
                auto_population: "fixed_shift",
                effective_on: "2026-10-15",
                reviewed_by_subject: "synthetic-owner",
                version: 1,
                created_at: "2026-10-15T00:00:00Z",
                updated_at: "2026-10-15T00:00:00Z",
              },
            ]
          : [404, { error: "not_found", message: "roster settings not found" }];
      else if (url.includes("roster-assignments")) body = page([assignment]);
      else if (url.includes("roster-configs")) body = page([template]);
      if (init?.method === "PUT") {
        writes.push({ url, body: JSON.parse(String(init.body)) });
        status = 400;
        body = { error: "invalid_request", message: "Use a supported IANA timezone." };
      }
      return new Response(JSON.stringify(body), { status });
    }),
  );
  return writes;
}
it("treats missing settings as unconfigured, requires review and preserves a rejected save", async () => {
  const writes = mock();
  const { user } = renderWithProviders(<RosterPolicy />);
  expect(await screen.findByText(/have not been reviewed/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Configure roster settings" }));
  await user.type(screen.getByLabelText("Timezone"), "Local");
  await user.click(screen.getByRole("button", { name: "Save roster settings" }));
  expect(await screen.findByText("Confirm that you reviewed the policy.")).toBeInTheDocument();
  expect(screen.getByText(/supported IANA timezone such as/)).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.clear(screen.getByLabelText("Timezone"));
  await user.type(screen.getByLabelText("Timezone"), "Asia/Kolkata");
  await user.click(
    screen.getByRole("checkbox", {
      name: "I reviewed the timezone, roster week and automatic population",
    }),
  );
  await user.click(screen.getByRole("button", { name: "Save roster settings" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("input is preserved");
  expect(screen.getByLabelText("Timezone")).toHaveValue("Asia/Kolkata");
  expect(writes[0]).toMatchObject({
    url: expect.stringContaining(`/workforce/hospitals/${hospital}/hr/roster-settings`),
    body: { expected_version: 0, timezone: "Asia/Kolkata", reviewed: true },
  });
});
it("shows the reviewed policy read-only to readers", async () => {
  mock({ allowed: false, configured: true });
  renderWithProviders(<RosterPolicy />);
  expect(await screen.findByText("Asia/Kolkata")).toBeInTheDocument();
  expect(screen.getByText("Fixed-shift staff only")).toBeInTheDocument();
  expect(screen.getByText(/Synthetic Nurse/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Edit roster settings" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Assign weekly template" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Edit Synthetic Nurse" })).not.toBeInTheDocument();
});
it("keeps a superseded pinned revision selectable and sends it with the expected version", async () => {
  const writes = mock({ configured: true });
  const { user } = renderWithProviders(<RosterPolicy />);
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Nurse" }));
  await user.click(await screen.findByRole("combobox", { name: "Weekly template" }));
  expect(screen.getByRole("option", { name: /pinned revision/ })).toBeInTheDocument();
  expect(screen.getByRole("option", { name: /Synthetic Week Revised/ })).toBeInTheDocument();
  await user.keyboard("{Escape}");
  await user.click(screen.getByRole("checkbox", { name: "Fixed shift (populated automatically)" }));
  await user.click(
    screen.getByRole("checkbox", {
      name: "I reviewed the template, effective date and population mode",
    }),
  );
  await user.click(screen.getByRole("button", { name: "Save assignment" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: expect.stringContaining(`/staff/${staffID}/roster-assignment`),
    body: {
      expected_version: 1,
      weekly_template_revision_id: pinned,
      fixed: false,
      active: true,
      effective_on: "2026-10-15",
      reviewed: true,
    },
  });
});
