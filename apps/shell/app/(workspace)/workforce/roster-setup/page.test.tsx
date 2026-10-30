import { chooseOption } from "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import RosterSetup from "./page";
import { NavigationGuardProvider } from "@helivanta/ui";

const router = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));
const hospital = "11111111-1111-4111-8111-111111111111";
const shiftID = "22222222-2222-4222-8222-222222222222";
const revision = "33333333-3333-4333-8333-333333333333";
const areaID = "44444444-4444-4444-8444-444444444444";
const areaRevision = "55555555-5555-4555-8555-555555555555";
const shift = {
  revision_id: revision,
  shift_id: shiftID,
  id: shiftID,
  code: "SYN-DAY",
  name: "Synthetic Day",
  active: true,
  start_time: "08:00",
  end_time: "16:00",
  duration_minutes: 480,
};
const area = {
  id: areaID,
  revision_id: areaRevision,
  kind: "area",
  code: "SYN-AREA",
  name: "Synthetic Area",
  description: "Synthetic policy",
  active: true,
  effective_on: "2026-10-14",
  reviewed_by_subject: "synthetic-owner",
  version: 1,
  shifts: [shift],
  days: [],
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => {
  vi.unstubAllGlobals();
  router.push.mockClear();
});
function mock(
  allowed = true,
  hospitals = [{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }],
) {
  const writes: { url: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      let status = 200;
      let body: unknown = page([]);
      if (url.includes("permissions")) body = { data: allowed ? ["workforce.roster.manage"] : [] };
      else if (url.endsWith("/hospitals")) body = page(hospitals);
      else if (url.includes("/shifts")) body = page([shift]);
      else if (url.includes("/history"))
        body = page([{ ...area, id: areaRevision, config_id: areaID }]);
      else if (url.includes("roster-configs")) body = page(url.includes("kind=area") ? [area] : []);
      if (init?.method === "PUT") {
        writes.push({ url, body: JSON.parse(String(init.body)) });
        status = 409;
        body = { message: "Configuration changed. Reload before saving." };
      }
      return new Response(JSON.stringify(body), { status });
    }),
  );
  return writes;
}
it("reopens saved membership, requires review and preserves a conflict for same-ID retry", async () => {
  const writes = mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <RosterSetup />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Area" }));
  expect(screen.getByLabelText("Code")).toBeDisabled();
  expect(screen.getByLabelText("Effective date")).toHaveValue("2026-10-14");
  expect(screen.getByRole("region", { name: "Edit roster area" })).toHaveTextContent("SYN-DAY");
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  expect(
    await screen.findByText("Confirm that you reviewed the configuration."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.click(
    screen.getByRole("checkbox", { name: "I reviewed the effective date and scheduling rules" }),
  );
  await user.clear(screen.getByLabelText("Name"));
  await user.paste("Synthetic Area Revised");
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("changed");
  expect(screen.getByLabelText("Name")).toHaveValue("Synthetic Area Revised");
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
});
it("reads immutable membership history and hides management controls from readers", async () => {
  mock(false);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <RosterSetup />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText("Synthetic Area")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add roster area" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Edit Synthetic Area" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "History for Synthetic Area" }));
  await waitFor(() =>
    expect(screen.getByRole("region", { name: "Roster configuration history" })).toHaveTextContent(
      "SYN-DAY",
    ),
  );
});
it("uses shared selectors for weekly allocation and restores choices after toggling weekly off", async () => {
  mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <RosterSetup />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("combobox", { name: "Configuration" }));
  await user.click(screen.getByRole("option", { name: "Weekly templates" }));
  await user.click(await screen.findByRole("button", { name: "Add weekly template" }));
  await waitFor(() => expect(screen.getByLabelText("Name")).toHaveFocus());
  await user.click(screen.getByRole("checkbox", { name: "Monday weekly off" }));
  await user.click(await screen.findByRole("combobox", { name: "Monday first area" }));
  await user.click(screen.getByRole("option", { name: /Synthetic Area/ }));
  await user.click(screen.getByRole("combobox", { name: "Monday first shift" }));
  await user.click(screen.getByRole("option", { name: /SYN-DAY/ }));
  const day = screen.getByRole("group", { name: "Monday" });
  expect(day).toHaveTextContent("SYN-DAY");
  await user.click(screen.getByRole("checkbox", { name: "Monday weekly off" }));
  expect(within(day).queryByRole("combobox")).not.toBeInTheDocument();
  await user.click(screen.getByRole("checkbox", { name: "Monday weekly off" }));
  expect(day).toHaveTextContent("SYN-DAY");
  expect(day).toHaveTextContent("Synthetic Area");
});

it("preserves roster input on Cancel until explicitly discarded and restores focus", async () => {
  const writes = mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <RosterSetup />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Area" }));
  await user.type(screen.getByLabelText("Description"), " unsaved area");
  await user.click(screen.getByRole("button", { name: "Cancel changes" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Keep editing" }),
  );
  expect(screen.getByLabelText("Description")).toHaveValue("Synthetic policy unsaved area");
  await user.click(screen.getByRole("button", { name: "Cancel changes" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard" }),
  );
  expect(screen.queryByRole("region", { name: "Edit roster area" })).not.toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Edit Synthetic Area" })).toHaveFocus(),
  );
});

it("holds configuration kind changes until the draft is explicitly discarded", async () => {
  mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <RosterSetup />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Area" }));
  await user.type(screen.getByLabelText("Description"), " retain across kind change");
  await user.click(screen.getByRole("combobox", { name: "Configuration" }));
  await user.click(screen.getByRole("option", { name: "Shift groups" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Keep editing" }),
  );
  expect(screen.getByRole("combobox", { name: "Configuration" })).toHaveTextContent("Roster areas");
  expect(screen.getByLabelText("Description")).toHaveValue(
    "Synthetic policy retain across kind change",
  );
  await user.click(screen.getByRole("combobox", { name: "Configuration" }));
  await user.click(screen.getByRole("option", { name: "Shift groups" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard" }),
  );
  expect(await screen.findByRole("button", { name: "Add shift group" })).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Edit roster area" })).not.toBeInTheDocument();
});

it("guards replacement with history and returns focus to its opener", async () => {
  mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <RosterSetup />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Area" }));
  await user.type(screen.getByLabelText("Description"), " keep history draft");
  await user.click(screen.getByRole("button", { name: "History for Synthetic Area" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Keep editing" }),
  );
  expect(screen.getByLabelText("Description")).toHaveValue("Synthetic policy keep history draft");
  await user.click(screen.getByRole("button", { name: "History for Synthetic Area" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard" }),
  );
  expect(
    await screen.findByRole("region", { name: "Roster configuration history" }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Edit roster area" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Close history" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "History for Synthetic Area" })).toHaveFocus(),
  );
});

it("confirms replacing a new form and resets its unsaved values", async () => {
  mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <RosterSetup />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Add roster area" }));
  await user.type(screen.getByLabelText("Name"), "Synthetic replaceable area");
  await user.click(screen.getByRole("button", { name: "Add roster area" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard" }),
  );
  expect(screen.getByLabelText("Name")).toHaveValue("");
  expect(screen.getByLabelText("Code")).toHaveValue("");
});

it("guards the shift-setup link and only navigates after explicit discard", async () => {
  mock();
  const original = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn((url: RequestInfo | URL, init?: RequestInit) =>
      String(url).includes("/shifts")
        ? Promise.resolve(Response.json(page([])))
        : original(url, init),
    ),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <RosterSetup />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Add roster area" }));
  await user.type(screen.getByLabelText("Name"), "Synthetic unsaved area link");
  const link = await screen.findByRole("link", { name: "shift" });
  await user.click(link);
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Keep editing" }),
  );
  expect(screen.getByLabelText("Name")).toHaveValue("Synthetic unsaved area link");
  expect(router.push).not.toHaveBeenCalled();
  await user.click(link);
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard" }),
  );
  expect(router.push).toHaveBeenCalledExactlyOnceWith("/workforce/shifts");
});

it("keeps hospital scope until explicitly discarded", async () => {
  const other = "66666666-6666-4666-8666-666666666666";
  mock(true, [
    { id: hospital, name: "Synthetic Hospital", access_level: "manager" },
    { id: other, name: "Other Synthetic Hospital", access_level: "manager" },
  ]);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <RosterSetup />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Area" }));
  await user.type(screen.getByLabelText("Description"), " scoped draft");
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Hospital" }),
    "Other Synthetic Hospital",
  );
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Keep editing" }),
  );
  expect(screen.getByLabelText("Description")).toHaveValue("Synthetic policy scoped draft");
  expect(
    vi.mocked(fetch).mock.calls.some(([url]) => String(url).includes(`/hospitals/${other}/`)),
  ).toBe(false);
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Hospital" }),
    "Other Synthetic Hospital",
  );
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard" }),
  );
  await screen.findByRole("heading", { name: "Other Synthetic Hospital" });
  expect(screen.queryByRole("region", { name: "Edit roster area" })).not.toBeInTheDocument();
});
it("holds replacement while a save is pending and closes a successful save without prompting", async () => {
  mock();
  const original = fetch;
  let release!: () => void;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "PUT") {
        await pending;
        return Response.json({ data: { ...area, version: 2 } });
      }
      return original(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <RosterSetup />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Area" }));
  await user.type(screen.getByLabelText("Description"), " pending area save");
  await user.click(
    screen.getByRole("checkbox", { name: "I reviewed the effective date and scheduling rules" }),
  );
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  await screen.findByRole("button", { name: "Saving…" });
  await user.click(screen.getByRole("button", { name: "History for Synthetic Area" }));
  expect(screen.getByRole("region", { name: "Edit roster area" })).toBeInTheDocument();
  expect(
    screen.queryByRole("region", { name: "Roster configuration history" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  release();
  await waitFor(() =>
    expect(screen.queryByRole("region", { name: "Edit roster area" })).not.toBeInTheDocument(),
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Edit Synthetic Area" })).toHaveFocus(),
  );
});
it("resets a same-record replacement only after confirmation", async () => {
  mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <RosterSetup />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Area" }));
  await user.type(screen.getByLabelText("Description"), " replace this edit");
  await user.click(screen.getByRole("button", { name: "Edit Synthetic Area" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard" }),
  );
  expect(screen.getByLabelText("Description")).toHaveValue("Synthetic policy");
});

it("removes a rejected second weekly shift and preserves the first allocation", async () => {
  const writes = mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <RosterSetup />
    </NavigationGuardProvider>,
  );
  await chooseOption(
    user,
    await screen.findByRole("combobox", { name: "Configuration" }),
    "Weekly templates",
  );
  await user.click(await screen.findByRole("button", { name: "Add weekly template" }));
  await user.type(screen.getByLabelText("Name"), "Synthetic weekly correction");
  await user.type(screen.getByLabelText("Code"), "SYN-WEEKLY-CORRECTION");
  await user.click(screen.getByRole("checkbox", { name: "Monday weekly off" }));
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Monday first area" }),
    /Synthetic Area/,
  );
  await chooseOption(user, screen.getByRole("combobox", { name: "Monday first shift" }), /SYN-DAY/);
  await user.click(screen.getByRole("button", { name: "Add second shift for Monday" }));
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Monday second area" }),
    /Synthetic Area/,
  );
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Monday second shift" }),
    /SYN-DAY/,
  );
  await user.click(
    screen.getByRole("checkbox", { name: "I reviewed the effective date and scheduling rules" }),
  );
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  expect(await screen.findByText("Choose each shift once per day.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "Remove second shift for Monday" }));
  expect(screen.queryByRole("combobox", { name: "Monday second area" })).not.toBeInTheDocument();
  expect(screen.getByRole("combobox", { name: "Monday first area" })).toHaveTextContent(
    "Synthetic Area",
  );
  expect(screen.getByRole("combobox", { name: "Monday first shift" })).toHaveTextContent("SYN-DAY");
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  const input = writes[0]!.body as { days: { allocations: unknown[] }[] };
  expect(input.days[0]!.allocations).toEqual([
    { area_revision_id: areaRevision, shift_revision_id: revision },
  ]);
});
