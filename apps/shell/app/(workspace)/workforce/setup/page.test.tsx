import "@/test/select";
import { chooseOption } from "@/test/select";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import HRSetup from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

function mountSetup() {
  return renderWithProviders(
    <NavigationGuardProvider>
      <HRSetup />
    </NavigationGuardProvider>,
  );
}

function mockSetup(
  options: {
    allowed?: boolean;
    fail?: boolean;
    mapping?: boolean;
    /** Reads whose URL this matches answer 403. */
    refuse?: (url: string) => boolean;
  } = {},
) {
  const writes: { url: string; body: Record<string, unknown> }[] = [];
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    let body: unknown = page([]);
    let status = 200;
    if (url.includes("permissions"))
      body = {
        tenant_id: hospital,
        subject: "synthetic-manager",
        data:
          options.allowed === false
            ? []
            : ["workforce.master.manage", "organization.structure.read"],
      };
    else if (url.endsWith("/hospitals"))
      body = page([{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }]);
    else if (url.includes("/department-links") || url.includes("/organization/")) body = page([]);
    else if (url.includes("/history"))
      body = page([
        {
          id: "revision-1",
          master_id: "master-1",
          version: 1,
          name: "Original Synthetic",
          code: "SYN-01",
          active: true,
          created_at: "2026-01-01T00:00:00Z",
        },
      ]);
    else if (url.includes("/masters"))
      body = page([
        {
          id: options.mapping ? "33333333-3333-4333-8333-333333333333" : "master-1",
          revision_id: options.mapping ? "44444444-4444-4444-8444-444444444444" : "revision-1",
          kind: "department",
          name: "Synthetic Department",
          code: "SYN-01",
          description: "",
          active: true,
          version: 2,
        },
      ]);
    if (!init?.method && options.refuse?.(url))
      return new Response(JSON.stringify({ error: "forbidden", message: "secret detail" }), {
        status: 403,
      });
    if (init?.method === "PUT") {
      const input = JSON.parse(String(init.body));
      writes.push({ url, body: input });
      status = options.fail ? 409 : 200;
      body = options.fail
        ? { message: "HR setup changed. Reload before saving." }
        : { id: "saved", ...input, version: 1 };
    }
    return new Response(JSON.stringify(body), { status });
  });
  vi.stubGlobal("fetch", fetcher);
  return { writes, fetcher };
}

it("focuses the create form and preserves its stable identity and input on conflict", async () => {
  const { writes } = mockSetup({ fail: true });
  const { user } = mountSetup();
  await user.click(await screen.findByRole("button", { name: "Add setup record" }));
  await waitFor(() => expect(screen.getByLabelText("Name")).toHaveFocus());
  await user.type(screen.getByLabelText("Name"), "Synthetic New Department");
  await user.type(screen.getByLabelText("Code"), "SYN-NEW");
  await user.click(screen.getByRole("button", { name: "Save setup record" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("changed");
  expect(screen.getByLabelText("Name")).toHaveValue("Synthetic New Department");
  await user.click(screen.getByRole("button", { name: "Save setup record" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(writes[0]!.body).toMatchObject({ expected_version: 0, kind: "department", active: true });
});

it("shows revision history and keeps code and category immutable while editing", async () => {
  mockSetup();
  const { user } = mountSetup();
  await user.click(await screen.findByRole("button", { name: "History for Synthetic Department" }));
  expect(await screen.findByText("Original Synthetic")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Close history" }));
  await user.click(screen.getByRole("button", { name: "Edit Synthetic Department" }));
  expect(screen.getByLabelText("Code")).toBeDisabled();
  expect(screen.getByLabelText("Name")).toHaveValue("Synthetic Department");
});

it("allows catalog reading without exposing management controls", async () => {
  mockSetup({ allowed: false });
  mountSetup();
  expect(await screen.findByText("Synthetic Department")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add setup record" })).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Edit Synthetic Department" }),
  ).not.toBeInTheDocument();
});

it("uses the shared category dropdown and hospital-scoped API", async () => {
  const { fetcher } = mockSetup();
  const { user } = mountSetup();
  await chooseOption(user, await screen.findByLabelText("Category"), "Grades");
  await waitFor(() =>
    expect(fetcher).toHaveBeenCalledWith(
      expect.stringContaining(`/hospitals/${hospital}/hr/masters?kind=grade`),
      expect.anything(),
    ),
  );
});

it("keeps an unsaved department mapping when a category change is cancelled", async () => {
  mockSetup({ mapping: true });
  const { user } = mountSetup();
  await user.click(
    await screen.findByRole("button", { name: "Organization mapping for Synthetic Department" }),
  );
  await user.type(
    await screen.findByRole("textbox", { name: "Mapping reason" }),
    "Synthetic unsaved mapping",
  );
  await chooseOption(user, screen.getByRole("combobox", { name: "Category" }), "Designations");
  expect(await screen.findByText("Discard changes to department mapping?")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Keep editing" }));
  expect(screen.getByRole("textbox", { name: "Mapping reason" })).toHaveValue(
    "Synthetic unsaved mapping",
  );
  expect(screen.getByRole("combobox", { name: "Category" })).toHaveTextContent("Departments");
});

const refusal = "This wasn't found, or you don't have access to it.";

it("shows refused hospitals as a refusal, with no retry", async () => {
  mockSetup({ refuse: (url) => url.endsWith("/hospitals") });
  mountSetup();
  expect(await screen.findByText(refusal)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
});

it("withdraws loaded records and the open editor once a re-read is refused", async () => {
  let refused = false;
  mockSetup({ refuse: (url) => refused && url.includes("/masters") && !url.includes("/history") });
  const { user } = mountSetup();
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Department" }));
  expect(screen.getByLabelText("Name")).toHaveValue("Synthetic Department");
  refused = true;
  window.dispatchEvent(new Event("visibilitychange"));
  expect(await screen.findByText(refusal)).toBeInTheDocument();
  expect(screen.queryByText("Synthetic Department")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Name")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /try again|refresh setup/i })).toBeNull();
  expect(screen.queryByText(/secret detail/)).not.toBeInTheDocument();
});

it("shows refused revision history as a refusal, with no retry", async () => {
  mockSetup({ refuse: (url) => url.includes("/history") });
  const { user } = mountSetup();
  await user.click(await screen.findByRole("button", { name: "History for Synthetic Department" }));
  expect(await screen.findByText(refusal)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
});
