import "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { addDays, weekdayOf } from "@helivanta/api";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "@/test/select";
import Rosters from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const rosterID = "22222222-2222-4222-8222-222222222222";
const nurse = "33333333-3333-4333-8333-333333333333";
const areaRevision = "44444444-4444-4444-8444-444444444444";
const shiftRevision = "55555555-5555-4555-8555-555555555555";
const existing = "66666666-6666-4666-8666-666666666666";
const at = "2026-10-15T00:00:00Z";
const settings = {
  revision_id: "77777777-7777-4777-8777-777777777777",
  timezone: "Asia/Kolkata",
  week_start: 0,
  auto_population: "fixed_shift",
  effective_on: "2026-10-01",
  reviewed_by_subject: "synthetic-owner",
  version: 1,
  created_at: at,
  updated_at: at,
};
const roster = (status: "draft" | "frozen") => ({
  id: rosterID,
  revision_id: "88888888-8888-4888-8888-888888888888",
  name: "Synthetic Ward",
  start_date: "2026-10-19",
  end_date: "2026-10-25",
  status,
  settings_revision_id: settings.revision_id,
  timezone: "Asia/Kolkata",
  week_start: 0,
  auto_population: "fixed_shift",
  allocations: 1,
  reviewed_by_subject: "synthetic-owner",
  version: 3,
  created_at: at,
  updated_at: at,
});
const allocation = {
  id: existing,
  staff_id: nurse,
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  roster_date: "2026-10-19",
  shift_revision_id: shiftRevision,
  shift_code: "DAY",
  shift_name: "Day",
  start_time: "08:00",
  end_time: "16:00",
  area_revision_id: areaRevision,
  area_code: "WARD",
  area_name: "Ward",
  starts_at: "2026-10-19T02:30:00Z",
  ends_at: "2026-10-19T10:30:00Z",
  additional: false,
  added_version: 2,
  created_at: at,
};
const area = {
  id: "99999999-9999-4999-8999-999999999999",
  revision_id: areaRevision,
  kind: "area",
  code: "WARD",
  name: "Ward",
  description: "",
  active: true,
  effective_on: "2026-10-01",
  reviewed_by_subject: "synthetic-owner",
  version: 1,
  created_at: at,
  updated_at: at,
  shifts: [
    {
      revision_id: shiftRevision,
      shift_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
      code: "DAY",
      name: "Day",
      start_time: "08:00",
      end_time: "16:00",
      duration_minutes: 480,
    },
  ],
  days: [],
};
const revision = {
  id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
  revision_id: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
  roster_id: rosterID,
  action: "changed",
  mode: "roster",
  status: "draft",
  added: 1,
  removed: 0,
  skipped_existing: 0,
  skipped_unassigned: 0,
  skipped_conflicts: 0,
  reason: "Cover the ward",
  reviewed_by_subject: "synthetic-owner",
  version: 2,
  created_at: at,
};
const clash = {
  allocation_id: existing,
  staff_id: nurse,
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  roster_date: "2026-10-19",
  credentials: [
    {
      credential_id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
      type: "Nursing registration",
      valid_from: "2025-01-01",
      valid_until: "2026-10-18",
      validity_version: 1,
      standing: "expired",
    },
  ],
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

function mock({
  grants = ["workforce.roster.manage"],
  manager = true,
  configured = true,
  status = "draft" as "draft" | "frozen",
  reject = null as null | ((body: { add?: { id: string }[] }) => unknown),
  exchange = null as null | string,
  clashes = [] as unknown[],
  refuseFreeze = 0,
} = {}) {
  const writes: { method: string; url: string; body: unknown }[] = [];
  const clashReads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      if (method !== "GET") {
        writes.push({ method, url, body: JSON.parse(String(init?.body)) });
        if (url.endsWith("/freeze") && refuseFreeze > 0) {
          refuseFreeze--;
          return Response.json(
            {
              error: "conflict",
              message: "Some allocations clash with reviewed credentials.",
              reason: "credential_clashes",
              outstanding: 1,
            },
            { status: 409 },
          );
        }
        if (reject)
          return new Response(JSON.stringify(reject(writes.at(-1)!.body as never)), {
            status: 409,
          });
        const head = url.endsWith(`/rosters/${rosterID}`) ? roster("draft") : roster(status);
        return new Response(JSON.stringify(method === "PUT" ? head : { roster: head, revision }), {
          status: 200,
        });
      }
      let code = 200;
      let body: unknown = page([]);
      if (url.includes("permissions")) body = { data: grants };
      else if (url.endsWith("/workforce/hospitals"))
        body = page([
          {
            id: hospital,
            name: "Synthetic Hospital",
            access_level: manager ? "manager" : "reader",
          },
        ]);
      else if (url.endsWith("/roster-settings"))
        [code, body] = configured
          ? [200, settings]
          : [404, { error: "not_found", message: "roster settings not found" }];
      else if (url.includes("/allocations"))
        body = page(
          url.includes("version=2") ? [] : [{ ...allocation, exchange_id: exchange ?? undefined }],
        );
      else if (url.includes("/credential-clashes")) {
        clashReads.push(url);
        body = page(clashes);
      } else if (url.includes("/revisions")) body = page([revision]);
      else if (url.endsWith(`/rosters/${rosterID}`)) body = roster(status);
      else if (url.endsWith("/hr/rosters")) body = page([roster(status)]);
      else if (url.includes("roster-configs")) body = page([area]);
      else if (url.endsWith("/staff"))
        body = page([
          {
            id: nurse,
            hospital_id: hospital,
            employee_code: "SYN-001",
            display_name: "Synthetic Nurse",
            job_category: "nurse",
            employment_status: "active",
            created_at: at,
            updated_at: at,
          },
        ]);
      return new Response(JSON.stringify(body), { status: code });
    }),
  );
  return Object.assign(writes, { clashReads });
}

it("sends unconfigured hospitals to roster policy instead of offering a roster", async () => {
  mock({ configured: false });
  renderWithProviders(<Rosters />);
  expect(await screen.findByRole("link", { name: "Review roster policy" })).toHaveAttribute(
    "href",
    "/workforce/roster-policy",
  );
  expect(screen.queryByRole("button", { name: "New roster" })).not.toBeInTheDocument();
});

it("requires review, then creates whole weeks starting on the configured weekday", async () => {
  const writes = mock();
  const { user } = renderWithProviders(<Rosters />);
  await user.click(await screen.findByRole("button", { name: "New roster" }));
  await user.type(screen.getByLabelText("Name"), "Synthetic Ward");
  await user.click(screen.getByRole("button", { name: "Create roster" }));
  expect(await screen.findByText("Confirm that you reviewed the roster.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.click(screen.getByRole("checkbox", { name: "I reviewed the roster name and period" }));
  await user.click(screen.getByRole("button", { name: "Create roster" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  const body = writes[0].body as { start_date: string; end_date: string };
  expect(writes[0].url).toMatch(/\/hr\/rosters\/[0-9a-f-]{36}$/);
  expect(weekdayOf(body.start_date)).toBe(0);
  expect(body).toEqual({
    name: "Synthetic Ward",
    start_date: body.start_date,
    end_date: addDays(body.start_date, 13),
    reviewed: true,
  });
});

it("shows readers the grid and history without write actions", async () => {
  mock({ grants: [], manager: false });
  const { user } = renderWithProviders(<Rosters />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic Ward" }));
  const grid = await screen.findByRole("region", { name: "Allocations" });
  expect(await within(grid).findByText(/DAY 08:00–16:00 · WARD/)).toBeInTheDocument();
  expect(within(grid).getByRole("rowheader", { name: /Synthetic Nurse/ })).toBeInTheDocument();
  for (const name of [
    "Change shifts",
    "Populate",
    "Freeze roster",
    "Discard roster",
    "Credential clashes",
  ])
    expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Roster history" }));
  expect(await screen.findByText(/Cover the ward/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "View roster as of v2" }));
  expect(await screen.findByText("Allocations as of v2")).toBeInTheDocument();
  expect(await screen.findByText("No shifts are allocated in this version.")).toBeInTheDocument();
  expect(screen.queryByText(/exchanged/)).not.toBeInTheDocument();
});

it("marks duties an approved exchange moved on a frozen roster", async () => {
  mock({
    grants: [],
    manager: false,
    status: "frozen",
    exchange: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
  });
  const { user } = renderWithProviders(<Rosters />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic Ward" }));
  const grid = await screen.findByRole("region", { name: "Allocations" });
  expect(await within(grid).findByText("· exchanged")).toBeInTheDocument();
});

it("sends additions and ticked removals, and marks rows the API reports as overlapping", async () => {
  const writes = mock();
  const { user } = renderWithProviders(<Rosters />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic Ward" }));
  await user.click(await screen.findByRole("button", { name: "Change shifts" }));
  await user.click(
    await screen.findByRole("checkbox", { name: "Remove DAY for Synthetic Nurse on 2026-10-19" }),
  );
  await chooseOption(user, await screen.findByRole("combobox", { name: "Staff" }), /SYN-001/);
  await chooseOption(user, screen.getByRole("combobox", { name: "Date" }), "2026-10-20");
  await chooseOption(user, screen.getByRole("combobox", { name: "Area" }), "Ward");
  await chooseOption(user, screen.getByRole("combobox", { name: "Shift" }), /DAY 08:00/);
  await user.click(screen.getByRole("button", { name: "Add shift" }));
  await user.click(screen.getByRole("checkbox", { name: "I reviewed these shift changes" }));
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  const body = writes[0].body as { add: { id: string }[] };
  expect(writes[0]).toMatchObject({ method: "POST", url: expect.stringMatching(/\/changes$/) });
  expect(body).toMatchObject({
    expected_version: 3,
    mode: "roster",
    add: [
      {
        staff_id: nurse,
        roster_date: "2026-10-20",
        area_revision_id: areaRevision,
        shift_revision_id: shiftRevision,
      },
    ],
    remove: [existing],
    reviewed: true,
  });
  expect(body.add[0].id).toMatch(/^[0-9a-f-]{36}$/);
});

it("preserves the change and highlights the overlapping allocation on 409", async () => {
  const writes = mock({
    reject: ({ add = [] }) => ({
      error: "conflict",
      message: "These allocations overlap existing shifts.",
      reason: "allocation_overlap",
      allocation_ids: add.map((a) => a.id),
    }),
  });
  const { user } = renderWithProviders(<Rosters />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic Ward" }));
  await user.click(await screen.findByRole("button", { name: "Change shifts" }));
  await chooseOption(user, await screen.findByRole("combobox", { name: "Staff" }), /SYN-001/);
  await chooseOption(user, screen.getByRole("combobox", { name: "Date" }), "2026-10-19");
  await chooseOption(user, screen.getByRole("combobox", { name: "Area" }), "Ward");
  await chooseOption(user, screen.getByRole("combobox", { name: "Shift" }), /DAY 08:00/);
  await user.click(screen.getByRole("button", { name: "Add shift" }));
  await user.click(screen.getByRole("checkbox", { name: "I reviewed these shift changes" }));
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("input is preserved");
  expect(writes).toHaveLength(1);
  const list = screen.getByRole("list", { name: "Shifts to add" });
  expect(list).toHaveTextContent("2026-10-19");
  expect(list).toHaveTextContent("overlaps an existing shift");
});

it("asks for a reason and confirmation before discarding", async () => {
  const writes = mock();
  const { user } = renderWithProviders(<Rosters />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic Ward" }));
  await user.click(await screen.findByRole("button", { name: "Discard roster" }));
  await user.click(
    screen.getByRole("checkbox", { name: "I reviewed that this draft should be discarded" }),
  );
  const form = screen.getByRole("form", { name: "Discard roster" });
  await user.click(within(form).getByRole("button", { name: "Discard roster" }));
  expect(
    await screen.findByText("Give a reason; it is kept in the roster history."),
  ).toBeInTheDocument();
  await user.type(screen.getByLabelText("Reason"), "Period replanned");
  await user.click(within(form).getByRole("button", { name: "Discard roster" }));
  const dialog = await screen.findByRole("dialog", { name: "Discard this roster?" });
  expect(writes).toHaveLength(0);
  await user.click(within(dialog).getByRole("button", { name: "Discard roster" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({
    url: expect.stringMatching(/\/discard$/),
    body: { expected_version: 3, reason: "Period replanned", reviewed: true },
  });
});

it("offers correction of a frozen roster only with the correct permission", async () => {
  mock({ status: "frozen" });
  const first = renderWithProviders(<Rosters />);
  await first.user.click(await screen.findByRole("button", { name: "Open Synthetic Ward" }));
  expect(
    await screen.findByRole("heading", { name: "Synthetic Ward · Frozen" }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Unfreeze for correction" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Change shifts" })).not.toBeInTheDocument();
  first.unmount();
  vi.unstubAllGlobals();
  mock({ status: "frozen", grants: ["workforce.roster.manage", "workforce.roster.correct"] });
  const second = renderWithProviders(<Rosters />);
  await second.user.click(await screen.findByRole("button", { name: "Open Synthetic Ward" }));
  expect(
    await screen.findByRole("button", { name: "Unfreeze for correction" }),
  ).toBeInTheDocument();
});

async function openFreeze(user: ReturnType<typeof renderWithProviders>["user"]) {
  await user.click(await screen.findByRole("button", { name: "Open Synthetic Ward" }));
  await user.click(await screen.findByRole("button", { name: "Freeze roster" }));
  const form = screen.getByRole("form", { name: "Freeze roster" });
  await user.click(
    within(form).getByRole("checkbox", { name: "I reviewed the shifts, including leave" }),
  );
  return form;
}

it("lists credential clashes and freezes only after they are acknowledged", async () => {
  const writes = mock({ clashes: [clash] });
  const { user } = renderWithProviders(<Rosters />);
  const form = await openFreeze(user);
  const row = await within(form).findByRole("row", { name: /Synthetic Nurse · SYN-001/ });
  expect(row).toHaveTextContent("Nursing registration: Expired (valid 2025-01-01 to 2026-10-18)");
  await user.click(within(form).getByRole("button", { name: "Freeze roster" }));
  expect(
    await within(form).findByText("Acknowledge the credential clashes before freezing."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.click(
    within(form).getByRole("checkbox", { name: /I acknowledge 1 shift outside a reviewed/ }),
  );
  await user.click(within(form).getByRole("button", { name: "Freeze roster" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({
    url: expect.stringMatching(/\/freeze$/),
    body: { expected_version: 3, reviewed: true, acknowledged_clashes: [existing] },
  });
});

it("freezes with an empty acknowledgement when nothing clashes", async () => {
  const writes = mock();
  const { user } = renderWithProviders(<Rosters />);
  const form = await openFreeze(user);
  expect(
    await within(form).findByText(/No current shift falls outside a reviewed credential/),
  ).toBeInTheDocument();
  expect(within(form).queryByRole("checkbox", { name: /I acknowledge/ })).not.toBeInTheDocument();
  await user.click(within(form).getByRole("button", { name: "Freeze roster" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.body).toMatchObject({ acknowledged_clashes: [] });
});

it("refreshes the clashes and asks again when new ones appear before the freeze", async () => {
  const writes = mock({ clashes: [clash], refuseFreeze: 1 });
  const { user } = renderWithProviders(<Rosters />);
  const form = await openFreeze(user);
  const ack = await within(form).findByRole("checkbox", { name: /I acknowledge 1 shift/ });
  await user.click(ack);
  const reads = writes.clashReads.length;
  await user.click(within(form).getByRole("button", { name: "Freeze roster" }));
  expect(await within(form).findByRole("alert")).toHaveTextContent(
    "1 new credential clash appeared. Review the refreshed list and acknowledge again.",
  );
  await waitFor(() => expect(writes.clashReads.length).toBeGreaterThan(reads));
  expect(within(form).getByRole("checkbox", { name: /I acknowledge 1 shift/ })).not.toBeChecked();
});

it("shows managers the clash list without freezing", async () => {
  mock({ clashes: [clash], status: "frozen" });
  const { user } = renderWithProviders(<Rosters />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic Ward" }));
  await user.click(await screen.findByRole("button", { name: "Credential clashes" }));
  const panel = screen.getByRole("region", { name: "Credential clashes" });
  expect(await within(panel).findByText(/Expired \(valid 2025-01-01/)).toBeInTheDocument();
});
