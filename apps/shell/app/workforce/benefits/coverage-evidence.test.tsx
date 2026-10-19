import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../../test/select";
import {
  APPROVE,
  coverage,
  dependentID,
  enrollment,
  enrollmentID,
  evidenceFile,
  hospital,
  lifeID,
  mockBenefits,
  proposal,
  proposalID,
  staffID,
  VIEW,
} from "../../../test/benefits";
import Benefits from "./page";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

type User = Awaited<ReturnType<typeof renderWithProviders>>["user"];
const proposals = `/workforce/hospitals/${hospital}/staff/${staffID}/benefit-enrollments/${enrollmentID}/proposals`;
const partnerHealth = "Synthetic Health · Synthetic Partner (Synthetic Spouse)";
const waiting = enrollment({
  coverage,
  pending: proposal({ base_version: 1, dependent_id: dependentID }),
});
const approvedFile = evidenceFile({
  id: "ffffffff-ffff-4fff-8fff-fffffffffff1",
  record_id: coverage.proposal_id,
  filename: "synthetic-approved-certificate.png",
});
const scanning = evidenceFile({ status: "scan_pending", scanned_at: null });
const heldReason = /Evidence is still being scanned. You can decide once every file has finished./;

const openCoverage = async (user: User) => {
  const panel = await screen.findByRole("region", { name: "Staff benefits" });
  await chooseOption(
    user,
    await within(panel).findByRole("combobox", { name: "Staff member" }),
    "Synthetic Nurse · SYN-1",
  );
  return screen.findByRole("region", { name: "Coverage" });
};

it("lists the evidence behind coverage in force and attaches only to the waiting proposal", async () => {
  const { reads, uploads } = mockBenefits({
    enrollments: [
      waiting,
      enrollment({ id: "cccccccc-cccc-4ccc-8ccc-cccccccccccd", plan_id: lifeID }),
    ],
    evidence: [approvedFile, evidenceFile()],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  expect(
    within(section).queryByRole("button", {
      name: "Evidence for Synthetic Life · Synthetic Partner (Synthetic Spouse)",
    }),
  ).toBeNull();
  await user.click(
    await within(section).findByRole("button", { name: `Evidence for ${partnerHealth}` }),
  );
  const inForce = await within(section).findByRole("region", {
    name: "Evidence for the coverage in force",
  });
  expect(
    await within(inForce).findByText("synthetic-approved-certificate.png"),
  ).toBeInTheDocument();
  expect(within(inForce).getByText("Ready")).toBeInTheDocument();
  expect(
    within(inForce).getByRole("button", { name: "Download synthetic-approved-certificate.png" }),
  ).toBeInTheDocument();
  expect(within(inForce).queryByRole("form")).toBeNull();
  const proposed = within(section).getByRole("region", {
    name: "Evidence for the proposed coverage",
  });
  expect(
    await within(proposed).findByText("synthetic-marriage-certificate.png"),
  ).toBeInTheDocument();
  expect(reads).toContain(`/api/v1${proposals}/${coverage.proposal_id}/evidence`);
  expect(reads).toContain(`/api/v1${proposals}/${proposalID}/evidence`);

  const form = within(proposed).getByRole("form", { name: "Attach eligibility evidence" });
  await user.click(within(form).getByRole("button", { name: "Upload evidence" }));
  expect(await within(form).findByText("Choose a file first.")).toBeInTheDocument();
  const input = within(form).getByLabelText(/Marriage, birth or other eligibility proof/);
  await user.upload(input, new File([new Uint8Array(10485761)], "huge.png", { type: "image/png" }));
  expect(await within(form).findByText("Choose an image up to 10 MB.")).toBeInTheDocument();
  expect(uploads).toHaveLength(0);
  const image = new File([new Uint8Array([137, 80, 78, 71])], "synthetic-birth.png", {
    type: "image/png",
  });
  await user.upload(input, image);
  await user.click(within(form).getByRole("button", { name: "Upload evidence" }));
  await waitFor(() => expect(uploads).toHaveLength(2));
  expect(uploads[0]).toEqual({
    url: expect.stringMatching(new RegExp(`${proposals}/${proposalID}/evidence/[0-9a-f-]{36}$`)),
    body: {
      filename: "synthetic-birth.png",
      content_type: "image/png",
      size: 4,
      sha256: expect.stringMatching(/^[a-f0-9]{64}$/),
    },
  });
  expect(uploads[1]!.url).toBe(`${uploads[0]!.url}/content`);
  expect(uploads[1]!.body).toBe(image);
  expect(await screen.findByText("Evidence uploaded")).toBeInTheDocument();
});

it("shows a reader the evidence without any way to attach", async () => {
  mockBenefits({
    permissions: [VIEW],
    subject: "synthetic-reader",
    access: "reader",
    enrollments: [waiting],
    evidence: [evidenceFile()],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  await user.click(
    await within(section).findByRole("button", { name: `Evidence for ${partnerHealth}` }),
  );
  const proposed = await within(section).findByRole("region", {
    name: "Evidence for the proposed coverage",
  });
  expect(
    await within(proposed).findByText("synthetic-marriage-certificate.png"),
  ).toBeInTheDocument();
  expect(within(proposed).queryByRole("form")).toBeNull();
});

it("holds the review with the reason while a file is still scanning", async () => {
  const { writes } = mockBenefits({
    permissions: [VIEW, APPROVE],
    subject: "synthetic-checker",
    enrollments: [waiting],
    evidence: [scanning],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  await user.click(
    await within(section).findByRole("button", { name: `Review proposal for ${partnerHealth}` }),
  );
  const form = within(section).getByRole("form", { name: "Review coverage proposal" });
  const evidence = within(form).getByRole("region", { name: "Evidence for the proposed coverage" });
  expect(await within(evidence).findByText("Scanning")).toBeInTheDocument();
  expect(within(evidence).queryByRole("form")).toBeNull();
  expect(within(form).getByText(heldReason)).toBeInTheDocument();
  expect(within(form).getByRole("button", { name: "Approve coverage" })).toBeDisabled();
  expect(within(form).getByRole("button", { name: "Reject coverage" })).toBeDisabled();
  expect(within(form).getByRole("button", { name: "Close" })).toBeEnabled();
  expect(writes).toHaveLength(0);
});

it("lets the reviewer download evidence without deciding, then decide once scans finish", async () => {
  const create = vi.fn(() => "blob:synthetic-evidence");
  vi.stubGlobal(
    "URL",
    Object.assign(class extends URL {}, { createObjectURL: create, revokeObjectURL: vi.fn() }),
  );
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
  const { writes, reads } = mockBenefits({
    permissions: [VIEW, APPROVE],
    subject: "synthetic-checker",
    enrollments: [waiting],
    evidence: [evidenceFile()],
  });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  await user.click(
    await within(section).findByRole("button", { name: `Review proposal for ${partnerHealth}` }),
  );
  const form = within(section).getByRole("form", { name: "Review coverage proposal" });
  expect(await within(form).findByText("Ready")).toBeInTheDocument();
  expect(within(form).queryByText(heldReason)).toBeNull();
  await user.click(
    within(form).getByRole("button", { name: "Download synthetic-marriage-certificate.png" }),
  );
  expect(await screen.findByText("Evidence downloaded")).toBeInTheDocument();
  expect(click).toHaveBeenCalledOnce();
  expect(reads).toContain(
    `/api/v1${proposals}/${proposalID}/evidence/${evidenceFile().id}/content`,
  );
  expect(writes).toHaveLength(0);
  expect(within(form).getByRole("button", { name: "Approve coverage" })).toBeEnabled();
});

it("holds a withdrawal with the reason while a file is still scanning", async () => {
  const { writes } = mockBenefits({ enrollments: [waiting], evidence: [scanning] });
  const { user } = renderWithProviders(<Benefits />);
  const section = await openCoverage(user);
  await user.click(
    await within(section).findByRole("button", { name: `Withdraw proposal for ${partnerHealth}` }),
  );
  const form = within(section).getByRole("form", { name: "Withdraw coverage proposal" });
  const evidence = within(form).getByRole("region", { name: "Evidence for the proposed coverage" });
  expect(await within(evidence).findByText("Scanning")).toBeInTheDocument();
  expect(within(evidence).queryByRole("form")).toBeNull();
  expect(within(form).getByText(heldReason)).toBeInTheDocument();
  expect(within(form).getByRole("button", { name: "Withdraw proposal" })).toBeDisabled();
  expect(within(form).getByRole("button", { name: "Close" })).toBeEnabled();
  expect(writes).toHaveLength(0);
});
