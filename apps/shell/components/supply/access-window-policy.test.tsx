import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { OrganisationAccess } from "./organisation-access";
import { OrganisationSupport } from "./organisation-support";
import { useAccessWindowPolicy } from "./access-window-policy";
import { dateTimeInputValue } from "./time-input";
import { useState } from "react";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  subject: "TEST-owner",
  partyId: "0199e000-0000-7000-8000-000000000001",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

function fixture(
  initialLimits: unknown = { max_grant_hours: 1, max_support_hours: 1 },
  rows: { grants?: unknown[]; support?: unknown[] } = {},
) {
  let limits = initialLimits;
  const writes: string[] = [];
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: [
            "supply.party.access.manage",
            "supply.support.read",
            "supply.support.manage",
            "supply.support.revoke",
            "supply.premise.read",
            "iam.member.manage",
          ],
        }),
      );
    if (url.endsWith("/access-limits"))
      return limits === "unavailable"
        ? new Response(
            JSON.stringify({ error: "unavailable", message: "TEST policy unavailable" }),
            { status: 503 },
          )
        : new Response(JSON.stringify(limits));
    if (init?.method === "PUT" || init?.method === "POST") {
      writes.push(String(init.body));
      return new Response(JSON.stringify({ error: "unavailable", message: "TEST retry safely" }), {
        status: 500,
      });
    }
    if (url.endsWith("/grants") && !url.endsWith("/access/grants"))
      return new Response(JSON.stringify(page(rows.grants ?? [])));
    if (url.endsWith("/support")) return new Response(JSON.stringify(page(rows.support ?? [])));
    if (url.includes("/iam/members"))
      return new Response(JSON.stringify(page([{ subject: "TEST-staff" }])));
    if (url.endsWith("/premises"))
      return new Response(
        JSON.stringify(
          page([
            {
              id: "0199e000-0000-7000-8000-000000000051",
              code: "TEST-WH",
              name: "TEST Warehouse",
              kind: "warehouse",
              status: "active",
            },
          ]),
        ),
      );
    return new Response(JSON.stringify(page([])));
  });
  vi.stubGlobal("fetch", fetcher);
  // Land the editor's focus frame late so typing before it fails every run, not occasionally.
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => setTimeout(cb, 25));
  return {
    writes,
    fetcher,
    setLimits: (value: unknown) => {
      limits = value;
    },
  };
}

it("validates a new grant against the loaded one-hour policy before writing", async () => {
  const { writes, fetcher } = fixture();
  renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Add time-limited grant" }));
  expect(screen.getByLabelText("Expires at")).toHaveValue("");
  await user.click(screen.getByLabelText("Tenant member"));
  await user.click(await screen.findByRole("option", { name: "TEST-staff" }));
  fireEvent.change(screen.getByLabelText("Valid from"), { target: { value: "2026-10-17T12:00" } });
  fireEvent.change(screen.getByLabelText("Expires at"), { target: { value: "2026-10-17T14:00" } });
  await user.type(screen.getByLabelText("Reason for access"), "TEST limited audit access");
  await user.click(screen.getByRole("button", { name: "Save access grant" }));
  expect(await screen.findByText("Access window must not exceed 1 hour.")).toBeInTheDocument();
  expect(screen.getByLabelText("Expires at")).toHaveAttribute("aria-invalid", "true");
  expect(writes).toHaveLength(0);
  fireEvent.change(screen.getByLabelText("Expires at"), { target: { value: "2026-10-17T13:00" } });
  await user.click(screen.getByRole("button", { name: "Save access grant" }));
  expect(await screen.findByText("TEST retry safely")).toBeInTheDocument();
  expect(writes).toHaveLength(1);
  expect(screen.getByLabelText("Reason for access")).toHaveValue("TEST limited audit access");
  expect(
    new Headers(
      fetcher.mock.calls.find(([url]) => url.endsWith("/access-limits"))?.[1]?.headers,
    ).get("X-Helivanta-Expected-Tenant"),
  ).toBe(scope.tenantId);
});

const historicalGrant = {
  id: "0199e000-0000-7000-8000-000000000030",
  party_id: scope.partyId,
  subject: "TEST-staff",
  role: "auditor",
  premise_id: null,
  valid_from: "2026-10-17T12:00:00Z",
  expires_at: "2026-10-17T14:00:00Z",
  reason: "TEST historical approved access",
  status: "active",
  effective_status: "active",
  version: 1,
};

it.each(["unchanged", "shortened", "revoked", "administrator", "extended"])(
  "%s historical access follows the prospective duration limit",
  async (change) => {
    const role = change === "administrator" ? "administrator" : "auditor";
    const { writes } = fixture(change === "revoked" ? "unavailable" : undefined, {
      grants: [{ ...historicalGrant, role }],
    });
    renderWithProviders(<OrganisationAccess scope={scope} roles={["administrator"]} />);
    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("button", { name: `Manage TEST-staff ${role} grant` }),
    );
    if (["shortened", "administrator", "extended"].includes(change))
      fireEvent.change(screen.getByLabelText("Expires at"), {
        target: {
          value: dateTimeInputValue(
            change === "shortened" ? "2026-10-17T13:30:00Z" : "2026-10-17T15:00:00Z",
          ),
        },
      });
    if (change === "revoked") {
      await user.click(screen.getByLabelText("Grant status"));
      await user.click(await screen.findByRole("option", { name: "Revoke access" }));
    }
    await user.click(screen.getByRole("button", { name: "Save access grant" }));
    if (change === "extended") {
      expect(await screen.findByText("Access window must not exceed 1 hour.")).toBeInTheDocument();
      expect(writes).toHaveLength(0);
      return;
    }
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(screen.queryByText("Access window must not exceed 1 hour.")).not.toBeInTheDocument();
  },
);

const supportRequest = {
  id: "0199e000-0000-7000-8000-000000000050",
  party_id: scope.partyId,
  premise_id: "0199e000-0000-7000-8000-000000000051",
  beneficiary_subject: "TEST-staff",
  requester_subject: "TEST-requester",
  ticket_reference: "TEST-REVIEW",
  reason: "TEST independent review",
  valid_from: "2026-10-17T12:00:00Z",
  expires_at: "2026-10-17T14:00:00Z",
  status: "pending",
  effective_status: "pending",
  version: 1,
};

it("blocks approving an older support request exceeding the current shorter policy", async () => {
  const { writes } = fixture(undefined, {
    support: [{ ...supportRequest, allowed_actions: ["approve", "reject"] }],
  });
  renderWithProviders(<OrganisationSupport scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Review support TEST-REVIEW" }));
  await user.type(screen.getByLabelText("Decision reason"), "TEST independent review");
  await user.click(screen.getByRole("button", { name: "Record support decision" }));
  expect(await screen.findByText("Access window must not exceed 1 hour.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
});

it.each(["reject", "revoke"])(
  "allows support %s while the duration policy is unavailable",
  async (action) => {
    const { writes } = fixture("unavailable", {
      support: [{ ...supportRequest, allowed_actions: [action] }],
    });
    renderWithProviders(<OrganisationSupport scope={scope} roles={["administrator"]} />);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Review support TEST-REVIEW" }));
    await user.type(screen.getByLabelText("Decision reason"), "TEST restrict existing access");
    await user.click(screen.getByRole("button", { name: "Record support decision" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(JSON.parse(writes[0]).action ?? "revoke").toBe(action);
  },
);

function PolicyProbe({ current }: { current: typeof scope }) {
  const policy = useAccessWindowPolicy(current);
  return <output>{policy.ready ? policy.data?.max_grant_hours : "unavailable"}</output>;
}

function ScopeProbe({ field }: { field: keyof typeof scope }) {
  const [current, setCurrent] = useState(scope);
  return (
    <>
      <PolicyProbe current={current} />
      <button
        onClick={() =>
          setCurrent({
            ...scope,
            [field]: field === "subject" ? "TEST-other" : "0199e000-0000-7000-8000-000000000099",
          })
        }
      >
        Switch scope
      </button>
    </>
  );
}

it.each(["tenantId", "partyId", "subject"] as const)(
  "never reuses another %s policy while the new scope fails",
  async (field) => {
    const { setLimits } = fixture();
    const { user } = renderWithProviders(<ScopeProbe field={field} />);
    expect(await screen.findByText("1")).toBeInTheDocument();
    setLimits("unavailable");
    await user.click(screen.getByRole("button", { name: "Switch scope" }));
    expect(screen.getByText("unavailable")).toBeInTheDocument();
    expect(screen.queryByText("1")).not.toBeInTheDocument();
  },
);

it("uses the loaded support window in copy and rejects longer requests inline", async () => {
  const { writes } = fixture();
  renderWithProviders(<OrganisationSupport scope={scope} roles={["administrator"]} />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Request support access" }));
  expect(screen.getByLabelText("Support expires at")).toHaveValue("");
  expect(await screen.findByText(/Choose a window of no more than 1 hour\./)).toBeInTheDocument();
  expect(screen.queryByText(/Choose a window of no more than 24 hours/)).not.toBeInTheDocument();
  await user.click(screen.getByLabelText("Support beneficiary"));
  await user.click(await screen.findByRole("option", { name: "TEST-staff" }));
  await user.click(screen.getByLabelText("Location to investigate"));
  await user.click(await screen.findByRole("option", { name: "TEST-WH · TEST Warehouse" }));
  await user.type(screen.getByLabelText("Support ticket reference"), "TEST-POLICY");
  await user.type(screen.getByLabelText("Reason for investigation"), "TEST investigate safely");
  fireEvent.change(screen.getByLabelText("Support starts at"), {
    target: { value: "2026-10-17T12:00" },
  });
  fireEvent.change(screen.getByLabelText("Support expires at"), {
    target: { value: "2026-10-17T14:00" },
  });
  await user.click(screen.getByRole("button", { name: "Submit support request" }));
  expect(await screen.findByText("Access window must not exceed 1 hour.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  fireEvent.change(screen.getByLabelText("Support expires at"), {
    target: { value: "2026-10-17T13:00" },
  });
  await user.click(screen.getByRole("button", { name: "Submit support request" }));
  await waitFor(() => expect(writes).toHaveLength(1));
});

it.each([
  ["grant", "unavailable"],
  ["support", "unavailable"],
  ["grant", { max_grant_hours: 0, max_support_hours: 24 }],
  ["support", { max_grant_hours: 1, max_support_hours: 25 }],
])(
  "keeps %s input while unavailable or invalid policy prevents granting access (%j)",
  async (kind, limits) => {
    const { writes, setLimits } = fixture(limits);
    const grant = kind === "grant";
    renderWithProviders(
      grant ? (
        <OrganisationAccess scope={scope} roles={["administrator"]} />
      ) : (
        <OrganisationSupport scope={scope} roles={["administrator"]} />
      ),
    );
    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("button", {
        name: grant ? "Add time-limited grant" : "Request support access",
      }),
    );
    const first = screen.getByLabelText(grant ? "Tenant member" : "Support beneficiary");
    await waitFor(() => expect(first).toHaveFocus());
    const reason = screen.getByLabelText(grant ? "Reason for access" : "Reason for investigation");
    await user.type(reason, "TEST preserve unsaved reason");
    if (grant) {
      await user.click(screen.getByLabelText("Tenant member"));
      await user.click(await screen.findByRole("option", { name: "TEST-staff" }));
    }
    expect(
      await screen.findAllByText(/Current access duration limits are unavailable/),
    ).not.toHaveLength(0);
    const save = screen.getByRole("button", {
      name: grant ? "Save access grant" : "Submit support request",
    });
    expect(save).toBeDisabled();
    expect(writes).toHaveLength(0);
    setLimits({ max_grant_hours: 1, max_support_hours: 1 });
    await user.click(screen.getAllByRole("button", { name: "Try again" }).at(-1)!);
    await waitFor(() => expect(save).toBeEnabled());
    expect(reason).toHaveValue("TEST preserve unsaved reason");
  },
);
