import { NavigationGuardProvider } from "@helivanta/ui";
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
    commitRetryBeforeFailure?: boolean;
    classification?: boolean;
  } = {},
) {
  const requests: { url: string; init?: RequestInit }[] = [];
  let pending: { [key: string]: unknown } | undefined;
  let contentAttempts = 0;
  let retryRequests = 0;
  let classificationState = {
    file_id: evidence.id,
    hospital_id: hospital.id,
    staff_id: staff.id,
    version: 0,
    classification_revision_id: "",
    classification_name: "",
    updated_at: null as string | null,
  };
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
    if (url.endsWith("/classification")) {
      if (!options.classification)
        return Response.json(
          { error: "not_found", message: "Previous API does not expose this view." },
          { status: 404 },
        );
      if (init?.method === "PUT") {
        const input = JSON.parse(String(init.body));
        classificationState = {
          ...classificationState,
          version: classificationState.version + 1,
          classification_revision_id: input.classification_revision_id,
          classification_name: input.classification_revision_id ? "Synthetic file category" : "",
          updated_at: "2026-10-28T00:00:00Z",
        };
      }
      return Response.json(classificationState);
    }
    if (url.includes("/classification/history?"))
      return Response.json({
        data: classificationState.version
          ? [
              {
                id: "99999999-9999-4999-8999-999999999999",
                file_id: evidence.id,
                hospital_id: hospital.id,
                staff_id: staff.id,
                version: classificationState.version,
                classification_revision_id: classificationState.classification_revision_id,
                classification_name: classificationState.classification_name,
                actor_subject: "synthetic-admin",
                created_at: "2026-10-28T00:00:00Z",
              },
            ]
          : [],
        page: { has_more: false, next_cursor: null },
      });
    if (url.includes("/hr/masters?kind=document_category"))
      return Response.json({
        data: [
          {
            id: "77777777-7777-4777-8777-777777777777",
            revision_id: "88888888-8888-4888-8888-888888888888",
            kind: "document_category",
            code: "SYN-FILE",
            name: "Synthetic file category",
            description: "Synthetic only",
            active: true,
            version: 1,
            created_at: "2026-10-28T00:00:00Z",
            updated_at: "2026-10-28T00:00:00Z",
          },
        ],
        page: { has_more: false, next_cursor: null },
      });
    if (init?.method === "PUT") {
      if (url.endsWith("/revocation")) {
        const revoked = { ...evidence, status: "revoked" };
        savedFiles = [revoked];
        return Response.json(revoked);
      }
      if (url.includes("/retries/")) {
        if (options.failRetry && (!options.commitRetryBeforeFailure || retryRequests++ === 0)) {
          if (options.commitRetryBeforeFailure)
            savedFiles = [
              {
                ...evidence,
                status: "scan_pending",
                scan_attempt: 2,
                failure_code: null,
                updated_at: new Date(Date.now()).toISOString(),
              },
            ];
          return Response.json(
            { error: "unavailable", message: "Synthetic retry unavailable" },
            { status: 503 },
          );
        }
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
  renderWithProviders(
    <StaffDocumentPanel
      scope={{ tenantId: tenant, subject: "user-documents" }}
      onContextChanged={() => {}}
      hospital={hospital}
      staff={staff}
      onClose={() => {}}
    />,
  );
  expect(await screen.findByRole("heading", { name: "Staff documents" })).toBeInTheDocument();
  expect(await screen.findByText(/File storage is not available/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Upload file" })).toBeDisabled();
  expect(requests.some((request) => request.url.endsWith("/enrollment"))).toBe(false);
  for (const request of requests.filter((request) => request.url.includes("/document"))) {
    expect(new Headers(request.init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
    expect(new Headers(request.init?.headers).get("X-Helivanta-Expected-Subject")).toBe(
      "user-documents",
    );
  }
});

it("opens documents with document and staff permissions and closes them before adding staff", async () => {
  const { fetcher } = fixture();
  const documentFetch = fetcher.getMockImplementation()!;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/iam/me/permissions"))
        return Response.json({
          tenant_id: tenant,
          subject: "user-documents",
          data: ["workforce.documents.manage", "workforce.staff.manage"],
        });
      if (url.endsWith("/hospitals"))
        return Response.json({ data: [hospital], page: { has_more: false, next_cursor: null } });
      if (url.endsWith("/staff"))
        return Response.json({ data: [staff], page: { has_more: false, next_cursor: null } });
      return documentFetch(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Workforce />
    </NavigationGuardProvider>,
  );
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
    <StaffDocumentPanel
      scope={{ tenantId: tenant, subject: "user-documents" }}
      onContextChanged={() => {}}
      hospital={hospital}
      staff={staff}
      onClose={() => {}}
    />,
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
    <StaffDocumentPanel
      scope={{ tenantId: tenant, subject: "user-documents" }}
      onContextChanged={() => {}}
      hospital={hospital}
      staff={staff}
      onClose={() => {}}
    />,
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
    <StaffDocumentPanel
      scope={{ tenantId: tenant, subject: "user-documents" }}
      onContextChanged={() => {}}
      hospital={hospital}
      staff={staff}
      onClose={() => {}}
    />,
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
        <StaffDocumentPanel
          scope={{ tenantId: tenant, subject: "user-documents" }}
          onContextChanged={() => {}}
          hospital={hospital}
          staff={current}
          onClose={() => {}}
        />
      </>
    );
  }
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <SwitchableWorkspace />
    </NavigationGuardProvider>,
  );
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
    <StaffDocumentPanel
      scope={{ tenantId: tenant, subject: "user-documents" }}
      onContextChanged={() => {}}
      hospital={hospital}
      staff={staff}
      onClose={() => {}}
    />,
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

it("opens one scoped classification and keeps an older API fail closed", async () => {
  const { requests } = fixture({ files: [evidence] });
  const { user } = renderWithProviders(
    <StaffDocumentPanel
      scope={{ tenantId: tenant, subject: "user-documents" }}
      onContextChanged={() => {}}
      hospital={hospital}
      staff={staff}
      onClose={() => {}}
    />,
  );
  await user.click(await screen.findByRole("button", { name: `Classify ${evidence.filename}` }));
  expect(await screen.findByText(/Document classification is not available/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Save classification" })).not.toBeInTheDocument();
  const request = requests.find((row) =>
    row.url.endsWith(`/staff/${staff.id}/documents/${evidence.id}/classification`),
  );
  expect(request).toBeDefined();
  expect(new Headers(request?.init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  expect(requests.some((row) => row.url.endsWith("/enrollment"))).toBe(false);
});

it("classifies a saved file through Global API without changing its evidence purpose", async () => {
  const { requests } = fixture({ files: [evidence], classification: true });
  const { user } = renderWithProviders(
    <StaffDocumentPanel
      scope={{ tenantId: tenant, subject: "user-documents" }}
      onContextChanged={() => {}}
      hospital={hospital}
      staff={staff}
      onClose={() => {}}
    />,
  );
  await user.click(await screen.findByRole("button", { name: `Classify ${evidence.filename}` }));
  const categoryControl = await screen.findByRole("combobox", { name: "Document classification" });
  await waitFor(() => expect(categoryControl).toHaveFocus());
  await user.click(categoryControl);
  await user.click(screen.getByRole("option", { name: /Synthetic file category/ }));
  await user.click(screen.getByRole("button", { name: "Save classification" }));
  expect(await screen.findByText("Document classification saved.")).toBeInTheDocument();
  await waitFor(() =>
    expect(screen.getByRole("combobox", { name: "Document classification" })).toHaveTextContent(
      "Synthetic file category",
    ),
  );
  const writes = requests.filter(
    (row) => row.url.endsWith("/classification") && row.init?.method === "PUT",
  );
  expect(writes).toHaveLength(1);
  expect(JSON.parse(String(writes[0].init?.body))).toEqual({
    expected_version: 0,
    classification_revision_id: "88888888-8888-4888-8888-888888888888",
  });
  for (const row of requests.filter(
    (row) => row.url.includes("/classification") || row.url.includes("/hr/masters"),
  )) {
    expect(row.url).toContain("/api/v1/workforce/");
    expect(new Headers(row.init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  }
  expect(screen.getByText(/Employment evidence ·/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Show classification history" }));
  expect(
    await within(await screen.findByRole("region", { name: "Classification history" })).findByText(
      /Version 1/,
    ),
  ).toBeInTheDocument();
});

it("offers retry for an expired pending scan while keeping a recent pending scan protected", async () => {
  const clock = vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-28T00:46:00Z"));
  try {
    const pending = {
      ...evidence,
      status: "scan_pending",
      failure_code: null,
      updated_at: "2026-10-28T00:00:00Z",
    };
    const recent = {
      ...pending,
      id: "66666666-6666-4666-8666-666666666666",
      filename: "recent-processing.png",
      updated_at: "2026-10-28T00:45:00Z",
    };
    const { requests } = fixture({ files: [pending, recent], failRetry: true });
    const { user } = renderWithProviders(
      <StaffDocumentPanel
        scope={{ tenantId: tenant, subject: "user-documents" }}
        onContextChanged={() => {}}
        hospital={hospital}
        staff={staff}
        onClose={() => {}}
      />,
    );
    await screen.findByText(recent.filename);
    expect(screen.getAllByRole("button", { name: "Retry scan" })).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Retry scan" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Synthetic retry unavailable");
    await user.click(screen.getByRole("button", { name: "Retry previous request" }));
    await waitFor(() =>
      expect(requests.filter((r) => r.url.includes("/retries/"))).toHaveLength(2),
    );
    expect(requests.filter((r) => r.url.includes("/retries/")).map((r) => r.url)).toEqual([
      expect.stringContaining(`/documents/${evidence.id}/retries/2`),
      expect.stringContaining(`/documents/${evidence.id}/retries/2`),
    ]);
  } finally {
    clock.mockRestore();
  }
});

it("confirms the same uncertain attempt after refresh shows it already pending", async () => {
  const clock = vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-28T00:46:00Z"));
  try {
    const { requests } = fixture({
      files: [
        {
          ...evidence,
          status: "scan_pending",
          failure_code: null,
          updated_at: "2026-10-28T00:00:00Z",
        },
      ],
      failRetry: true,
      commitRetryBeforeFailure: true,
    });
    const { user } = renderWithProviders(
      <StaffDocumentPanel
        scope={{ tenantId: tenant, subject: "user-documents" }}
        onContextChanged={() => {}}
        hospital={hospital}
        staff={staff}
        onClose={() => {}}
      />,
    );
    await user.click(await screen.findByRole("button", { name: "Retry scan" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Synthetic retry unavailable");
    await user.click(screen.getByRole("button", { name: "Refresh status" }));
    await waitFor(() =>
      expect(screen.queryByText(/Processing has not completed/)).not.toBeInTheDocument(),
    );
    await user.click(await screen.findByRole("button", { name: "Retry previous request" }));
    expect(await screen.findByText("The scan request has been confirmed.")).toBeInTheDocument();
    expect(requests.filter((r) => r.url.includes("/retries/")).map((r) => r.url)).toEqual([
      expect.stringContaining("/retries/2"),
      expect.stringContaining("/retries/2"),
    ]);
  } finally {
    clock.mockRestore();
  }
});

it("clears an in-flight file preparation when the account changes within the same tenant", async () => {
  const { requests } = fixture();
  let finishRead!: (value: ArrayBuffer) => void;
  const file = new File([new Uint8Array([1, 2, 3])], "account-bound.png", { type: "image/png" });
  Object.defineProperty(file, "arrayBuffer", {
    value: () =>
      new Promise<ArrayBuffer>((resolve) => {
        finishRead = resolve;
      }),
  });
  function AccountWorkspace() {
    const [subject, setSubject] = useState("user-documents");
    return (
      <>
        <button onClick={() => setSubject("synthetic-other-document-account")}>
          Change document account
        </button>
        <StaffDocumentPanel
          scope={{ tenantId: tenant, subject }}
          onContextChanged={() => {}}
          hospital={hospital}
          staff={staff}
          onClose={() => {}}
        />
      </>
    );
  }
  const { user } = renderWithProviders(<AccountWorkspace />);
  await screen.findByRole("combobox", { name: "Evidence purpose" });
  await user.upload(screen.getByLabelText("Choose file"), file);
  await user.click(screen.getByRole("button", { name: "Upload file" }));
  await screen.findByRole("button", { name: "Uploading…" });
  await user.click(screen.getByRole("button", { name: "Change document account" }));
  finishRead(new Uint8Array([1, 2, 3]).buffer);
  await waitFor(() => expect(screen.getByRole("button", { name: "Upload file" })).toBeDisabled());
  expect(screen.queryByText(/account-bound.png/)).not.toBeInTheDocument();
  expect(requests.some((request) => request.init?.method === "PUT")).toBe(false);
  expect(
    requests.some(
      (request) =>
        request.url.endsWith("/document-records") &&
        new Headers(request.init?.headers).get("X-Helivanta-Expected-Subject") ===
          "synthetic-other-document-account",
    ),
  ).toBe(true);
});

it("clears protected file state when a command reports a changed workspace", async () => {
  const { fetcher } = fixture({ files: [evidence] });
  const original = fetcher.getMockImplementation()!;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/retries/"))
        return Response.json(
          { error: "session_changed", message: "Synthetic account changed" },
          { status: 409 },
        );
      return original(url, init);
    }),
  );
  const onContextChanged = vi.fn();
  const { user } = renderWithProviders(
    <StaffDocumentPanel
      scope={{ tenantId: tenant, subject: "user-documents" }}
      onContextChanged={onContextChanged}
      hospital={hospital}
      staff={staff}
      onClose={() => {}}
    />,
  );
  await user.click(await screen.findByRole("button", { name: "Retry scan" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    /account, workspace or access changed/,
  );
  expect(screen.queryByText("synthetic-evidence.png")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Retry previous request" })).not.toBeInTheDocument();
  await waitFor(() => expect(onContextChanged).toHaveBeenCalled());
});
