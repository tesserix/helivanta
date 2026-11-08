import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import { screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import PharmacyRelationships from "./pharmacy-relationships/page";
import { PostingCutoffPage as PostingCutoffs } from "./posting-cutoff-page";
import PostingReviews from "./posting-reviews/page";

// #1595: these pages verify the account before opening a workspace. Their
// identity read has no "Staff access changed" branch, so it is the one
// place a refusal reaches the failure itself.
const pages = [
  ["pharmacy relationships", PharmacyRelationships, "Your account could not be verified."],
  ["posting cutoffs", PostingCutoffs, "Your posting cutoff account could not be verified."],
  ["posting reviews", PostingReviews, "Your posting review account could not be verified."],
] as const;
afterEach(() => vi.unstubAllGlobals());

function permissionsAnswer(status: number) {
  const fetcher = vi.fn(async () => Response.json({ error: "x", message: "secret" }, { status }));
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

it.each(pages)("%s ends the session on a 401 with no retry", async (_, Page) => {
  permissionsAnswer(401);
  renderWithProviders(
    <NavigationGuardProvider>
      <Page />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Your session has ended. Sign in again to continue.",
  );
  expect(screen.queryByRole("button")).toBeNull();
});

it.each(pages)("%s asks again after an outage", async (_, Page, message) => {
  const fetcher = permissionsAnswer(503);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Page />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(message);
  const reads = fetcher.mock.calls.length;
  await user.click(screen.getByRole("button", { name: "Try again" }));
  await vi.waitFor(() => expect(fetcher.mock.calls.length).toBeGreaterThan(reads));
});
