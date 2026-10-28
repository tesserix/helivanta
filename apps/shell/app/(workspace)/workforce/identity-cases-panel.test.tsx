import "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { StaffIdentitySourcePanel } from "./identity-source-panel";

const tenant = "11111111-1111-4111-8111-111111111111";
const hospital = "22222222-2222-4222-8222-222222222222";
const staff = "33333333-3333-4333-8333-333333333333";
const source = {
  hospital_id: hospital,
  staff_id: staff,
  employee_code: "SYN-DRAFT-1",
  display_name: "Synthetic Draft Staff",
  given_name: "Synthetic",
  family_name: "Staff",
  date_of_birth: "1990-01-01",
  employment_status: "active",
  employment_epoch: 1,
  staff_version: 4,
  identity_hash: "a".repeat(64),
};

afterEach(() => vi.unstubAllGlobals());

function setup(
  options: {
    manage?: boolean;
    initial?: Record<string, unknown>;
    source?: Partial<typeof source>;
  } = {},
) {
  const writes: { url: string; body: Record<string, unknown>; headers: Headers }[] = [];
  let saved: Record<string, unknown> | null = options.initial ?? null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/iam/me/permissions")) {
        return Response.json({ tenant_id: tenant, subject: "synthetic-manager", data: [] });
      }
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body));
        writes.push({ url, body, headers: new Headers(init.headers) });
        if (writes.length === 1) {
          return Response.json(
            { error: "unavailable", message: "Synthetic uncertain result" },
            { status: 503 },
          );
        }
        saved = {
          id: url.split("/").at(-1),
          revision_id: body.command_id,
          hospital_id: hospital,
          staff_id: staff,
          version: 1,
          status: "draft",
          kind: body.kind,
          policy_reference: body.policy_reference,
          check_reference: body.check_reference,
          observed_on: body.observed_on,
          valid_until: body.valid_until,
          identity_hash: body.identity_hash,
          employment_epoch: body.employment_epoch,
          proposed_by_subject: "synthetic-manager",
          actor_subject: "synthetic-manager",
          created_at: "2026-10-28T01:00:00Z",
          updated_at: "2026-10-28T01:00:00Z",
        };
        return Response.json(saved);
      }
      if (url.includes("/checks")) {
        return Response.json({
          data: saved ? [saved] : [],
          page: { has_more: false, next_cursor: null },
        });
      }
      return Response.json({
        ...source,
        ...options.source,
        manage_checks_allowed: options.manage ?? true,
      });
    }),
  );
  return writes;
}

it("requires review facts and retains an exact draft command after an uncertain save", async () => {
  const writes = setup();
  const { user } = renderWithProviders(
    <StaffIdentitySourcePanel hospitalId={hospital} staffId={staff} onClose={() => {}} />,
  );
  await user.click(await screen.findByRole("button", { name: "Prepare review draft" }));
  const form = screen.getByRole("form", { name: "Prepare review draft" });
  await user.click(within(form).getByRole("button", { name: "Save draft" }));
  expect(writes).toHaveLength(0);
  expect(within(form).getByText("Enter the hospital policy reference.")).toBeVisible();
  expect(within(form).getByText("Enter a check reference.")).toBeVisible();
  expect(within(form).getByLabelText("Policy reference")).toHaveFocus();
  await user.type(within(form).getByLabelText("Policy reference"), "SYN-IDENTITY-POLICY");
  await user.type(
    within(form).getByLabelText("Check reference"),
    "Synthetic manual identity check",
  );
  await user.clear(within(form).getByLabelText("Observation date (UTC)"));
  await user.type(within(form).getByLabelText("Observation date (UTC)"), "2020-01-01");
  await user.type(within(form).getByLabelText("Valid until"), "2030-01-01T12:00");
  await user.click(within(form).getByRole("button", { name: "Save draft" }));
  expect(await within(form).findByRole("alert")).toHaveTextContent("same draft");
  expect(within(form).getByLabelText("Policy reference")).toBeDisabled();
  expect(writes).toHaveLength(1);
  expect(writes[0]!.body).toMatchObject({
    expected_version: 0,
    kind: "identity",
    policy_reference: "SYN-IDENTITY-POLICY",
    identity_hash: source.identity_hash,
    employment_epoch: 1,
    observed_on: "2020-01-01",
  });
  expect(writes[0]!.headers.get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  expect(writes[0]!.headers.get("X-Helivanta-Expected-Subject")).toBe("synthetic-manager");
  expect(String(writes[0]!.body.command_id)[14]).toBe("7");
  await user.click(within(form).getByRole("button", { name: "Retry same draft" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]!.url).toBe(writes[0]!.url);
  expect(writes[1]!.body).toEqual(writes[0]!.body);
  expect(await screen.findByText("SYN-IDENTITY-POLICY")).toBeInTheDocument();
  expect(screen.queryByRole("form", { name: "Prepare review draft" })).toBeNull();
  expect(screen.getByText(/does not approve identity/i)).toBeInTheDocument();
});

function storedDraft() {
  return {
    id: "01900000-0000-7000-8000-000000000001",
    revision_id: "01900000-0000-7000-8000-000000000002",
    hospital_id: hospital,
    staff_id: staff,
    version: 1,
    status: "draft",
    kind: "background",
    policy_reference: "SYN-EXISTING-POLICY",
    check_reference: "Synthetic private check reference",
    observed_on: "2020-01-01",
    valid_until: "2030-01-01T00:00:00Z",
    identity_hash: source.identity_hash,
    employment_epoch: 1,
    proposed_by_subject: "synthetic-maker",
    actor_subject: "synthetic-maker",
    created_at: "2026-10-28T01:00:00Z",
    updated_at: "2026-10-28T01:00:00Z",
  };
}

it("lets a scoped reviewer inspect history without preparing or editing drafts", async () => {
  setup({ manage: false, initial: storedDraft() });
  const { user } = renderWithProviders(
    <StaffIdentitySourcePanel hospitalId={hospital} staffId={staff} onClose={() => {}} />,
  );
  expect(await screen.findByText("SYN-EXISTING-POLICY")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Prepare review draft" })).toBeNull();
  expect(screen.queryByRole("button", { name: /^Edit draft/ })).toBeNull();
  await user.click(screen.getByRole("button", { name: "History for SYN-EXISTING-POLICY" }));
  const history = screen.getByRole("region", { name: "Review draft history" });
  expect(await within(history).findByText("Version 1 · draft")).toBeInTheDocument();
  const call = vi.mocked(fetch).mock.calls.find(([url]) => String(url).includes("/history"));
  expect(call?.[1]?.cache).toBe("no-store");
  expect(new Headers(call?.[1]?.headers).get("X-Helivanta-Expected-Subject")).toBe(
    "synthetic-manager",
  );
});

it("does not render a case returned for another staff member", async () => {
  setup({ initial: { ...storedDraft(), staff_id: "44444444-4444-4444-8444-444444444444" } });
  renderWithProviders(
    <StaffIdentitySourcePanel hospitalId={hospital} staffId={staff} onClose={() => {}} />,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Could not load or validate review drafts",
  );
  expect(screen.queryByText("SYN-EXISTING-POLICY")).toBeNull();
  expect(screen.queryByText("Synthetic private check reference")).toBeNull();
});

it("retains inactive staff draft history while disabling new changes", async () => {
  setup({ initial: storedDraft(), source: { employment_status: "inactive", employment_epoch: 3 } });
  renderWithProviders(
    <StaffIdentitySourcePanel hospitalId={hospital} staffId={staff} onClose={() => {}} />,
  );
  expect(await screen.findByText("SYN-EXISTING-POLICY")).toBeInTheDocument();
  expect(screen.getByText(/Staff is inactive/)).toBeInTheDocument();
  expect(screen.getByText(/changed since this draft/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Prepare review draft" })).toBeNull();
  expect(screen.queryByRole("button", { name: /^Edit draft/ })).toBeNull();
});
