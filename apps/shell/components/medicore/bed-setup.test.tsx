import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { BedSetup } from "./bed-setup";

const hospital = {
  id: "0199e000-0000-7000-8000-00000000a001",
  name: "TEST General",
  access_level: "manager" as const,
};
const page = (data: unknown[]) =>
  new Response(JSON.stringify({ data, page: { next_cursor: null, has_more: false } }));
const ward = {
  id: "0199e000-0000-7000-8000-0000000000c1",
  hospital_id: hospital.id,
  code: "W-A",
  name: "Ward A",
  created_at: "",
};
const room = {
  id: "0199e000-0000-7000-8000-0000000000c2",
  hospital_id: hospital.id,
  ward_id: ward.id,
  code: "R-1",
  created_at: "",
};

afterEach(() => vi.unstubAllGlobals());

function server(put = () => new Response("{}")) {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (init.method === "PUT") return put();
    if (url.includes("/wards")) return page([ward]);
    if (url.includes("/rooms")) return page([room]);
    return page([]);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const puts = (f: ReturnType<typeof server>) =>
  f.mock.calls.filter(([, init]) => (init as RequestInit | undefined)?.method === "PUT") as [
    string,
    RequestInit,
  ][];

describe("BedSetup", () => {
  it("adds a ward with an upper-cased code, then refuses an invalid one inline", async () => {
    const fetcher = server();
    const { user } = renderWithProviders(<BedSetup hospital={hospital} />);
    await user.type(await screen.findByLabelText("Ward code"), "w-b");
    await user.type(screen.getByLabelText("Ward name"), "Ward B");
    await user.click(screen.getByRole("button", { name: "Add ward" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(
      new RegExp(`/hospitals/${hospital.id}/wards/[0-9a-f-]{36}$`),
    );
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({ code: "W-B", name: "Ward B" });

    await user.type(screen.getByLabelText("Ward code"), "bad code");
    await user.type(screen.getByLabelText("Ward name"), "X");
    await user.click(screen.getByRole("button", { name: "Add ward" }));
    expect(await screen.findByText("Up to 20 letters, digits and dashes")).toBeInTheDocument();
    expect(puts(fetcher)).toHaveLength(1);
  });

  it("adds a bed in a chosen room with its class", async () => {
    const fetcher = server();
    const { user } = renderWithProviders(<BedSetup hospital={hospital} />);
    await chooseOption(
      user,
      await screen.findByRole("combobox", { name: "Room" }),
      /R-1 · Ward W-A/,
    );
    await user.type(screen.getByLabelText("Bed code"), "a-7");
    await chooseOption(user, screen.getByRole("combobox", { name: "Bed class" }), "ICU");
    await user.click(screen.getByRole("button", { name: "Add bed" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(new RegExp(`/rooms/${room.id}/beds/[0-9a-f-]{36}$`));
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({ code: "A-7", bed_class: "icu" });
  });

  it("shows the server's refusal of a code already used", async () => {
    server(
      () =>
        new Response(
          JSON.stringify({
            error: "conflict",
            message: "a ward, room or bed code is already used at this hospital",
          }),
          {
            status: 409,
          },
        ),
    );
    const { user } = renderWithProviders(<BedSetup hospital={hospital} />);
    await chooseOption(user, await screen.findByRole("combobox", { name: "Ward" }), /W-A/);
    await user.type(screen.getByLabelText("Room code"), "R-1");
    await user.click(screen.getByRole("button", { name: "Add room" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("already used at this hospital");
  });
});
