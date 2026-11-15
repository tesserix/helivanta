import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { OutletAccess } from "./outlet-access";

const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const me = "user-owner";
let ids = 0;
const personNames: Record<string, string> = {
  [me]: "Asha Rao",
  "user-ph1": "Priya Shah",
  "user-new": "Mahesh Rao",
};
const personRoles: Record<string, { key: string; label: string }> = {
  [me]: { key: "pharmacy_owner", label: "Pharmacy owner" },
  "user-ph1": { key: "pharmacist", label: "Pharmacist" },
  "user-new": { key: "pharmacist", label: "Pharmacist" },
};
const optionName = (subject: string) =>
  `${personNames[subject]} — ${personRoles[subject].label} · ${subject}@example.test`;
const presentation = (subject: string) => ({
  subject,
  available: true,
  name: personNames[subject],
  email: `${subject}@example.test`,
  roles: [personRoles[subject]],
});
const grant = (subject: string, level: "reader" | "editor") => ({
  id: `0199e000-0000-7000-8000-${String(++ids).padStart(12, "0")}`,
  subject,
  level,
  created_at: "2026-11-01T06:30:00Z",
  updated_at: "2026-11-01T06:30:00Z",
});
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const page = (data: unknown[]) => json({ data, page: { next_cursor: null, has_more: false } });

type Sent = { method: string; url: string; body?: unknown };

function serve({
  grants = () => [grant(me, "editor"), grant("user-ph1", "reader")],
  grantsStatus = 200,
  membersStatus = 200,
}: {
  grants?: () => ReturnType<typeof grant>[];
  grantsStatus?: number;
  membersStatus?: number;
} = {}) {
  const sent: Sent[] = [];
  const removed = new Set<string>();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      sent.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      if (url.endsWith("/iam/member-presentations"))
        return json({
          tenant_id: scope.tenantId,
          actor_subject: me,
          data: JSON.parse(String(init?.body)).subjects.map(presentation),
        });
      if (url.endsWith("/iam/me/profile"))
        return json({
          subject: me,
          tenant_id: scope.tenantId,
          display_name: personNames[me],
          email: `${me}@example.test`,
          roles: [personRoles[me]],
        });
      if (method === "DELETE") removed.add(decodeURIComponent(url.split("/access/")[1]));
      if (method === "PUT" || method === "DELETE") return json({ revoked: method === "DELETE" });
      if (url.includes("/access-grants"))
        return grantsStatus === 200
          ? page(grants().filter((row) => !removed.has(row.subject)))
          : json({ error: "down" }, grantsStatus);
      if (url.endsWith("/access"))
        return removed.has(me) ? json({ error: "not found" }, 404) : json({ level: "editor" });
      if (url.includes("/iam/members"))
        return membersStatus === 200
          ? page([
              {
                id: "m1",
                subject: me,
                role_key: "pharmacy_owner",
                created_at: "2026-11-01T00:00:00Z",
              },
              {
                id: "m2",
                subject: "user-ph1",
                role_key: "pharmacist",
                created_at: "2026-11-01T00:00:00Z",
              },
              {
                id: "m3",
                subject: "user-new",
                role_key: "pharmacist",
                created_at: "2026-11-01T00:00:00Z",
              },
            ])
          : json({ error: "forbidden" }, membersStatus);
      throw new Error(`Unexpected test API request: ${method} ${url}`);
    }),
  );
  return sent;
}

const render = (canManage = true) =>
  renderWithProviders(
    <OutletAccess scope={scope} subject={me} outletName="TEST Main Outlet" canManage={canManage} />,
  );
const reads = (sent: Sent[], part: string) =>
  sent.filter((s) => s.method === "GET" && s.url.includes(part)).length;
const writes = (sent: Sent[]) =>
  sent.filter((s) => s.method !== "GET" && !s.url.endsWith("/iam/member-presentations"));

afterEach(() => vi.unstubAllGlobals());

describe("OutletAccess", () => {
  it("resolves existing access outside the loaded membership page without adding it to new choices", async () => {
    serve({ grants: () => [grant("user-outside", "reader")] });
    const original = globalThis.fetch;
    vi.stubGlobal("fetch", async (url: string, init?: RequestInit) => {
      if (url.endsWith("/iam/member-presentations"))
        return json({
          tenant_id: scope.tenantId,
          actor_subject: me,
          data: JSON.parse(String(init?.body)).subjects.map((subject: string) =>
            subject === "user-outside"
              ? {
                  subject,
                  available: true,
                  name: "Kiran Das",
                  email: "kiran@example.test",
                  roles: [{ key: "pharmacist", label: "Pharmacist" }],
                }
              : presentation(subject),
          ),
        });
      return original(url, init);
    });
    const { user } = render();
    expect(
      await screen.findByRole("combobox", { name: "Access level for Kiran Das" }),
    ).toHaveTextContent("Reader");
    await screen.findByRole("button", { name: "Refresh account details" });
    await user.click(screen.getByRole("combobox", { name: "Person" }));
    expect(
      screen.queryByRole("option", { name: /Kiran Das|user-outside/ }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: optionName(me) })).toBeInTheDocument();
  });

  it("withholds stale names and blocks a retained selection after identity lookup fails", async () => {
    const sent = serve({ grants: () => [] });
    const original = globalThis.fetch;
    let unavailable = false;
    vi.stubGlobal("fetch", async (url: string, init?: RequestInit) =>
      unavailable && url.endsWith("/iam/member-presentations")
        ? json({ error: "unavailable" }, 503)
        : original(url, init),
    );
    const { user } = render();
    await screen.findByRole("button", { name: "Refresh account details" });
    await chooseOption(
      user,
      screen.getByRole("combobox", { name: "Person" }),
      optionName("user-new"),
    );
    await user.click(screen.getByLabelText("Confirm this is the outlet to grant access at"));
    unavailable = true;
    await user.click(screen.getByRole("button", { name: "Refresh account details" }));
    await screen.findByText("Could not load account names and roles.");
    expect(screen.queryByText(/Mahesh Rao/)).not.toBeInTheDocument();
    const submit = screen.getByRole("button", { name: "Grant pharmacy access" });
    expect(submit).toBeDisabled();
    await act(async () => {
      fireEvent.submit(submit.closest("form")!);
    });
    expect(writes(sent)).toEqual([]);
  });

  it("does not substitute account IDs for an unavailable identity", async () => {
    serve({ grants: () => [] });
    const original = globalThis.fetch;
    vi.stubGlobal("fetch", async (url: string, init?: RequestInit) => {
      if (url.endsWith("/iam/member-presentations"))
        return json({
          tenant_id: scope.tenantId,
          actor_subject: me,
          data: JSON.parse(String(init?.body)).subjects.map((subject: string) =>
            subject === "user-new"
              ? { subject, available: false, roles: [personRoles[subject]] }
              : presentation(subject),
          ),
        });
      return original(url, init);
    });
    const { user } = render();
    await screen.findByText(/Some accounts are unavailable for selection/);
    await user.click(screen.getByRole("combobox", { name: "Person" }));
    expect(screen.queryByRole("option", { name: /Mahesh Rao|user-new/ })).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: optionName(me) })).toBeInTheDocument();
  });

  it("shows people by their identity names and organization roles while retaining account IDs for commands", async () => {
    serve({ grants: () => [] });
    render();
    await screen.findByText("Nobody has access to this outlet yet.");
    await userEvent.click(screen.getByRole("combobox", { name: "Person" }));
    expect(
      await screen.findByRole("option", { name: /Asha Rao.*Pharmacy owner/ }),
    ).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /Priya Shah.*Pharmacist/ })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: me })).not.toBeInTheDocument();
  });

  it("without the manage permission shows only your own access", async () => {
    const sent = serve();
    render(false);
    expect(await screen.findByText(/Your access here/)).toHaveTextContent("editor");
    expect(screen.queryByRole("list", { name: "Who has access" })).not.toBeInTheDocument();
    expect(reads(sent, "/access-grants")).toBe(0);
    expect(reads(sent, "/iam/members")).toBe(0);
  });

  it("lists who has access and marks you", async () => {
    serve();
    render();
    await screen.findByRole("combobox", { name: "Access level for Asha Rao" });
    const list = await screen.findByRole("list", { name: "Who has access" });
    const row = (subject: string) =>
      within(list)
        .getByRole("combobox", { name: `Access level for ${personNames[subject]}` })
        .closest("li")!;
    expect(within(row(me)).getByText("You")).toBeInTheDocument();
    expect(within(row("user-ph1")).queryByText("You")).not.toBeInTheDocument();
    expect(within(row("user-ph1")).getByRole("combobox")).toHaveTextContent("Reader");
  });

  it("says when nobody has access, and never says so when the list fails", async () => {
    serve({ grants: () => [] });
    const { unmount } = render();
    expect(await screen.findByText("Nobody has access to this outlet yet.")).toBeInTheDocument();
    unmount();
    serve({ grantsStatus: 500 });
    render();
    expect(await screen.findByText(/Could not load who has access/)).toBeInTheDocument();
    expect(screen.queryByText("Nobody has access to this outlet yet.")).not.toBeInTheDocument();
  });

  it("grants a chosen person a chosen level and refetches", async () => {
    const user = userEvent.setup();
    const sent = serve();
    render();
    await screen.findByRole("list", { name: "Who has access" });
    const person = screen.getByRole("combobox", { name: "Person" });
    await user.click(person);
    expect(await screen.findByRole("option", { name: optionName("user-new") })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: optionName("user-ph1") })).not.toBeInTheDocument();
    expect(screen.queryByRole("option", { name: optionName(me) })).not.toBeInTheDocument();
    await user.click(screen.getByRole("option", { name: optionName("user-new") }));
    await chooseOption(user, screen.getByRole("combobox", { name: "Level" }), "Reader");
    await user.click(screen.getByLabelText("Confirm this is the outlet to grant access at"));
    const before = reads(sent, "/access-grants");
    await user.click(screen.getByRole("button", { name: "Grant pharmacy access" }));
    await waitFor(() =>
      expect(writes(sent)).toEqual([
        expect.objectContaining({ method: "PUT", body: { level: "reader" } }),
      ]),
    );
    expect(writes(sent)[0].url).toMatch(/\/access\/user-new$/);
    await waitFor(() => expect(reads(sent, "/access-grants")).toBeGreaterThan(before));
  });

  it("asks you to confirm the outlet before granting", async () => {
    const user = userEvent.setup();
    const sent = serve();
    render();
    await screen.findByRole("list", { name: "Who has access" });
    await chooseOption(
      user,
      screen.getByRole("combobox", { name: "Person" }),
      optionName("user-new"),
    );
    await user.click(screen.getByRole("button", { name: "Grant pharmacy access" }));
    expect(
      await screen.findByText("Confirm the outlet before granting access."),
    ).toBeInTheDocument();
    expect(writes(sent)).toEqual([]);
  });

  it("changes a person's level", async () => {
    const user = userEvent.setup();
    const sent = serve();
    render();
    await chooseOption(
      user,
      await screen.findByRole("combobox", { name: "Access level for Priya Shah" }),
      "Editor",
    );
    await waitFor(() =>
      expect(writes(sent)).toEqual([
        expect.objectContaining({ method: "PUT", body: { level: "editor" } }),
      ]),
    );
    expect(writes(sent)[0].url).toMatch(/\/access\/user-ph1$/);
    expect(await screen.findByText("Access changed")).toBeInTheDocument();
  });

  it("removes access only after confirmation", async () => {
    const user = userEvent.setup();
    const sent = serve();
    render();
    await user.click(await screen.findByRole("button", { name: "Remove Priya Shah" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Remove Priya Shah's access to TEST Main Outlet?");
    expect(dialog).not.toHaveTextContent("You will lose access to this outlet.");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(writes(sent)).toEqual([]);
    await user.click(screen.getByRole("button", { name: "Remove Priya Shah" }));
    await user.click(
      within(await screen.findByRole("dialog")).getByRole("button", { name: "Remove access" }),
    );
    await waitFor(() =>
      expect(writes(sent)).toEqual([expect.objectContaining({ method: "DELETE" })]),
    );
    expect(writes(sent)[0].url).toMatch(/\/access\/user-ph1$/);
    expect(await screen.findByText("Access removed")).toBeInTheDocument();
  });

  it("warns before you remove your own access, then shows you have none", async () => {
    const user = userEvent.setup();
    serve();
    render();
    await user.click(await screen.findByRole("button", { name: `Remove ${personNames[me]}` }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("You will lose access to this outlet.");
    await user.click(within(dialog).getByRole("button", { name: "Remove access" }));
    // The panel shows the server's answer, not the level you had.
    expect(
      await screen.findByText(/You don.t have access to this outlet yet\./),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Your access here/)).not.toBeInTheDocument();
  });

  it("without tenant member access still lists grants and offers yourself", async () => {
    const user = userEvent.setup();
    serve({ grants: () => [grant("user-ph1", "reader")], membersStatus: 403 });
    render();
    expect(
      await screen.findByText(
        "Only a tenant administrator can add a new person here. You can still change or remove existing access.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("list", { name: "Who has access" })).toHaveTextContent(
      "Account details unavailable",
    );
    await user.click(screen.getByRole("combobox", { name: "Person" }));
    expect(await screen.findByRole("option", { name: optionName(me) })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: optionName("user-new") })).not.toBeInTheDocument();
  });
});
