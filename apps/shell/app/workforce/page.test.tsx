import { chooseOption } from "../../test/select";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import Workforce from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("loads the granted hospital and saves staff through its API", async () => {
  let saved: Record<string, string> | null = null;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    let body: unknown = { data: [] };
    if (url.endsWith("/workforce/hospitals"))
      body = page([{ id: hospital, name: "YES HOSPITAL", access_level: "manager" }]);
    if (url.includes("/staff")) body = page(saved ? [{ id: "staff-1", ...saved }] : []);
    if (init?.method === "PUT") {
      saved = JSON.parse(String(init.body));
      body = saved;
    }
    return new Response(JSON.stringify(body), { status: 200 });
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Add staff" }));
  await user.type(screen.getByLabelText("Full name"), "Synthetic Nurse");
  await user.type(screen.getByLabelText("Employee code"), "YES-101");
  await chooseOption(user, screen.getByLabelText("Job category"), "Nurse");
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await waitFor(() =>
    expect(fetcher).toHaveBeenCalledWith(
      expect.stringContaining(`/hospitals/${hospital}/staff/`),
      expect.objectContaining({ method: "PUT" }),
    ),
  );
  expect(await screen.findByText("Synthetic Nurse")).toBeInTheDocument();
  expect(screen.getByText(/Powered by/)).toBeInTheDocument();
});
it("shows an actionable API failure rather than an empty directory", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ message: "Unavailable" }), { status: 503 })),
  );
  renderWithProviders(<Workforce />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load hospitals");
  expect(screen.queryByRole("button", { name: "Add staff" })).not.toBeInTheDocument();
});

it("lets a tenant administrator explicitly create and grant their first hospital", async () => {
  const requests: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") requests.push(url);
      const body = url.includes("permissions")
        ? { data: ["workforce.hospital.manage"], subject: "owner-1", tenant_id: hospital }
        : url.includes("hospitals")
          ? page([])
          : {};
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByText("Add a hospital"));
  await user.type(screen.getByLabelText("Hospital name"), "YES Main Hospital");
  await user.click(screen.getByLabelText("Grant me staff manager access to this hospital"));
  await user.click(screen.getByRole("button", { name: "Create hospital" }));
  await waitFor(() => expect(requests).toHaveLength(2));
  expect(requests[1]).toMatch(/\/access\/owner-1$/);
});
