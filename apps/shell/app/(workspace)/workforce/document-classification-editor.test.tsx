import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import userEvent from "@testing-library/user-event";
import type { UseApiPagedQueryResult, HRMaster } from "@helivanta/api";
import { ApiError, beginTenantTransition, cancelTenantTransition } from "@helivanta/api";
import { DocumentClassificationEditor } from "./document-classification-editor";

const mocks = vi.hoisted(() => ({
  can: vi.fn(),
  switching: false,
  metadata: vi.fn(),
  history: vi.fn(),
  masters: vi.fn(),
  save: vi.fn(),
  reload: vi.fn(),
}));
vi.mock("@helivanta/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@helivanta/api")>()),
  usePermissions: () => ({ can: mocks.can }),
  useTenantTransition: () => mocks.switching,
  useDocumentClassification: (...args: unknown[]) => mocks.metadata(...args),
  useDocumentClassificationHistory: (...args: unknown[]) => mocks.history(...args),
  useApiPagedQuery: (...args: unknown[]) => mocks.masters(...args),
  saveDocumentClassification: (...args: unknown[]) => mocks.save(...args),
}));
const scope = {
  subject: "synthetic-document-account",
  tenantId: "11111111-1111-4111-8111-111111111111",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
};
const reference = "55555555-5555-4555-8555-555555555555";
const newReference = "66666666-6666-4666-8666-666666666666";
const file = {
  id: "44444444-4444-4444-8444-444444444444",
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  record_id: scope.staffId,
  record_kind: "enrollment" as const,
  record_version: 1,
  category: "identity_evidence" as const,
  filename: "synthetic-identity.png",
  content_type: "image/png" as const,
  size: 256,
  sha256: "a".repeat(64),
  status: "upload_pending" as const,
  scan_attempt: 1,
  uploaded_at: null,
  scanned_at: null,
  failure_code: null,
  expires_at: "2026-10-28T01:00:00Z",
  created_at: "2026-10-28T00:00:00Z",
  updated_at: "2026-10-28T00:00:00Z",
};
const current = {
  file_id: file.id,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  version: 3,
  classification_revision_id: reference,
  classification_name: "Synthetic recorded category",
  updated_at: "2026-10-28T00:00:00Z",
};
function mount() {
  return renderWithProviders(
    <DocumentClassificationEditor scope={scope} file={file} onClose={vi.fn()} />,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.switching = false;
  mocks.can.mockReturnValue(true);
  mocks.metadata.mockReturnValue({
    data: current,
    isPending: false,
    isFetching: false,
    isError: false,
    error: null,
    refetch: mocks.reload,
  });
  mocks.history.mockReturnValue({
    data: { data: [], page: { has_more: false, next_cursor: null } },
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  });
  mocks.masters.mockReturnValue({
    items: [
      {
        id: newReference,
        revision_id: newReference,
        kind: "document_category",
        code: "SYN-NEW",
        name: "Synthetic active category",
        active: true,
        version: 1,
        description: "Synthetic category only",
        created_at: "2026-10-28T00:00:00Z",
        updated_at: "2026-10-28T00:00:00Z",
      },
    ],
    isPending: false,
    isFetchingMore: false,
    isRefreshing: false,
    isError: false,
    error: null,
    hasMore: false,
    loadMore: vi.fn(),
    retry: vi.fn(),
  } satisfies UseApiPagedQueryResult<HRMaster>);
  mocks.save.mockResolvedValue({
    ...current,
    version: 4,
    classification_revision_id: newReference,
    classification_name: "Synthetic active category",
  });
});
afterEach(() => {
  cleanup();
  cancelTenantTransition();
});

describe("staff document classification editor", () => {
  it("does not expose Save while protected metadata loads or is unavailable", async () => {
    mocks.metadata.mockReturnValue({
      isPending: true,
      isFetching: true,
      isError: false,
      refetch: mocks.reload,
    });
    const view = mount();
    expect(screen.queryByRole("button", { name: "Save classification" })).not.toBeInTheDocument();
    mocks.metadata.mockReturnValue({
      isPending: false,
      isFetching: false,
      isError: true,
      error: new Error("previous API"),
      refetch: mocks.reload,
    });
    view.unmount();
    mount();
    expect(screen.queryByRole("button", { name: "Save classification" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(mocks.reload).toHaveBeenCalled();
  });
  it("retains a recorded retired category and sends only an explicit versioned selection", async () => {
    mount();
    expect(screen.getByRole("combobox", { name: "Document classification" })).toHaveTextContent(
      "Recorded: Synthetic recorded category",
    );
    expect(screen.getByText(/does not change the evidence purpose/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("combobox", { name: "Document classification" }));
    await userEvent.click(screen.getByRole("option", { name: /Synthetic active category/ }));
    await userEvent.click(screen.getByRole("button", { name: "Save classification" }));
    await waitFor(() =>
      expect(mocks.save).toHaveBeenCalledWith(
        scope,
        file.id,
        { expected_version: 3, classification_revision_id: newReference },
        expect.any(AbortSignal),
      ),
    );
    await waitFor(() => expect(mocks.reload).toHaveBeenCalled());
  });
  it("clears the category explicitly without changing file purpose", async () => {
    mount();
    await userEvent.click(screen.getByRole("combobox", { name: "Document classification" }));
    await userEvent.click(screen.getByRole("option", { name: "Unclassified" }));
    await userEvent.click(screen.getByRole("button", { name: "Save classification" }));
    await waitFor(() =>
      expect(mocks.save).toHaveBeenCalledWith(
        scope,
        file.id,
        { expected_version: 3, classification_revision_id: "" },
        expect.any(AbortSignal),
      ),
    );
  });
  it("freezes uncertain changes and retries the exact prior command", async () => {
    mocks.save
      .mockRejectedValueOnce(new TypeError("connection closed"))
      .mockResolvedValueOnce(current);
    mount();
    await userEvent.click(screen.getByRole("button", { name: "Save classification" }));
    expect(await screen.findByRole("button", { name: "Retry previous save" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Document classification" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save classification" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Retry previous save" }));
    await waitFor(() => expect(mocks.save).toHaveBeenCalledTimes(2));
    expect(mocks.save.mock.calls[1][2]).toEqual(mocks.save.mock.calls[0][2]);
  });
  it("denies mutation during tenant transition or without document permission", () => {
    mocks.can.mockReturnValue(false);
    const view = mount();
    expect(screen.queryByRole("button", { name: "Save classification" })).not.toBeInTheDocument();
    expect(mocks.metadata.mock.calls[0][2]).toBe(false);
    mocks.can.mockReturnValue(true);
    mocks.switching = true;
    view.unmount();
    mount();
    expect(screen.queryByRole("button", { name: "Save classification" })).not.toBeInTheDocument();
  });
  it("loads bounded history only when requested and preserves clear revisions", async () => {
    mocks.history.mockReturnValue({
      data: {
        data: [
          {
            id: newReference,
            version: 2,
            classification_revision_id: "",
            classification_name: "",
            actor_subject: "synthetic-admin",
            created_at: "2026-10-27T00:00:00Z",
          },
        ],
        page: { has_more: false, next_cursor: null },
      },
      isPending: false,
      isError: false,
      error: null,
      refetch: vi.fn(),
    });
    mount();
    expect(mocks.history.mock.calls[0][3]).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "Show classification history" }));
    expect(screen.getByRole("region", { name: "Classification history" })).toHaveTextContent(
      "Unclassified",
    );
    expect(mocks.history.mock.lastCall?.[3]).toBe(true);
  });
});

it("requires a reload after a stale write instead of retrying a new command", async () => {
  mocks.save.mockRejectedValue(new ApiError("conflict", "Synthetic stale classification", 409));
  mount();
  await userEvent.click(screen.getByRole("button", { name: "Save classification" }));
  expect(await screen.findByText(/Document classification changed. Reload/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Save classification" })).toBeDisabled();
  expect(screen.getByRole("combobox", { name: "Document classification" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Retry previous save" })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Reload classification" }));
  expect(mocks.reload).toHaveBeenCalled();
});

it("aborts an in-flight classification write when the tenant transition begins", async () => {
  mocks.save.mockImplementation(
    (_scope, _file, _input, signal: AbortSignal) =>
      new Promise((_resolve, reject) =>
        signal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), {
          once: true,
        }),
      ),
  );
  mount();
  await userEvent.click(screen.getByRole("button", { name: "Save classification" }));
  await waitFor(() => expect(mocks.save).toHaveBeenCalledTimes(1));
  const signal = mocks.save.mock.calls[0][3] as AbortSignal;
  beginTenantTransition();
  await waitFor(() => expect(signal.aborted).toBe(true));
  expect(mocks.save).toHaveBeenCalledTimes(1);
});

it("does not carry an uncertain classification command into another account", async () => {
  mocks.save.mockRejectedValueOnce(new TypeError("connection closed"));
  function Accounts() {
    const [subject, setSubject] = React.useState(scope.subject);
    return (
      <>
        <button onClick={() => setSubject("synthetic-next-account")}>
          Change classification account
        </button>
        <DocumentClassificationEditor scope={{ ...scope, subject }} file={file} onClose={vi.fn()} />
      </>
    );
  }
  renderWithProviders(<Accounts />);
  await userEvent.click(screen.getByRole("button", { name: "Save classification" }));
  await screen.findByRole("button", { name: "Retry previous save" });
  await userEvent.click(screen.getByRole("button", { name: "Change classification account" }));
  expect(screen.queryByRole("button", { name: "Retry previous save" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Save classification" })).toBeEnabled();
  expect(mocks.save).toHaveBeenCalledTimes(1);
});
