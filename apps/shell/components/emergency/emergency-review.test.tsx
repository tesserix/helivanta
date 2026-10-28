import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { EmergencyTransportReviewPanel, EmergencyClinicalAdmin } from "./emergency-review";

const base = "0199abc0-0000-7000-8000-000000000001";
const row = {
  id: "0199abc0-0000-7000-8000-000000000002",
  base_id: base,
  version: 1,
  status: "received",
};
const grant = {
  id: "0199abc0-0000-7000-8000-000000000003",
  subject: "user-doctor",
  version: 1,
  enabled: true,
  evidence_reference: "SYN-PRIVILEGE",
  expires_at: "2026-10-20T00:00:00Z",
  verified_by: "user-admin",
  currently_active: true,
  created_at: "2026-10-18T00:00:00Z",
  updated_at: "2026-10-18T00:00:00Z",
};
const json = (data: unknown, status = 200) =>
  new Response(JSON.stringify(data), { status, headers: { "Content-Type": "application/json" } });
const page = { data: [], page: { has_more: false, next_cursor: null } };
afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

it("does not offer review on a role without explicit clinical access", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) =>
      Promise.resolve(
        json(
          url.includes("permissions")
            ? { data: ["emergency.case.read", "emergency.case.assess"] }
            : url.endsWith("clinical-reviewer")
              ? { error: "not_found", message: "resource not found" }
              : page,
          url.endsWith("clinical-reviewer") ? 404 : 200,
        ),
      ),
    ),
  );
  renderWithProviders(<EmergencyTransportReviewPanel row={row} />);
  expect(
    await screen.findByText(/Active independently verified review access is required/),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Record transport requirement" }),
  ).not.toBeInTheDocument();
});

it("retries the exact review after an ambiguous response and makes no dispatch claim", async () => {
  const writes: { path: string; body: string }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push({ path: url, body: String(init.body) });
        return Promise.resolve(
          writes.length === 1
            ? json({ error: "unavailable", message: "Review unavailable" }, 503)
            : json({
                ...JSON.parse(String(init.body)),
                id: "synthetic-review",
                sequence: 1,
                case_id: row.id,
                reviewer_subject: "user-doctor",
                reviewer_grant_id: grant.id,
                created_at: "2026-10-18T00:00:00Z",
              }),
        );
      }
      return Promise.resolve(
        json(
          url.includes("permissions")
            ? { data: ["emergency.case.read", "emergency.case.assess"], subject: "user-doctor" }
            : url.endsWith("clinical-reviewer")
              ? grant
              : page,
        ),
      );
    }),
  );
  const { user } = renderWithProviders(<EmergencyTransportReviewPanel row={row} />);
  await user.click(await screen.findByRole("combobox", { name: "Reviewed transport class" }));
  await user.click(screen.getByRole("option", { name: "Advanced life support" }));
  await user.type(screen.getByLabelText("Reviewed transport policy"), "SYN-TRANSPORT-POLICY");
  await user.type(screen.getByLabelText("Transport review valid until"), "2026-10-20T12:00");
  await user.click(screen.getByRole("button", { name: "Record transport requirement" }));
  expect(await screen.findByText(/Review unavailable/)).toBeInTheDocument();
  expect(screen.getByLabelText("Reviewed transport policy")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Record transport requirement" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]).toEqual(writes[0]);
  expect(JSON.parse(writes[0].body)).toMatchObject({ case_version: 1, required_class: "als" });
  expect(await screen.findByText(/No vehicle has been reserved or dispatched/)).toBeInTheDocument();
});

it("loads current verification before submitting a versioned privilege change", async () => {
  let saved: unknown = null;
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push(String(init.body));
        saved = { ...grant, ...JSON.parse(String(init.body)), version: 1 };
        return Promise.resolve(json(saved));
      }
      if (url.includes("permissions"))
        return Promise.resolve(
          json({ data: ["emergency.clinical.manage"], subject: "user-admin" }),
        );
      return Promise.resolve(
        saved ? json(saved) : json({ error: "not_found", message: "resource not found" }, 404),
      );
    }),
  );
  const { user } = renderWithProviders(<EmergencyClinicalAdmin base={base} />);
  await user.type(
    await screen.findByLabelText("Clinical reviewer identity subject"),
    "user-doctor",
  );
  await user.click(screen.getByRole("button", { name: "Load clinical review access" }));
  await user.type(
    await screen.findByLabelText("Clinical privilege approval reference"),
    "SYN-PRIVILEGE",
  );
  await user.type(screen.getByLabelText("Clinical review access expires at"), "2026-10-20T12:00");
  await user.click(screen.getByLabelText("Clinical review access enabled"));
  await user.click(screen.getByRole("button", { name: "Save clinical review access" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(JSON.parse(writes[0])).toMatchObject({
    version: 0,
    enabled: true,
    evidence_reference: "SYN-PRIVILEGE",
  });
  expect(JSON.parse(writes[0])).not.toHaveProperty("verified_by");
});

it("can revoke its own access using the original expiry instant", async () => {
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push(String(init.body));
        return Promise.resolve(json({ ...grant, enabled: false, version: 2 }));
      }
      return Promise.resolve(
        json(
          url.includes("permissions")
            ? { data: ["emergency.clinical.manage"], subject: grant.subject }
            : grant,
        ),
      );
    }),
  );
  const { user } = renderWithProviders(<EmergencyClinicalAdmin base={base} />);
  await user.type(
    await screen.findByLabelText("Clinical reviewer identity subject"),
    grant.subject,
  );
  await user.click(screen.getByRole("button", { name: "Load clinical review access" }));
  await user.click(await screen.findByRole("button", { name: "Revoke clinical review access" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(JSON.parse(writes[0])).toEqual({
    version: 1,
    enabled: false,
    evidence_reference: grant.evidence_reference,
    expires_at: grant.expires_at,
  });
});
