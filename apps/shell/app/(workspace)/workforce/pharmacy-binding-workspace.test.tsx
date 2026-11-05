import { useState } from "react";
import { renderWithProviders } from "@helivanta/api/testing";
import { screen } from "@testing-library/react";
import { PharmacyBindingWorkspace } from "./pharmacy-binding-workspace";

const id = (n: number) => "019a0000-0000-7000-8000-" + String(n).padStart(12, "0");
const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-reader" };
const hospital = "22222222-2222-4222-8222-222222222222";
const choice = {
  pharmacy_id: id(1),
  outlet_id: id(2),
  pharmacy_name: "Synthetic Pharmacy",
  outlet_name: "Synthetic Outlet",
};
const binding = {
  id: id(3),
  pharmacy_id: choice.pharmacy_id,
  outlet_id: choice.outlet_id,
  hospital_id: hospital,
  department_id: id(4),
  department_revision_id: id(5),
  replaces_binding_id: null,
  replaces_binding_version: null,
  reason: "Synthetic department responsibility",
  status: "approved",
  version: 2,
  proposed_by_subject: "synthetic-preparer",
  proposer_staff_id: id(6),
  created_at: "2026-11-05T00:00:00Z",
  decided_by_subject: "synthetic-reviewer",
  reviewer_staff_id: id(7),
  decision_reason: "Synthetic independent decision",
  decided_at: "2026-11-05T01:00:00Z",
  ended_at: null,
};
const department = {
  id: id(4),
  revision_id: id(5),
  version: 1,
  hospital_id: hospital,
  code: "PHARMACY",
  name: "Synthetic Pharmacy Department",
  kind: "department",
  status: "active",
  parent_id: null,
  effective_at: "2026-11-05T00:00:00Z",
  created_at: "2026-11-05T00:00:00Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
function respond(url: string) {
  if (url.includes("staff-actor-scopes"))
    return Response.json(page([{ hospital_id: hospital, name: "Synthetic Hospital" }]));
  if (url.includes("staff-binding-outlet-choices")) return Response.json(page([choice]));
  if (url.includes("posting-department-choices")) return Response.json(page([department]));
  if (url.includes("/history?")) return Response.json(page([]));
  if (url.includes("staff-department-bindings?")) return Response.json(page([binding]));
  if (url.endsWith("/" + binding.id)) return Response.json({ binding, source_state: "current" });
  throw new Error("Unexpected QA request: " + url);
}
afterEach(() => vi.unstubAllGlobals());
it("browses recorded relationships using canonical hospital, outlet and department metadata", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => respond(url)),
  );
  const { user } = renderWithProviders(
    <PharmacyBindingWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await user.click(
    await screen.findByRole("button", {
      name: "Open pharmacy relationships for Synthetic Hospital",
    }),
  );
  await user.click(
    await screen.findByRole("button", {
      name: "Open relationships for Synthetic Pharmacy · Synthetic Outlet",
    }),
  );
  await user.click(
    await screen.findByRole("button", {
      name: "Inspect Synthetic Pharmacy Department relationship",
    }),
  );
  expect(await screen.findByText("Department source verified")).toBeInTheDocument();
  expect(screen.getByText("Synthetic department responsibility")).toBeInTheDocument();
  expect(screen.queryByLabelText("Hospital ID")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Outlet ID")).not.toBeInTheDocument();
});

async function openRelationship(user: ReturnType<typeof renderWithProviders>["user"]) {
  await user.click(
    await screen.findByRole("button", {
      name: "Open pharmacy relationships for Synthetic Hospital",
    }),
  );
  await user.click(
    await screen.findByRole("button", {
      name: "Open relationships for Synthetic Pharmacy · Synthetic Outlet",
    }),
  );
  await user.click(
    await screen.findByRole("button", {
      name: "Inspect Synthetic Pharmacy Department relationship",
    }),
  );
}

it("retains a detected newer version when moving to an older history page", async () => {
  const newer = {
    id: id(20),
    binding_id: binding.id,
    outlet_id: choice.outlet_id,
    hospital_id: hospital,
    actor_subject: scope.subject,
    action: "read",
    command_id: null,
    recorded_at: "2026-11-05T02:00:00Z",
    recorded_version: 3,
    source_state: "changed",
    related_binding_id: null,
    related_binding_version: null,
    authorizations: [
      {
        assignment_id: id(21),
        assignment_version: 1,
        scope_epoch: 1,
        membership_epoch: 1,
        permission: "pharmacy.staff_binding.read",
      },
      {
        assignment_id: id(22),
        assignment_version: 1,
        scope_epoch: 1,
        membership_epoch: 1,
        permission: "workforce.staff_actor.use",
      },
    ],
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/history?") && !url.includes("cursor="))
        return Response.json({
          data: [newer],
          page: { has_more: true, next_cursor: "older-history" },
        });
      return respond(url);
    }),
  );
  const { user } = renderWithProviders(
    <PharmacyBindingWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await openRelationship(user);
  expect(
    await screen.findByText("Relationship changed. Recheck the current record."),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Next history page" }));
  await screen.findByText("No history entries on this page.");
  expect(screen.queryByText("Department source verified")).not.toBeInTheDocument();
  expect(screen.getByText("Relationship changed. Recheck the current record.")).toBeInTheDocument();
});

it("hides retained private detail when the fresh read is unavailable", async () => {
  let unavailable = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      unavailable && url.endsWith("/" + binding.id)
        ? Response.json({ error: { code: "unavailable", message: "Unavailable" } }, { status: 503 })
        : respond(url),
    ),
  );
  const { user } = renderWithProviders(
    <PharmacyBindingWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await openRelationship(user);
  await screen.findByText("Department source verified");
  unavailable = true;
  await user.click(screen.getByRole("button", { name: "Recheck relationship" }));
  await screen.findByText("relationship detail could not be verified.");
  expect(screen.queryByText(binding.reason)).not.toBeInTheDocument();
  expect(screen.queryByText("Department source verified")).not.toBeInTheDocument();
});

it("clears the selected outlet and detail when the account changes", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options: RequestInit) =>
      new Headers(options.headers).get("X-Helivanta-Expected-Subject") === "another-account"
        ? Response.json(page([]))
        : respond(url),
    ),
  );
  function AccountHarness() {
    const [subject, setSubject] = useState(scope.subject);
    return (
      <>
        <button onClick={() => setSubject("another-account")}>Switch QA account</button>
        <PharmacyBindingWorkspace scope={{ ...scope, subject }} onContextChanged={() => {}} />
      </>
    );
  }
  const { user } = renderWithProviders(<AccountHarness />);
  await openRelationship(user);
  await screen.findByText("Department source verified");
  await user.click(screen.getByRole("button", { name: "Switch QA account" }));
  await screen.findByText("No approved Staff hospitals on this page.");
  expect(screen.queryByText(binding.reason)).not.toBeInTheDocument();
});

it("verifies the account before showing the routed relationship workspace", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("/iam/me/permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          permissions: [],
        });
      return respond(url);
    }),
  );
  const { default: Page } = await import("./pharmacy-relationships/page");
  renderWithProviders(<Page />);
  expect(
    await screen.findByRole("button", {
      name: "Open pharmacy relationships for Synthetic Hospital",
    }),
  ).toBeInTheDocument();
});

it("clears the complete detail when protected history access is revoked", async () => {
  let revoked = false;
  const cutoff = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      revoked && url.includes("/history?")
        ? Response.json({ error: { code: "forbidden", message: "Denied" } }, { status: 403 })
        : respond(url),
    ),
  );
  const { user } = renderWithProviders(
    <PharmacyBindingWorkspace scope={scope} onContextChanged={cutoff} />,
  );
  await openRelationship(user);
  await screen.findByText("Department source verified");
  revoked = true;
  await user.click(screen.getByRole("button", { name: "Recheck relationship" }));
  await screen.findByText("Staff access changed. Reopen Staff to verify your current workspace.");
  expect(cutoff).toHaveBeenCalledTimes(1);
  expect(screen.queryByText(binding.reason)).not.toBeInTheDocument();
});

it("continues an empty filtered hospital page instead of treating it as the end", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("staff-actor-scopes") && !url.includes("cursor="))
        return Response.json({ data: [], page: { has_more: true, next_cursor: "next-hospital" } });
      return respond(url);
    }),
  );
  const { user } = renderWithProviders(
    <PharmacyBindingWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await screen.findByText("No approved Staff hospitals on this page.");
  await user.click(screen.getByRole("button", { name: "Next hospital page" }));
  expect(
    await screen.findByRole("button", {
      name: "Open pharmacy relationships for Synthetic Hospital",
    }),
  ).toBeInTheDocument();
});

it("shows changed department source without suggesting current operating authority", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.endsWith("/" + binding.id)
        ? Response.json({ binding, source_state: "changed" })
        : respond(url),
    ),
  );
  const { user } = renderWithProviders(
    <PharmacyBindingWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await openRelationship(user);
  expect(await screen.findByText("Department source changed")).toBeInTheDocument();
  expect(screen.queryByText("Department source verified")).not.toBeInTheDocument();
  expect(screen.getByText(binding.reason)).toBeInTheDocument();
});
