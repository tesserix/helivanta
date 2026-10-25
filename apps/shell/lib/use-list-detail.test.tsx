import { act, fireEvent, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { DetailHeader, ListDetail, NavigationGuardProvider } from "@helivanta/ui";
import { useListDetail, useReportState, type FormState } from "./use-list-detail";

// A live address: push and replace change what useSearchParams returns.
const nav = vi.hoisted(() => ({
  search: "",
  listeners: new Set<() => void>(),
  calls: [] as { method: "push" | "replace"; href: string }[],
}));
vi.mock("next/navigation", async () => {
  const { useSyncExternalStore } = await import("react");
  const subscribe = (fn: () => void) => {
    nav.listeners.add(fn);
    return () => nav.listeners.delete(fn);
  };
  const go = (method: "push" | "replace") => (href: string) => {
    nav.calls.push({ method, href });
    nav.search = href.split("?")[1] ?? "";
    nav.listeners.forEach((fn) => fn());
  };
  return {
    usePathname: () => "/pharmacy/catalogue",
    useSearchParams: () => new URLSearchParams(useSyncExternalStore(subscribe, () => nav.search)),
    useRouter: () => ({ push: go("push"), replace: go("replace") }),
  };
});
afterEach(() => {
  nav.search = "";
  nav.calls.length = 0;
});

const id = (n: number) => `0199e000-0000-7000-8000-00000000010${n}`;
type Row = { id: string; name: string };
const three: Row[] = [1, 2, 3].map((n) => ({ id: id(n), name: `TEST-${n}` }));

function Draft({ report, onSave }: { report: FormState; onSave?: () => void }) {
  const [value, setValue] = useState("");
  const [saving, setSaving] = useState(false);
  useReportState(report, value !== "", saving);
  return (
    <>
      <label>
        Note
        <input value={value} onChange={(event) => setValue(event.target.value)} />
      </label>
      <button type="button" onClick={() => setSaving(true)}>
        Start saving
      </button>
      {onSave && (
        <button type="button" onClick={onSave}>
          Save new
        </button>
      )}
    </>
  );
}

function Page({ rows = three, onSaved }: { rows?: Row[]; onSaved?: (save: () => void) => void }) {
  const pane = useListDetail({
    rows,
    name: (row) => row.name,
    fallbackName: "this record",
    newName: "the new record",
    headingId: "list-heading",
  });
  const { detail, rowProps, openProps, paneRef } = pane;
  const body = detail.creating ? (
    <Draft
      report={pane.formState("new")}
      onSave={() => (onSaved ? onSaved(() => pane.saved(id(4))) : pane.saved(id(4)))}
    />
  ) : detail.openId ? (
    <>
      <DetailHeader
        title={pane.openName ?? "Record"}
        noun="record"
        previous={{ disabled: !pane.canStep(-1), onClick: () => pane.step(-1) }}
        next={{ disabled: !pane.canStep(1), onClick: () => pane.step(1) }}
        closeLabel="Close record"
        onClose={pane.close}
      />
      <Draft key={detail.openId} report={pane.formState("record")} />
    </>
  ) : null;
  return (
    <div onClickCapture={pane.holdLinks}>
      <h2 id="list-heading" tabIndex={-1}>
        Records
      </h2>
      <a href="/pharmacy/catalogue?tab=active">Active</a>
      <button type="button" onClick={pane.create}>
        Add record
      </button>
      <ListDetail
        label={pane.openName ?? "Record"}
        backHref={detail.closeHref}
        backLabel="Back to records"
        detail={body && <div ref={paneRef}>{body}</div>}
        list={
          <ul>
            {rows.map((row) => (
              <li key={row.id} {...rowProps(row.id)}>
                <button type="button" {...openProps(row.id)} onClick={() => pane.openRow(row.id)}>
                  Open {row.name}
                </button>
              </li>
            ))}
          </ul>
        }
      />
    </div>
  );
}

const renderPage = (props: Parameters<typeof Page>[0] = {}) =>
  renderWithProviders(
    <NavigationGuardProvider>
      <Page {...props} />
    </NavigationGuardProvider>,
  );
const frame = () => act(() => new Promise((resolve) => requestAnimationFrame(resolve)));

describe("useListDetail", () => {
  it("steps through the rows in order and stops at the ends", async () => {
    const { user } = renderPage();
    await user.click(screen.getByRole("button", { name: "Open TEST-1" }));
    expect(screen.getByRole("button", { name: "Previous record" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Next record" }));
    expect(screen.getByRole("heading", { name: "TEST-2" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Next record" }));
    expect(screen.getByRole("button", { name: "Next record" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Open TEST-3" }).closest("li")).toHaveAttribute(
      "aria-current",
      "true",
    );
    expect(nav.calls.map((call) => call.method)).toEqual(["push", "replace", "replace"]);
  });

  it("cannot step from a link or a reload: there are no rows to follow", () => {
    nav.search = `open=${id(2)}`;
    renderPage();
    expect(screen.getByRole("button", { name: "Previous record" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Next record" })).toBeDisabled();
  });

  it("keeps Next on the row below after the open record leaves the list", async () => {
    // A payment that settles a bill takes it out of Overdue (#1303 D4).
    function Shrinking() {
      const [rows, setRows] = useState(three);
      return (
        <>
          <button type="button" onClick={() => setRows(three.slice(1))}>
            Settle TEST-1
          </button>
          <Page rows={rows} />
        </>
      );
    }
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <Shrinking />
      </NavigationGuardProvider>,
    );
    await user.click(screen.getByRole("button", { name: "Open TEST-1" }));
    await user.click(screen.getByRole("button", { name: "Settle TEST-1" }));
    expect(screen.queryByRole("button", { name: "Open TEST-1" })).toBeNull();
    expect(screen.getByRole("heading", { name: "TEST-1" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Next record" }));
    expect(screen.getByRole("heading", { name: "TEST-2" })).toBeInTheDocument();
  });

  it("asks before Next discards an unsaved edit, and Keep editing keeps it", async () => {
    const { user } = renderPage();
    await user.click(screen.getByRole("button", { name: "Open TEST-1" }));
    await user.type(screen.getByLabelText("Note"), "TEST draft");
    await user.click(screen.getByRole("button", { name: "Next record" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Discard changes to TEST-1?");
    await user.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(screen.getByLabelText("Note")).toHaveValue("TEST draft");
    expect(new URLSearchParams(nav.search).get("open")).toBe(id(1));
  });

  it("holds stepping while a save is in flight", async () => {
    const { user } = renderPage();
    await user.click(screen.getByRole("button", { name: "Open TEST-1" }));
    await user.click(screen.getByRole("button", { name: "Start saving" }));
    expect(screen.getByRole("button", { name: "Next record" })).toBeDisabled();
  });

  it("closes on Escape and returns focus to the row it opened from", async () => {
    const { user } = renderPage();
    await user.click(screen.getByRole("button", { name: "Open TEST-2" }));
    fireEvent.keyDown(screen.getByLabelText("Note"), { key: "Escape" });
    await frame();
    expect(nav.search).toBe("");
    expect(screen.getByRole("button", { name: "Open TEST-2" })).toHaveFocus();
  });

  it("leaves Escape to an open menu inside the record", async () => {
    const { user } = renderPage();
    await user.click(screen.getByRole("button", { name: "Open TEST-2" }));
    const menu = document.createElement("div");
    menu.setAttribute("role", "listbox");
    menu.tabIndex = 0;
    screen.getByLabelText("Note").parentElement!.append(menu);
    fireEvent.keyDown(menu, { key: "Escape" });
    expect(new URLSearchParams(nav.search).get("open")).toBe(id(2));
  });

  it("returns focus to the list heading when the row has gone", async () => {
    nav.search = `open=${id(9)}`;
    const { user } = renderPage();
    await user.click(screen.getByRole("button", { name: "Close record" }));
    await frame();
    expect(screen.getByRole("heading", { name: "Records" })).toHaveFocus();
  });

  it("creating focuses the first field", async () => {
    const { user } = renderPage();
    await user.click(screen.getByRole("button", { name: "Add record" }));
    await frame();
    expect(screen.getByLabelText("Note")).toHaveFocus();
  });

  // #1303 E7: it went to whichever row was opened last.
  it("after a create and save, closing returns focus to the new record's row", async () => {
    const rows = [...three, { id: id(4), name: "TEST-4" }];
    const { user } = renderPage({ rows });
    await user.click(screen.getByRole("button", { name: "Open TEST-1" }));
    await user.click(screen.getByRole("button", { name: "Close record" }));
    await user.click(screen.getByRole("button", { name: "Add record" }));
    await user.click(screen.getByRole("button", { name: "Save new" }));
    expect(new URLSearchParams(nav.search).get("open")).toBe(id(4));
    await user.click(screen.getByRole("button", { name: "Close record" }));
    await frame();
    expect(screen.getByRole("button", { name: "Open TEST-4" })).toHaveFocus();
  });

  it("ignores a save that finishes after the page has gone", async () => {
    let finish = () => {};
    const view = renderPage({ onSaved: (save) => (finish = save) });
    await view.user.click(screen.getByRole("button", { name: "Add record" }));
    await view.user.click(screen.getByRole("button", { name: "Save new" }));
    const before = nav.calls.length;
    view.unmount();
    act(() => finish());
    expect(nav.calls.length).toBe(before);
  });

  it("holds an in-page link while unsaved, but not a Cmd-click", async () => {
    const { user } = renderPage();
    await user.click(screen.getByRole("button", { name: "Open TEST-1" }));
    await user.type(screen.getByLabelText("Note"), "TEST draft");
    const link = screen.getByRole("link", { name: "Active" });
    expect(fireEvent.click(link, { metaKey: true })).toBe(true);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(fireEvent.click(link)).toBe(false);
    expect(screen.getByRole("dialog")).toHaveTextContent("Discard changes to TEST-1?");
  });

  // #1303 E3: Refresh snapshot reads the rows again; stepping follows the new reading.
  it("retakes Previous / Next from the rows of a new reading", async () => {
    function Reading() {
      const [reading, setReading] = useState({ key: "t1", rows: three.slice(0, 2) });
      const pane = useListDetail({
        rows: reading.rows,
        name: (row) => row.name,
        fallbackName: "this record",
        newName: "the new record",
        headingId: "list-heading",
        snapshotKey: reading.key,
      });
      return (
        <>
          <button type="button" onClick={() => pane.openRow(id(2))}>
            Open TEST-2
          </button>
          <button type="button" onClick={() => setReading({ key: "t2", rows: three })}>
            Refresh
          </button>
          <output>{pane.canStep(1) ? "next on" : "next off"}</output>
        </>
      );
    }
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <Reading />
      </NavigationGuardProvider>,
    );
    await user.click(screen.getByRole("button", { name: "Open TEST-2" }));
    expect(screen.getByText("next off")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    expect(screen.getByText("next on")).toBeInTheDocument();
  });
});
