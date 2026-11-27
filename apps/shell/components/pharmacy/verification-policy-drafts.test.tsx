import { beforeEach, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { saveVerificationPolicyDraft, useVerificationPolicyDrafts } from "@helivanta/api";
import { VerificationPolicyDrafts } from "./verification-policy-drafts";
vi.mock("@helivanta/api", async (original) => ({
  ...(await original<typeof import("@helivanta/api")>()),
  useVerificationPolicyDrafts: vi.fn(),
  saveVerificationPolicyDraft: vi.fn(),
  readVerificationPolicyDraft: vi.fn(),
}));
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000010",
  pharmacyId: "0199e000-0000-7000-8000-000000000011",
  outletId: "0199e000-0000-7000-8000-000000000012",
  subject: "synthetic-preparer",
};
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(useVerificationPolicyDrafts).mockReturnValue({
    items: [],
    isPending: false,
    isError: false,
    isRefreshing: false,
    isFetchingMore: false,
    error: null,
    hasMore: false,
    loadMore: vi.fn(),
    retry: vi.fn(),
  });
});
function show() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <VerificationPolicyDrafts scope={scope} timeZone="Asia/Kolkata" />
    </QueryClientProvider>,
  );
}
it("explains preparation and requires deliberate choices without offering activation", async () => {
  const user = userEvent.setup();
  show();
  expect(screen.getByText(/do not authorize clinical verification/i)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "New policy draft" }));
  expect(screen.getByRole("heading", { name: "New administrative policy draft" })).toHaveFocus();
  expect(screen.getByLabelText("Independent second check")).toHaveTextContent("Choose explicitly");
  expect(screen.getByLabelText("Review validity (seconds)")).toHaveValue(null);
  await user.click(screen.getByRole("button", { name: "Save draft revision" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Enter");
  expect(saveVerificationPolicyDraft).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: /activate|approve|publish/i }),
  ).not.toBeInTheDocument();
});
it("retains command identity and inputs after an uncertain save for exact retry", async () => {
  const user = userEvent.setup();
  vi.mocked(saveVerificationPolicyDraft).mockRejectedValue(new Error("Connection interrupted"));
  show();
  await user.click(screen.getByRole("button", { name: "New policy draft" }));
  await user.type(screen.getByLabelText("Policy name"), "Synthetic proposal");
  await user.click(screen.getByLabelText("Independent second check"));
  await user.click(
    screen.getByRole("option", { name: "Propose a required independent second check" }),
  );
  await user.type(screen.getByLabelText("Review validity (seconds)"), "600");
  await user.type(screen.getByLabelText("Prescriber response target (seconds)"), "1200");
  await user.type(screen.getByLabelText("Reason for this revision"), "Synthetic test proposal");
  await user.click(screen.getByRole("button", { name: "Save draft revision" }));
  await screen.findByRole("button", { name: "Retry same revision" });
  const first = vi.mocked(saveVerificationPolicyDraft).mock.calls[0]!;
  expect(screen.getByLabelText("Policy name")).toHaveValue("Synthetic proposal");
  expect(screen.getByLabelText("Policy name")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry same revision" }));
  await waitFor(() => expect(saveVerificationPolicyDraft).toHaveBeenCalledTimes(2));
  expect(vi.mocked(saveVerificationPolicyDraft).mock.calls[1]!.slice(0, 4)).toEqual(
    first.slice(0, 4),
  );
  expect(screen.getByRole("button", { name: "Recover saved revision" })).toBeInTheDocument();
  expect(screen.queryByText(scope.subject)).not.toBeInTheDocument();
});

it("provides an explicit way back to current history after a confirmed version conflict", async () => {
  const { ApiError } = await import("@helivanta/api");
  vi.mocked(saveVerificationPolicyDraft).mockRejectedValue(
    new ApiError("verification_policy_draft_conflict", "Synthetic conflict", 409),
  );
  const user = userEvent.setup();
  show();
  await user.click(screen.getByRole("button", { name: "New policy draft" }));
  await user.type(screen.getByLabelText("Policy name"), "Synthetic proposal");
  await user.click(screen.getByLabelText("Independent second check"));
  await user.click(
    screen.getByRole("option", { name: "Propose a required independent second check" }),
  );
  await user.type(screen.getByLabelText("Review validity (seconds)"), "600");
  await user.type(screen.getByLabelText("Prescriber response target (seconds)"), "1200");
  await user.type(screen.getByLabelText("Reason for this revision"), "Synthetic proposal");
  await user.click(screen.getByRole("button", { name: "Save draft revision" }));
  await user.click(
    await screen.findByRole("button", { name: "Discard unsaved changes and refresh history" }),
  );
  expect(screen.getByRole("button", { name: "New policy draft" })).toBeInTheDocument();
  expect(saveVerificationPolicyDraft).toHaveBeenCalledTimes(1);
});
