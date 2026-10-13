import { chooseOption } from "../../../test/select";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { DEFAULT_BRANDING } from "@helivanta/api";
import BrandingPage from "./page";
afterEach(() => vi.unstubAllGlobals());
it("previews and persists owner branding while retaining platform attribution", async () => {
  const fetcher = vi.fn(
    async (url: string, init?: RequestInit) =>
      new Response(
        JSON.stringify(
          url.includes("permissions")
            ? {
                data: ["workspace.branding.manage"],
                tenant_id: "11111111-1111-4111-8111-111111111111",
              }
            : init?.body
              ? JSON.parse(String(init.body))
              : DEFAULT_BRANDING,
        ),
        { status: 200 },
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(<BrandingPage />);
  const name = await screen.findByLabelText("Organization name");
  await user.clear(name);
  await user.type(name, "YES HOSPITAL");
  await chooseOption(user, screen.getByLabelText("Medical theme"), "Care blue");
  await user.click(screen.getByRole("button", { name: "Publish branding" }));
  await waitFor(() =>
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/workspace/branding",
      expect.objectContaining({
        method: "PUT",
        body: expect.stringContaining('"display_name":"YES HOSPITAL"'),
      }),
    ),
  );
  expect(screen.getAllByText(/Powered by/).length).toBeGreaterThan(0);
});
