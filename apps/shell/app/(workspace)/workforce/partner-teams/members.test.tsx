import { screen, act } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { cancelTenantTransition } from "@helivanta/api";
import { NavigationGuardProvider } from "@helivanta/ui";
import { PartnerTeamsWorkspace } from "./workspace";
const tenant = "11111111-1111-4111-8111-111111111111";
const hospital = "22222222-2222-4222-8222-222222222222";
const id = "019ad451-1234-7123-8123-123456789012";
const memberId = "019ad451-1234-7123-8123-123456789013";
const timestamp = "2026-11-06T09:00:00Z";
const group = {
  id,
  hospital_id: hospital,
  name: "Receiving team",
  purpose: "receiving",
  reason: "Synthetic hospital recipient team",
  valid_from: timestamp,
  expires_at: "2027-01-01T09:00:00Z",
  status: "approved",
  version: 3,
  proposed_by: "governance-maker",
  membership_policy_version: 1,
  external_access_enabled: false,
  created_at: timestamp,
  updated_at: timestamp,
};
const input = {
  kind: "subject",
  subject: "synthetic-member",
  staff_id: hospital,
  posting_id: id,
  posting_revision_id: id,
  posting_version: 3,
  department_id: id,
  department_revision_id: id,
  expected_group_version: 3,
  valid_from: timestamp,
  expires_at: "2026-11-07T09:00:00Z",
  reason: "Reviewed receiving team nominee",
  sources_examined: true,
};
const member = {
  id: memberId,
  group_id: id,
  hospital_id: hospital,
  input,
  member_staff_id: hospital,
  member_display_name: "Synthetic Receiving Staff",
  member_link_id: memberId,
  member_link_version: 1,
  member_generation_id: memberId,
  member_employment_epoch: 1,
  member_membership_epoch: 1,
  prepared_source_use_id: memberId,
  source_checked_at: timestamp,
  source_until: input.expires_at,
  prepared_by: "nomination-maker",
  prepared_by_staff_id: id,
  prepared_by_link_id: id,
  prepared_by_generation_id: id,
  status: "pending_review",
  version: 1,
  created_at: timestamp,
  updated_at: timestamp,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => {
  act(() => cancelTenantTransition());
  vi.unstubAllGlobals();
});
it("lets scoped membership staff browse retained evidence without governance-admin access", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith(`/hospitals/${hospital}/partner-member-capabilities`))
        return Response.json({ hospital_id: hospital, can_prepare: false, can_review: false });
      if (url.endsWith("/iam/me/permissions"))
        return Response.json({
          tenant_id: tenant,
          subject: "membership-reader",
          data: [],
        });
      if (url.includes("/workforce/staff-actor-scopes?"))
        return Response.json(page([{ hospital_id: hospital, name: "Synthetic Hospital" }]));
      if (url.includes(`/iam/hospitals/${hospital}/partner-groups?`))
        return Response.json(page([group]));
      if (url.includes(`/partner-groups/${id}/member-proposals?`))
        return Response.json(page([member]));
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PartnerTeamsWorkspace />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Choose Synthetic Hospital" }));
  await user.click(await screen.findByRole("button", { name: "Members of Receiving team" }));
  expect(await screen.findByText("Synthetic Receiving Staff")).toBeInTheDocument();
  expect(
    screen.getByText(/Recorded membership is not current supplier access/),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Prepare a team" })).not.toBeInTheDocument();
});

it("recovers a lost committed membership decision without issuing a second command", async () => {
  let receipt: Record<string, unknown> | null = null;
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith(`/hospitals/${hospital}/partner-member-capabilities`))
        return Response.json({ hospital_id: hospital, can_prepare: false, can_review: true });
      if (url.endsWith("/iam/me/permissions"))
        return Response.json({
          tenant_id: tenant,
          subject: "membership-reviewer",
          data: ["iam.partner_group.members.read", "iam.partner_group.members.review"],
        });
      if (url.includes("/workforce/staff-actor-scopes?"))
        return Response.json(page([{ hospital_id: hospital, name: "Synthetic Hospital" }]));
      if (url.includes(`/iam/hospitals/${hospital}/partner-groups?`))
        return Response.json(page([group]));
      if (url.includes(`/partner-groups/${id}/member-proposals?`))
        return Response.json(page([receipt?.member ?? member]));
      if (url.includes(`/member-proposals/${memberId}/commands/`)) {
        if (init?.method === "PUT") {
          writes.push(url);
          const input = JSON.parse(String(init.body));
          receipt = {
            id: url.split("/").at(-1),
            member_id: memberId,
            input,
            result_version: 2,
            actor_subject: "membership-reviewer",
            actor_staff_id: tenant,
            actor_link_id: memberId,
            actor_generation_id: memberId,
            reviewed_source_use_id: memberId,
            member: { ...member, status: "approved", version: 2 },
            cohort_epoch: 1,
            created_at: timestamp,
          };
          throw new TypeError("Synthetic response lost after commit");
        }
        return Response.json(receipt);
      }
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PartnerTeamsWorkspace />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Choose Synthetic Hospital" }));
  await user.click(await screen.findByRole("button", { name: "Members of Receiving team" }));
  await user.click(await screen.findByRole("button", { name: "Review Synthetic Receiving Staff" }));
  await user.type(screen.getByLabelText("Review reason"), "Independent source and purpose review");
  await user.click(screen.getByLabelText("I examined the current Staff and posting sources"));
  await user.click(
    screen.getByLabelText("I confirm this Staff can participate in the team purpose"),
  );
  await user.click(screen.getByRole("button", { name: "Record membership decision" }));
  expect(await screen.findByText(/Decision outcome unconfirmed/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Change membership team" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Recover membership decision" }));
  expect(await screen.findByText("Recorded membership decision recovered.")).toBeInTheDocument();
  expect(writes).toHaveLength(1);
});

it("selects owner-provided Staff and recovers a lost nomination without changing its identity", async () => {
  let saved: Record<string, unknown> | null = null;
  const writes: string[] = [];
  const candidate = {
    id,
    hospital_id: hospital,
    staff_id: hospital,
    subject: input.subject,
    display_name: member.member_display_name,
    posting_revision_id: id,
    posting_version: 3,
    department_id: id,
    department_revision_id: id,
    starts_at: timestamp,
    nominal_until: "2026-12-31T09:00:00Z",
    created_at: timestamp,
    source_state: "candidate",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith(`/hospitals/${hospital}/partner-member-capabilities`))
        return Response.json({ hospital_id: hospital, can_prepare: true, can_review: false });
      if (url.endsWith("/iam/me/permissions"))
        return Response.json({
          tenant_id: tenant,
          subject: "membership-preparer",
          data: ["iam.partner_group.members.read", "iam.partner_group.members.prepare"],
        });
      if (url.includes("/workforce/staff-actor-scopes?"))
        return Response.json(page([{ hospital_id: hospital, name: "Synthetic Hospital" }]));
      if (url.includes(`/iam/hospitals/${hospital}/partner-groups?`))
        return Response.json(page([group]));
      if (url.includes(`/hospitals/${hospital}/partner-member-candidates?`))
        return Response.json(page([candidate]));
      if (url.includes(`/partner-groups/${id}/member-proposals?`))
        return Response.json(page(saved ? [saved] : []));
      if (url.includes(`/partner-groups/${id}/member-proposals/`)) {
        if (init?.method === "PUT") {
          writes.push(url);
          saved = {
            ...member,
            id: url.split("/").at(-1),
            input: JSON.parse(String(init.body)),
            prepared_by: "membership-preparer",
            source_until: candidate.nominal_until,
          };
          throw new TypeError("Synthetic nomination response lost after commit");
        }
        return Response.json(saved);
      }
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PartnerTeamsWorkspace />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Choose Synthetic Hospital" }));
  await user.click(await screen.findByRole("button", { name: "Members of Receiving team" }));
  await user.click(await screen.findByRole("button", { name: "Nominate a Staff member" }));
  await user.click(
    await screen.findByRole("button", { name: "Nominate Synthetic Receiving Staff" }),
  );
  await user.type(
    screen.getByLabelText("Nomination reason"),
    "Reviewed current receiving Staff and purpose",
  );
  await user.click(screen.getByLabelText("I examined the selected Staff account and posting"));
  await user.click(screen.getByRole("button", { name: "Prepare membership nomination" }));
  expect(await screen.findByText(/Nomination outcome unconfirmed/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Change membership hospital" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Recover membership nomination" }));
  expect(await screen.findByText("Recorded membership nomination recovered.")).toBeInTheDocument();
  expect(writes).toHaveLength(1);
});

it("clears retained staff information when a refreshed membership read loses authority", async () => {
  let revoked = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith(`/hospitals/${hospital}/partner-member-capabilities`))
        return Response.json({ hospital_id: hospital, can_prepare: false, can_review: false });
      if (url.endsWith("/iam/me/permissions"))
        return Response.json({
          tenant_id: tenant,
          subject: "membership-reader",
          data: ["iam.partner_group.members.read"],
        });
      if (url.includes("/workforce/staff-actor-scopes?"))
        return Response.json(page([{ hospital_id: hospital, name: "Synthetic Hospital" }]));
      if (url.includes(`/iam/hospitals/${hospital}/partner-groups?`))
        return Response.json(page([group]));
      if (url.includes(`/partner-groups/${id}/member-proposals?`))
        return revoked
          ? Response.json(
              { error: "forbidden", message: "Current authority withdrawn" },
              { status: 403 },
            )
          : Response.json(page([member]));
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PartnerTeamsWorkspace />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Choose Synthetic Hospital" }));
  await user.click(await screen.findByRole("button", { name: "Members of Receiving team" }));
  expect(await screen.findByText("Synthetic Receiving Staff")).toBeInTheDocument();
  revoked = true;
  await user.click(screen.getByRole("button", { name: "Refresh memberships" }));
  expect(
    await screen.findByText(/Partner team access or your account changed/),
  ).toBeInTheDocument();
  expect(screen.queryByText("Synthetic Receiving Staff")).not.toBeInTheDocument();
});
