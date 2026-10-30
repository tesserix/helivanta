import { chooseOption } from "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import Shifts from "./page";
import { NavigationGuardProvider } from "@helivanta/ui";
const hospital = "11111111-1111-4111-8111-111111111111";
const shift = {
  id: "shift-1",
  revision_id: "revision-1",
  code: "SYN-NIGHT",
  name: "Synthetic Night",
  shift_type: "night",
  description: "Synthetic policy only",
  start_time: "22:00",
  end_time: "06:00",
  break_minutes: 30,
  grace_minutes: 10,
  full_day_ot_minutes: 120,
  half_day_ot_minutes: 60,
  locum: false,
  active: true,
  version: 1,
  duration_minutes: 480,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
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
      else if (url.includes("/history"))
        body = page([
          { ...shift, id: "revision-1", shift_id: shift.id, created_at: "2026-01-01T00:00:00Z" },
        ]);
      else if (url.includes("/shifts")) body = page([shift]);
      if (init?.method === "PUT") {
        writes.push({ url, body: JSON.parse(String(init.body)) });
        status = 409;
        body = { message: "Shift changed. Reload before saving." };
      }
      return new Response(JSON.stringify(body), { status });
    }),
  );
  return writes;
}
it("reopens overnight fields, retains a failed edit and retries the same identity", async () => {
  const writes = mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Shifts />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Night" }));
  expect(screen.getByLabelText("Code")).toBeDisabled();
  expect(screen.getByLabelText("Start time")).toHaveValue("22:00");
  expect(screen.getByLabelText("End time")).toHaveValue("06:00");
  await user.clear(screen.getByLabelText("Name"));
  await user.paste("Synthetic Night Revised");
  await user.click(screen.getByRole("button", { name: "Save shift" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("changed");
  expect(screen.getByLabelText("Name")).toHaveValue("Synthetic Night Revised");
  await user.click(screen.getByRole("button", { name: "Save shift" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
});
it("shows protected revision timing and hides management controls from readers", async () => {
  mock(false);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Shifts />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText("Synthetic Night")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add shift" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Edit Synthetic Night" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "History for Synthetic Night" }));
  expect(await screen.findByRole("region", { name: "Shift revision history" })).toHaveTextContent(
    "22:00",
  );
});
it("focuses the add form and rejects zero-duration shifts before submitting", async () => {
  const writes = mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Shifts />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Add shift" }));
  await waitFor(() => expect(screen.getByLabelText("Name")).toHaveFocus());
  await user.paste("Synthetic Day");
  await user.click(screen.getByLabelText("Code"));
  await user.paste("SYN-DAY");
  await user.clear(screen.getByLabelText("End time"));
  await user.type(screen.getByLabelText("End time"), "08:00");
  await user.click(screen.getByRole("button", { name: "Save shift" }));
  expect(await screen.findByText(/different start and end times/)).toBeInTheDocument();
  expect(writes).toHaveLength(0);
});

it("keeps a dirty shift until cancellation is explicitly confirmed", async () => {
  const writes = mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Shifts />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Night" }));
  await user.clear(screen.getByLabelText("Name"));
  await user.type(screen.getByLabelText("Name"), "Synthetic unsaved shift");
  await user.click(screen.getByRole("button", { name: "Cancel changes" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Keep editing" }),
  );
  expect(screen.getByLabelText("Name")).toHaveValue("Synthetic unsaved shift");
  await user.click(screen.getByRole("button", { name: "Cancel changes" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard" }),
  );
  expect(screen.queryByRole("region", { name: "Edit shift" })).not.toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Edit Synthetic Night" })).toHaveFocus(),
  );
});

it("protects input before replacing the editor with history and restores the opener", async () => {
  mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Shifts />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Night" }));
  await user.type(screen.getByLabelText("Description"), " unsaved details");
  await user.click(screen.getByRole("button", { name: "History for Synthetic Night" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Keep editing" }),
  );
  expect(screen.getByLabelText("Description")).toHaveValue("Synthetic policy only unsaved details");
  await user.click(screen.getByRole("button", { name: "History for Synthetic Night" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard" }),
  );
  expect(await screen.findByRole("region", { name: "Shift revision history" })).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Edit shift" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Close history" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "History for Synthetic Night" })).toHaveFocus(),
  );
});

it("keeps the selected hospital and draft until a scope change is confirmed", async () => {
  const other = "22222222-2222-4222-8222-222222222222";
  mock(true, [
    { id: hospital, name: "Synthetic Hospital", access_level: "manager" },
    { id: other, name: "Other Synthetic Hospital", access_level: "manager" },
  ]);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Shifts />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Night" }));
  await user.type(screen.getByLabelText("Description"), " keep this draft");
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Hospital" }),
    "Other Synthetic Hospital",
  );
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Keep editing" }),
  );
  expect(screen.getByRole("combobox", { name: "Hospital" })).toHaveTextContent(
    "Synthetic Hospital",
  );
  expect(screen.getByLabelText("Description")).toHaveValue("Synthetic policy only keep this draft");
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
  expect(screen.queryByRole("region", { name: "Edit shift" })).not.toBeInTheDocument();
});

it("starts a fresh add form only after discarding the previous add draft", async () => {
  mock();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Shifts />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Add shift" }));
  await user.type(screen.getByLabelText("Name"), "Synthetic draft to replace");
  await user.click(screen.getByRole("button", { name: "Add shift" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard" }),
  );
  expect(screen.getByLabelText("Name")).toHaveValue("");
  expect(screen.getByLabelText("Code")).toHaveValue("");
});

it("holds panel navigation while a shift save is unresolved", async () => {
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
        return Response.json({ message: "Synthetic version conflict" }, { status: 409 });
      }
      return original(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Shifts />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Night" }));
  await user.type(screen.getByLabelText("Description"), " pending change");
  await user.click(screen.getByRole("button", { name: "Save shift" }));
  await screen.findByRole("button", { name: "Saving…" });
  await user.click(screen.getByRole("button", { name: "History for Synthetic Night" }));
  expect(screen.getByRole("region", { name: "Edit shift" })).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Shift revision history" })).not.toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  release();
  expect(await screen.findByRole("alert")).toHaveTextContent("Synthetic version conflict");
  expect(screen.getByLabelText("Description")).toHaveValue("Synthetic policy only pending change");
});
