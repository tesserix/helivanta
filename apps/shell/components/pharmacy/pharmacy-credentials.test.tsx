import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { PharmacyStaffSource } from "./staff-source";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/pharmacy/staff",
  useSearchParams: () => new URLSearchParams(),
}));

// Synthetic pharmacy, staff, licence numbers and councils only.
const tenant = "22222222-2222-4222-8222-222222222222";
const subject = "synthetic-pharmacy-hr";
const pharmacy = "019ddddd-dddd-7ddd-8ddd-dddddddddddd";
const credential = "019a1111-1111-7111-8111-111111111111";
const review = "019a2222-2222-7222-8222-222222222222";
const file = "019a3333-3333-7333-8333-333333333333";
const reminderId = "019a4444-4444-7444-8444-444444444444";
const stamp = "2026-11-12T00:00:00Z";
const staff = {
  id: "019ccccc-cccc-7ccc-8ccc-cccccccccccc",
  pharmacy_id: pharmacy,
  display_name: "Synthetic employed Pharmacist",
  employee_code: "SYN-PH-001",
  job_category: "pharmacist",
  employment_status: "active",
  created_at: stamp,
  updated_at: stamp,
};
const licenceType = "Synthetic pharmacist registration";
const base = `/api/v1/workforce/pharmacies/${pharmacy}`;
const credentialsURL = `${base}/staff/${staff.id}/credentials`;
const zoneURL = `${base}/hr/time-zone`;
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const reviewed = {
  number: "SYN-PC-1",
  valid_from: "2021-11-01",
  valid_until: "2026-11-20",
  version: 1,
  review_id: "019a5555-5555-7555-8555-555555555555",
  updated_at: stamp,
};
const pendingReview = (by: string) => ({
  id: review,
  staff_id: staff.id,
  credential_id: credential,
  base_version: 1,
  status: "pending",
  type: licenceType,
  number: "SYN-PC-2",
  issuer: "Synthetic Pharmacy Council",
  valid_from: "2026-11-01",
  valid_until: "2031-10-31",
  evidence_document_ids: [file],
  reason: "Checked the synthetic council register",
  proposed_by_subject: by,
  decision_note: "",
  created_at: stamp,
});
const credentials = (pending?: unknown) => ({
  credentials: [
    {
      id: credential,
      type: licenceType,
      number: "SYN-PC-1",
      issuer: "Synthetic Pharmacy Council",
      valid_from: "2021-11-01",
      valid_until: "2026-11-20",
      reviewed,
      status: "date_unknown",
      ...(pending ? { pending } : {}),
    },
  ],
});
const evidence = (status: string, id = file) => ({
  id,
  pharmacy_id: pharmacy,
  staff_id: staff.id,
  record_id: credential,
  record_kind: "credential",
  record_version: 1,
  category: "credential_evidence",
  filename: `synthetic-${status}.png`,
  content_type: "image/png",
  size: 3,
  sha256: "a".repeat(64),
  status,
  scan_attempt: 1,
  uploaded_at: stamp,
  scanned_at: stamp,
  failure_code: null,
  expires_at: "2026-11-12T00:30:00Z",
  created_at: stamp,
  updated_at: stamp,
});
const registerRow = {
  staff_id: staff.id,
  employee_code: staff.employee_code,
  display_name: staff.display_name,
  credential_id: credential,
  type: licenceType,
  number: "SYN-PC-1",
  issuer: "Synthetic Pharmacy Council",
  valid_from: "2021-11-01",
  valid_until: "2026-11-20",
  version: 1,
  review_id: reviewed.review_id,
  status: "date_unknown",
};
const reminder = (status = "open") => ({
  id: reminderId,
  staff_id: staff.id,
  employee_code: staff.employee_code,
  display_name: staff.display_name,
  credential_id: credential,
  type: licenceType,
  valid_until: "2026-11-20",
  validity_version: 1,
  threshold_days: 30,
  due_on: "2026-10-21",
  status,
  ...(status === "acknowledged"
    ? { acknowledged_by_subject: subject, acknowledged_at: stamp }
    : {}),
  created_at: stamp,
});
const savedZone = (time_zone: string, version: number) => ({
  version,
  time_zone,
  updated_by_subject: subject,
  updated_at: stamp,
});
const refusal = (status: number, error: string, message: string, reason?: string) =>
  Response.json({ error, message, ...(reason ? { reason } : {}) }, { status });

afterEach(() => vi.unstubAllGlobals());

type Call = { url: string; init?: RequestInit };
type Answer = (
  url: string,
  init?: RequestInit,
) => Response | undefined | Promise<Response | undefined>;
// One standalone pharmacy employer whose Staff manager holds the given permissions.
function pharmacyTenant({
  permissions = ["workforce.enrollment.manage", "workforce.documents.manage"],
  access = "manager",
  answer,
}: { permissions?: string[]; access?: "manager" | "reader"; answer?: Answer } = {}) {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, init });
      const custom = await answer?.(url, init);
      if (custom) return custom;
      if (url.endsWith("/permissions"))
        return Response.json({ tenant_id: tenant, subject, data: permissions });
      if (url.includes("/workforce/pharmacies?"))
        return Response.json(
          page([
            {
              pharmacy_id: pharmacy,
              name: "Synthetic Standalone Pharmacy",
              access_level: access,
              created_at: stamp,
            },
          ]),
        );
      if (url.startsWith(`${base}/staff?`)) return Response.json(page([staff]));
      if (url === credentialsURL) return Response.json(credentials());
      if (url.startsWith(`${base}/staff/${staff.id}/documents`))
        return Response.json(page([evidence("available"), evidence("revoked", review)]));
      if (url === zoneURL) return Response.json({ version: 0 });
      if (url.startsWith(`${base}/hr/credentials`)) return Response.json(page([registerRow]));
      if (url === `${base}/hr/credential-reminder-policy`)
        return Response.json({ version: 0, thresholds: [] });
      if (url.startsWith(`${base}/hr/credential-reminders?`))
        return Response.json(page([reminder()]));
      throw new Error(`Unexpected request ${init?.method ?? "GET"} ${url}`);
    }),
  );
  return calls;
}
function renderSource() {
  return renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyStaffSource />
    </NavigationGuardProvider>,
  );
}
// Every pharmacy credential request names the account it expects and is never cached.
function expectAccount(calls: Call[], prefix: string) {
  const matching = calls.filter((call) => call.url.startsWith(prefix));
  expect(matching.length).toBeGreaterThan(0);
  for (const call of matching) {
    const headers = new Headers(call.init?.headers);
    expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(tenant);
    expect(headers.get("X-Helivanta-Expected-Subject")).toBe(subject);
    expect(call.init?.cache).toBe("no-store");
  }
}
const writes = (calls: Call[]) =>
  calls.filter((call) => call.init?.method === "PUT" || call.init?.method === "POST");
const body = (call: Call | undefined) => JSON.parse(String(call?.init?.body));
async function openCredentials(user: ReturnType<typeof renderSource>["user"]) {
  await user.click(
    await screen.findByRole("button", { name: `Credentials for ${staff.display_name}` }),
  );
}
async function openRegister(user: ReturnType<typeof renderSource>["user"]) {
  await screen.findByRole("cell", { name: staff.display_name });
  await user.click(screen.getByRole("button", { name: "Credential register" }));
}
function gated() {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => (release = resolve));
  return { gate, release };
}

it("offers pharmacy credentials and the register only to a manager holding enrollment permission", async () => {
  for (const [access, permissions] of [
    ["manager", ["workforce.documents.manage"]],
    ["reader", ["workforce.enrollment.manage"]],
  ] as const) {
    pharmacyTenant({ access, permissions: [...permissions] });
    const view = renderSource();
    expect(await screen.findByRole("cell", { name: staff.display_name })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: `Credentials for ${staff.display_name}` }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Credential register" })).not.toBeInTheDocument();
    view.unmount();
  }
  pharmacyTenant();
  renderSource();
  expect(
    await screen.findByRole("button", { name: `Credentials for ${staff.display_name}` }),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Credential register" })).toBeInTheDocument();
});

it("shows pharmacy credentials under the expected account in the pharmacy calendar", async () => {
  const { gate, release } = gated();
  const calls = pharmacyTenant({
    answer: async (url) => {
      if (url !== credentialsURL) return undefined;
      await gate;
      return Response.json(credentials());
    },
  });
  const { user } = renderSource();
  await openCredentials(user);
  const panel = await screen.findByRole("region", { name: "Credential validity" });
  expect(within(panel).getByRole("status")).toHaveTextContent("Loading credentials…");
  release();
  expect(
    await within(panel).findByText(
      "Set this pharmacy's time zone in its credential register to see whether credentials are in date.",
    ),
  ).toBeInTheDocument();
  expect(within(panel).getByText("Time zone not set")).toBeInTheDocument();
  expect(within(panel).getByText(/Status uses the pharmacy calendar/)).toBeInTheDocument();
  expect(within(panel).getByText(/Synthetic Standalone Pharmacy/)).toBeInTheDocument();
  expect(within(panel).queryByText(/hospital|roster/i)).not.toBeInTheDocument();
  expectAccount(calls, credentialsURL);
  expect(calls.some((call) => call.url.includes("/workforce/hospitals/"))).toBe(false);
});

it("proposes a pharmacy review citing only this credential's released pharmacy evidence", async () => {
  const calls = pharmacyTenant({
    answer: (url, init) =>
      init?.method === "PUT" && url.startsWith(`${credentialsURL}/${credential}/reviews/`)
        ? Response.json({ ...pendingReview(subject), id: url.split("/").at(-1) })
        : undefined,
  });
  const { user } = renderSource();
  await openCredentials(user);
  await user.click(await screen.findByRole("button", { name: `Propose review of ${licenceType}` }));
  const form = await screen.findByRole("form", { name: "Propose credential review" });
  const boxes = await within(form).findAllByRole("checkbox");
  expect(boxes.map((box) => box.parentElement?.textContent)).toEqual(["synthetic-available.png"]);
  expectAccount(calls, `${base}/staff/${staff.id}/documents`);
  await user.click(boxes[0]!);
  await user.type(within(form).getByLabelText("What you checked"), "Checked the council register");
  await user.click(within(form).getByRole("button", { name: "Propose review" }));
  expect(await screen.findByText("Review proposed")).toBeInTheDocument();
  const [put] = writes(calls);
  expect(put?.url).toMatch(new RegExp(`^${credentialsURL}/${credential}/reviews/[0-9a-f-]{36}$`));
  expect(body(put)).toEqual({
    expected_version: 1,
    number: "SYN-PC-1",
    valid_from: "2021-11-01",
    valid_until: "2026-11-20",
    evidence_document_ids: [file],
    reason: "Checked the council register",
  });
  expectAccount(writes(calls), credentialsURL);
});

it("hides the decision on one's own proposal and lets another reviewer approve", async () => {
  const permissions = ["workforce.enrollment.manage", "workforce.credentials.review"];
  pharmacyTenant({
    permissions,
    answer: (url) =>
      url === credentialsURL ? Response.json(credentials(pendingReview(subject))) : undefined,
  });
  const own = renderSource();
  await openCredentials(own.user);
  expect(
    await screen.findByRole("button", { name: `Withdraw proposal for ${licenceType}` }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /^Review proposal/ })).not.toBeInTheDocument();
  own.unmount();

  const calls = pharmacyTenant({
    permissions,
    answer: (url, init) =>
      url === credentialsURL
        ? Response.json(credentials(pendingReview("synthetic-colleague")))
        : init?.method === "POST" && url.endsWith(`/reviews/${review}/decision`)
          ? Response.json({ ...pendingReview("synthetic-colleague"), status: "approved" })
          : undefined,
  });
  const { user } = renderSource();
  await openCredentials(user);
  await user.click(
    await screen.findByRole("button", { name: `Review proposal for ${licenceType}` }),
  );
  const form = await screen.findByRole("form", { name: "Decide credential review" });
  await user.click(within(form).getByRole("button", { name: "Approve" }));
  expect(await screen.findByText("Credential approved")).toBeInTheDocument();
  const [post] = writes(calls);
  expect(post?.url).toBe(`${credentialsURL}/${credential}/reviews/${review}/decision`);
  expect(body(post)).toEqual({ expected_version: 1, decision: "approve", note: "" });
  expectAccount(writes(calls), credentialsURL);
});

it("clears the pharmacy Staff source when the credentials answer 404", async () => {
  pharmacyTenant({
    answer: (url) =>
      url === credentialsURL ? refusal(404, "not_found", "staff not found") : undefined,
  });
  const { user } = renderSource();
  await openCredentials(user);
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("Your account or Staff access changed"),
  );
  expect(screen.queryByRole("region", { name: "Credential validity" })).not.toBeInTheDocument();
  expect(screen.queryByRole("cell", { name: staff.display_name })).not.toBeInTheDocument();
});

it("clears the pharmacy Staff source when a proposal is refused after the account changed", async () => {
  pharmacyTenant({
    answer: (url, init) =>
      init?.method === "PUT" && url.startsWith(`${credentialsURL}/`)
        ? refusal(409, "session_changed", "Your session changed.")
        : undefined,
  });
  const { user } = renderSource();
  await openCredentials(user);
  await user.click(await screen.findByRole("button", { name: `Propose review of ${licenceType}` }));
  const form = await screen.findByRole("form", { name: "Propose credential review" });
  await user.click((await within(form).findAllByRole("checkbox"))[0]!);
  await user.type(within(form).getByLabelText("What you checked"), "Checked the council register");
  await user.click(within(form).getByRole("button", { name: "Propose review" }));
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("Your account or Staff access changed"),
  );
  expect(screen.queryByRole("form", { name: "Propose credential review" })).not.toBeInTheDocument();
});

it("keeps the expiry filter disabled until the pharmacy time zone is set", async () => {
  const { gate, release } = gated();
  const calls = pharmacyTenant({
    answer: async (url) => {
      if (url !== zoneURL) return undefined;
      await gate;
      return Response.json({ version: 0 });
    },
  });
  const { user } = renderSource();
  await openRegister(user);
  const panel = await screen.findByRole("region", { name: "Credential register" });
  expect(within(panel).getByRole("status")).toHaveTextContent("Loading the pharmacy time zone…");
  release();
  expect(
    await within(panel).findByText(
      "Set this pharmacy's time zone to filter credentials by expiry.",
    ),
  ).toBeInTheDocument();
  const filter = within(panel).getByRole("form", { name: "Register filter" });
  expect(within(filter).getByLabelText("Expiring within (days)")).toBeDisabled();
  expect(within(filter).getByRole("button", { name: "Show credentials" })).toBeDisabled();
  const table = await within(panel).findByRole("table", {
    name: "Reviewed credentials at Synthetic Standalone Pharmacy",
  });
  expect(within(table).getByText("Time zone not set")).toBeInTheDocument();
  for (const prefix of [
    zoneURL,
    `${base}/hr/credentials`,
    `${base}/hr/credential-reminder-policy`,
    `${base}/hr/credential-reminders?`,
  ])
    expectAccount(calls, prefix);
  expect(within(panel).queryByText(/hospital|roster/i)).not.toBeInTheDocument();
});

it("saves the pharmacy time zone against its version, then allows the expiry filter", async () => {
  let zone: object = { version: 0 };
  const calls = pharmacyTenant({
    answer: (url, init) => {
      if (url !== zoneURL) return undefined;
      if (init?.method === "PUT") {
        const sent = body({ url, init });
        zone = savedZone(sent.time_zone, sent.expected_version + 1);
      }
      return Response.json(zone);
    },
  });
  const { user } = renderSource();
  await openRegister(user);
  const form = await screen.findByRole("form", { name: "Pharmacy time zone" });
  const input = within(form).getByLabelText("Time zone");
  await user.type(input, "Mars/Olympus_Mons");
  await user.click(within(form).getByRole("button", { name: "Save time zone" }));
  expect(
    await within(form).findByText("Use a supported IANA time zone such as Asia/Kolkata."),
  ).toBeInTheDocument();
  expect(writes(calls)).toHaveLength(0);
  await user.clear(input);
  await user.type(input, "Asia/Kolkata");
  await user.click(within(form).getByRole("button", { name: "Save time zone" }));
  expect(await screen.findByText("Time zone saved")).toBeInTheDocument();
  const [put] = writes(calls);
  expect(put?.url).toBe(zoneURL);
  expect(put?.init?.method).toBe("PUT");
  expect(body(put)).toEqual({ expected_version: 0, time_zone: "Asia/Kolkata" });
  expectAccount(writes(calls), zoneURL);
  expect(
    await screen.findByText(
      "Version 1: expiry standing, the register filter and reminders use Asia/Kolkata.",
    ),
  ).toBeInTheDocument();
  const filter = screen.getByRole("form", { name: "Register filter" });
  expect(within(filter).getByLabelText("Expiring within (days)")).toBeEnabled();
  const saved = screen.getByRole("form", { name: "Pharmacy time zone" });
  const changed = within(saved).getByLabelText("Time zone");
  await user.clear(changed);
  await user.type(changed, "Asia/Dubai");
  await user.click(within(saved).getByRole("button", { name: "Save time zone" }));
  expect(
    await screen.findByText(
      "Version 2: expiry standing, the register filter and reminders use Asia/Dubai.",
    ),
  ).toBeInTheDocument();
  expect(body(writes(calls)[1])).toEqual({ expected_version: 1, time_zone: "Asia/Dubai" });
});

it("keeps the typed time zone when another manager changed it first", async () => {
  let refused = false;
  const calls = pharmacyTenant({
    answer: (url, init) => {
      if (url !== zoneURL) return undefined;
      if (init?.method !== "PUT")
        return Response.json(refused ? savedZone("Asia/Dubai", 1) : { version: 0 });
      refused = true;
      return refusal(
        409,
        "conflict",
        "The pharmacy's time zone changed since you opened it. Reload before saving.",
        "version_conflict",
      );
    },
  });
  const { user } = renderSource();
  await openRegister(user);
  const form = await screen.findByRole("form", { name: "Pharmacy time zone" });
  await user.type(within(form).getByLabelText("Time zone"), "Asia/Kolkata");
  await user.click(within(form).getByRole("button", { name: "Save time zone" }));
  expect(
    await screen.findByText(
      "The pharmacy's time zone changed since you opened it. Reload before saving.",
    ),
  ).toBeInTheDocument();
  expect(within(form).getByLabelText("Time zone")).toHaveValue("Asia/Kolkata");
  expect(screen.getByRole("cell", { name: staff.display_name })).toBeInTheDocument();
  expect(writes(calls)).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: "Reload time zone" }));
  expect(
    await screen.findByText(
      "Version 1: expiry standing, the register filter and reminders use Asia/Dubai.",
    ),
  ).toBeInTheDocument();
  const reloaded = screen.getByRole("form", { name: "Pharmacy time zone" });
  expect(within(reloaded).getByLabelText("Time zone")).toHaveValue("Asia/Dubai");
  expect(screen.queryByRole("button", { name: "Reload time zone" })).not.toBeInTheDocument();
  expect(writes(calls)).toHaveLength(1);
});

it("shows the pharmacy's unset-zone answer when the register cannot filter", async () => {
  pharmacyTenant({
    answer: (url) =>
      url === zoneURL
        ? Response.json(savedZone("Asia/Kolkata", 1))
        : url.startsWith(`${base}/hr/credentials?expiring_within=`)
          ? refusal(
              409,
              "conflict",
              "Set this pharmacy's time zone before filtering by expiry.",
              "time_zone_unset",
            )
          : undefined,
  });
  const { user } = renderSource();
  await openRegister(user);
  const filter = await screen.findByRole("form", { name: "Register filter" });
  const input = within(filter).getByLabelText("Expiring within (days)");
  await waitFor(() => expect(input).toBeEnabled());
  await user.type(input, "30");
  await user.click(within(filter).getByRole("button", { name: "Show credentials" }));
  expect(
    await screen.findByText("Set this pharmacy's time zone before filtering by expiry."),
  ).toBeInTheDocument();
});

it("clears the pharmacy Staff source when the time zone answers 404", async () => {
  pharmacyTenant({
    answer: (url) => (url === zoneURL ? refusal(404, "not_found", "not found") : undefined),
  });
  const { user } = renderSource();
  await openRegister(user);
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("Your account or Staff access changed"),
  );
  expect(screen.queryByRole("region", { name: "Credential register" })).not.toBeInTheDocument();
});

it("saves reminder days and acknowledges a reminder in the pharmacy family", async () => {
  const calls = pharmacyTenant({
    answer: (url, init) =>
      url === `${base}/hr/credential-reminder-policy` && init?.method === "PUT"
        ? Response.json({
            version: 1,
            thresholds: body({ url, init }).thresholds,
            created_by_subject: subject,
            created_at: stamp,
          })
        : url.endsWith(`/credential-reminders/${reminderId}/acknowledgement`)
          ? Response.json(reminder("acknowledged"))
          : undefined,
  });
  const { user } = renderSource();
  await openRegister(user);
  const days = await screen.findByRole("form", { name: "Reminder days" });
  expect(
    within(screen.getByRole("region", { name: "Reminder days" })).getByText(
      "Reminders are off at this pharmacy until you set the days before expiry to raise them.",
    ),
  ).toBeInTheDocument();
  await user.type(within(days).getByLabelText("Days before expiry"), "90, 30, 0");
  await user.click(within(days).getByRole("button", { name: "Save reminder days" }));
  expect(await screen.findByText("Reminder days saved")).toBeInTheDocument();
  await user.click(
    await screen.findByRole("button", {
      name: `Acknowledge ${licenceType} reminder for ${staff.display_name}`,
    }),
  );
  expect(await screen.findByText("Reminder acknowledged")).toBeInTheDocument();
  const [policy, acknowledgement] = writes(calls);
  expect(policy?.url).toBe(`${base}/hr/credential-reminder-policy`);
  expect(body(policy)).toEqual({ expected_version: 0, thresholds: [90, 30, 0] });
  expect(acknowledgement?.url).toBe(
    `${base}/hr/credential-reminders/${reminderId}/acknowledgement`,
  );
  expect(acknowledgement?.init?.method).toBe("POST");
  expectAccount(writes(calls), `${base}/hr/`);
});
