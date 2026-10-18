import "../../test/select";
import { screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { WorkforceInsights } from "./workforce-insights";

const a = "11111111-1111-4111-8111-111111111111";
const b = "22222222-2222-4222-8222-222222222222";
const page = (data: unknown[], more = false) => ({
  data,
  page: { has_more: more, next_cursor: more ? "page2" : null },
});
afterEach(() => vi.unstubAllGlobals());

it("loads all hospital choices and changes the protected snapshot scope", async () => {
  const reads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      reads.push(url);
      let body: unknown = url.includes("cursor=")
        ? page([{ id: b, name: "Second Hospital", access_level: "reader" }])
        : page([{ id: a, name: "First Hospital", access_level: "reader" }], true);
      if (url.includes("/insights"))
        body = {
          hospital_id: url.includes(b) ? b : a,
          as_at: "2026-10-18T10:00:00Z",
          counts: [{ category: "doctor", status: "active", count: url.includes(b) ? 4 : 12 }],
        };
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  const { user } = renderWithProviders(<WorkforceInsights />);
  const region = screen.getByRole("region", { name: "Doctors & care teams" });
  expect((await within(region).findAllByText("12")).length).toBeGreaterThan(0);
  await user.click(screen.getByRole("button", { name: "Load more" }));
  await chooseOption(user, screen.getByLabelText("Staff hospital"), "Second Hospital");
  expect((await within(region).findAllByText("4")).length).toBeGreaterThan(0);
  expect(reads.some((url) => url.includes(`${b}/insights`))).toBe(true);
  expect(within(region).queryByText("12")).not.toBeInTheDocument();
});

it("does not display another hospital's response", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.includes("/insights")
              ? {
                  hospital_id: b,
                  as_at: "2026-10-18T10:00:00Z",
                  counts: [{ category: "doctor", status: "active", count: 123 }],
                }
              : page([{ id: a, name: "First Hospital", access_level: "reader" }]),
          ),
        ),
    ),
  );
  renderWithProviders(<WorkforceInsights />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load team insights");
  expect(screen.queryByText("123")).not.toBeInTheDocument();
});

it("shows no headcount when hospital access is absent", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify(page([])))),
  );
  renderWithProviders(<WorkforceInsights />);
  expect(await screen.findByText(/No hospital staff access/)).toBeInTheDocument();
  expect(screen.queryByText("Active doctors")).not.toBeInTheDocument();
});
