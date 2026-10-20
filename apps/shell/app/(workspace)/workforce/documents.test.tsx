import "@/test/select";
import { useState } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import { webcrypto } from "node:crypto";
import { renderWithProviders } from "@helivanta/api/testing";
import {
  beginTenantTransition,
  cancelTenantTransition,
  type Hospital,
  type Staff,
} from "@helivanta/api";
import { StaffDocumentPanel } from "./document-panel";
import Workforce from "./page";

const tenant = "33333333-3333-4333-8333-333333333333";
const hospital: Hospital = {
  id: "11111111-1111-4111-8111-111111111111",
  name: "Synthetic Hospital",
  access_level: "manager",
};
const staff: Staff = {
  id: "22222222-2222-4222-8222-222222222222",
  hospital_id: hospital.id,
  display_name: "Avery Synthetic",
  employee_code: "SYN-DOC-001",
  job_category: "nurse",
  employment_status: "active",
  created_at: "2026-10-15T00:00:00Z",
  updated_at: "2026-10-15T00:00:00Z",
};
const evidence = {
  id: "44444444-4444-4444-8444-444444444444",
  hospital_id: hospital.id,
  staff_id: staff.id,
  record_id: staff.id,
  record_kind: "enrollment",
  record_version: 1,
  category: "employment_evidence",
  filename: "synthetic-evidence.png",
  content_type: "image/png",
  size: 3,
  sha256: "a".repeat(64),
  status: "scan_error",
  scan_attempt: 1,
  uploaded_at: "2026-10-15T00:00:00Z",
  scanned_at: null,
  failure_code: "scanner_unavailable",
  created_at: "2026-10-15T00:00:00Z",
  updated_at: "2026-10-15T00:00:00Z",
  expires_at: "2026-10-15T00:30:00Z",
};

beforeEach(() => {
  window.localStorage.clear();
  vi.stubGlobal("crypto", webcrypto);
});
afterEach(() => {
  cancelTenantTransition();
  vi.unstubAllGlobals();
});

function fixture(
  options: {
    disabled?: boolean;
    files?: unknown[];
    failContent?: boolean;
    failRetry?: boolean;
  } = {},
) {
  const requests: { url: string; init?: RequestInit }[] = [];
  let pending: { [key: string]: unknown } | undefined;
  let contentAttempts = 0;
  let savedFiles = options.files ?? [];
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    requests.push({ url, init });
    if (url.endsWith("/iam/me/permissions"))
      return Response.json({
        data: ["workforce.documents.manage"],
        subject: "user-documents",
        tenant_id: tenant,
      });
    if (url.endsWith("/iam/me/tenants"))
      return Response.json({
        data: [{ tenant_id: tenant, current: true, roles: ["document_manager"] }],
      });
    if (url.endsWith("/document-records"))
      return Response.json({
        uploads_available: !options.disabled,
        records: [
          {
            record_kind: "enrollment",
            record_id: staff.id,
            label: "Staff enrollment",
            categories: ["employment_evidence", "identity_evidence"],
          },
        ],
      });
    if (init?.method === "PUT") {
      if (url.endsWith("/revocation")) {
        const revoked = { ...evidence, status: "revoked" };
        savedFiles = [revoked];
        return Response.json(revoked);
      }
      if (url.includes("/retries/")) {
        if (options.failRetry)
          return Response.json(
            { error: "unavailable", message: "Synthetic retry unavailable" },
            { status: 503 },
          );
        return Response.json({
          ...evidence,
          status: "scan_pending",
          scan_attempt: 2,
          failure_code: null,
        });
      }
      if (url.endsWith("/content")) {
        if (options.failContent && contentAttempts++ === 0)
          return Response.json(
            {
              error: "documents_unavailable",
              message: "Storage is temporarily unavailable. Retry the same upload.",
            },
            { status: 503 },
          );
        pending = { ...pending, status: "scan_pending", uploaded_at: "2026-10-15T00:00:00Z" };
        return Response.json(pending, { status: 202 });
      }
      pending = {
        ...evidence,
        ...JSON.parse(String(init.body)),
        id: url.split("/").at(-1),
        status: "upload_pending",
        scan_attempt: 1,
        uploaded_at: null,
        failure_code: null,
      };
      return Response.json(pending, { status: 201 });
    }
    if (url.endsWith("/documents"))
      return Response.json({
        data: pending ? [pending] : savedFiles,
        page: { has_more: false, next_cursor: null },
      });
    throw new Error("Unexpected synthetic route: " + url);
  });
  vi.stubGlobal("fetch", fetcher);
  return { requests, fetcher };
}

it("loads document choices without fetching protected enrollment and respects unavailable storage", async () => {
  const { requests } = fixture({ disabled: true });
  renderWithProviders(<StaffDocumentPanel hospital={hospital} staff={staff} onClose={() => {}} />);
  expect(await screen.findByRole("heading", { name: "Staff documents" })).toBeInTheDocument();
  expect(await screen.findByText(/File storage is not available/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Upload file" })).toBeDisabled();
  expect(requests.some((request) => request.url.endsWith("/enrollment"))).toBe(false);
  for (const request of requests.filter((request) => request.url.includes("/document")))
    expect(new Headers(request.init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
});

it("opens documents with document permission only and closes the private workspace before adding another staff member", async () => {
  const { fetcher } = fixture();
  const documentFetch = fetcher.getMockImplementation()!;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/hospitals"))
        return Response.json({ data: [hospital], page: { has_more: false, next_cursor: null } });
      if (url.endsWith("/staff"))
        return Response.json({ data: [staff], page: { has_more: false, next_cursor: null } });
      return documentFetch(url, init);
    }),
  );
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Documents for Avery Synthetic" }));
  const heading = await screen.findByRole("heading", { name: "Staff documents" });
  expect(heading).toHaveFocus();
  await user.click(screen.getByRole("button", { name: "Add staff" }));
  expect(screen.queryByRole("heading", { name: "Staff documents" })).not.toBeInTheDocument();
  expect(await screen.findByRole("heading", { name: "Add staff member" })).toBeInTheDocument();
});

it("retains the same intent when retrying an interrupted upload", async () => {
  const { requests } = fixture({ failContent: true });
  const { user } = renderWithProviders(
    <StaffDocumentPanel hospital={hospital} staff={staff} onClose={() => {}} />,
  );
  await screen.findByRole("combobox", { name: "Evidence purpose" });
  await user.upload(
    screen.getByLabelText("Choose file"),
    new File([new Uint8Array([1, 2, 3])], "synthetic-image.png", { type: "image/png" }),
  );
  await user.click(screen.getByRole("button", { name: "Upload file" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Retry the same upload");
  await user.click(screen.getByRole("button", { name: "Retry upload" }));
  await waitFor(() =>
    expect(requests.filter((request) => request.url.endsWith("/content"))).toHaveLength(2),
  );
  const intents = requests.filter(
    (request) => request.init?.method === "PUT" && !request.url.endsWith("/content"),
  );
  expect(intents).toHaveLength(2);
  expect(intents[0].url).toBe(intents[1].url);
  expect(intents[0].init?.body).toBe(intents[1].init?.body);
  expect(await screen.findByText("Processing")).toBeInTheDocument();
});

it("shows unsafe filename validation beside the file input before any upload request", async () => {
  const { requests } = fixture();
  const { user } = renderWithProviders(
    <StaffDocumentPanel hospital={hospital} staff={staff} onClose={() => {}} />,
  );
  await screen.findByRole("combobox", { name: "Evidence purpose" });
  await user.upload(
    screen.getByLabelText("Choose file"),
    new File([new Uint8Array([1, 2, 3])], "../synthetic.png", { type: "image/png" }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(/safe name/);
  expect(screen.getByLabelText("Choose file")).toHaveAttribute("aria-invalid", "true");
  expect(requests.some((request) => request.init?.method === "PUT")).toBe(false);
});

it("requires confirmation before revoking a ready document and removes its download action", async () => {
  const { requests } = fixture({
    files: [{ ...evidence, status: "available", failure_code: null }],
  });
  const { user } = renderWithProviders(
    <StaffDocumentPanel hospital={hospital} staff={staff} onClose={() => {}} />,
  );
  expect(await screen.findByRole("button", { name: "Download" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: "Revoke access" }));
  const dialog = await screen.findByRole("dialog", { name: "Revoke file access?" });
  expect(requests.some((request) => request.url.endsWith("/revocation"))).toBe(false);
  await user.click(within(dialog).getByRole("button", { name: "Revoke access" }));
  expect(await screen.findByText("Download access has been revoked.")).toBeInTheDocument();
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "Download" })).not.toBeInTheDocument(),
  );
  expect(requests.filter((request) => request.url.endsWith("/revocation"))).toHaveLength(1);
  expect(await screen.findByText("Revoked", { exact: true })).toBeInTheDocument();
});

it("cancels file preparation when another staff workspace opens", async () => {
  const { requests } = fixture();
  let finishRead!: (value: ArrayBuffer) => void;
  const file = new File([new Uint8Array([1, 2, 3])], "synthetic.png", { type: "image/png" });
  Object.defineProperty(file, "arrayBuffer", {
    value: () =>
      new Promise<ArrayBuffer>((resolve) => {
        finishRead = resolve;
      }),
  });
  function SwitchableWorkspace() {
    const [current, setCurrent] = useState(staff);
    return (
      <>
        <button
          onClick={() =>
            setCurrent({
              ...staff,
              id: "55555555-5555-4555-8555-555555555555",
              display_name: "Morgan Synthetic",
            })
          }
        >
          Next staff
        </button>
        <StaffDocumentPanel hospital={hospital} staff={current} onClose={() => {}} />
      </>
    );
  }
  const { user } = renderWithProviders(<SwitchableWorkspace />);
  await screen.findByRole("combobox", { name: "Evidence purpose" });
  await user.upload(screen.getByLabelText("Choose file"), file);
  await user.click(screen.getByRole("button", { name: "Upload file" }));
  await screen.findByRole("button", { name: "Uploading…" });
  await user.click(screen.getByRole("button", { name: "Next staff" }));
  finishRead(new Uint8Array([1, 2, 3]).buffer);
  expect(await screen.findByText(/Morgan Synthetic/)).toBeInTheDocument();
  await waitFor(() => expect(screen.getByRole("button", { name: "Upload file" })).toBeDisabled());
  expect(screen.queryByText("Selected:")).not.toBeInTheDocument();
  expect(requests.some((request) => request.init?.method === "PUT")).toBe(false);
});

it("keeps the same manual scan attempt after a lost acknowledgement and clears the workspace on a tenant transition", async () => {
  const { requests } = fixture({ files: [evidence], failRetry: true });
  const { user } = renderWithProviders(
    <StaffDocumentPanel hospital={hospital} staff={staff} onClose={() => {}} />,
  );
  await user.click(await screen.findByRole("button", { name: "Retry scan" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Synthetic retry unavailable");
  await user.click(screen.getByRole("button", { name: "Retry previous request" }));
  await waitFor(() =>
    expect(requests.filter((request) => request.url.includes("/retries/"))).toHaveLength(2),
  );
  const retries = requests.filter((request) => request.url.includes("/retries/"));
  expect(retries[0].url).toBe(retries[1].url);
  expect(retries[0].url).toContain("/retries/2");
  beginTenantTransition();
  await waitFor(() => expect(screen.queryByText("synthetic-evidence.png")).not.toBeInTheDocument());
  expect(
    await screen.findByText("Switching tenant. This document workspace has been cleared."),
  ).toBeInTheDocument();
});
