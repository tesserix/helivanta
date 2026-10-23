import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
const router = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));

import { NavigationGuardProvider, useGuardedLinkClick, useLeaveGuard } from "./navigation-guard";

function Editor({ dirty, go }: { dirty: boolean; go: () => void }) {
  const guard = useLeaveGuard(dirty, "TEST bill");
  return (
    <button type="button" onClick={() => guard.confirm(go)}>
      Switch
    </button>
  );
}

function ShellLink({ onNavigate }: { onNavigate: (href: string) => void }) {
  const click = useGuardedLinkClick();
  return (
    <a
      href="/dashboard"
      onClick={(event) => {
        click(event, "/dashboard");
        if (!event.defaultPrevented) onNavigate("/dashboard");
      }}
    >
      Overview
    </a>
  );
}

const renderGuard = (dirty: boolean, go = vi.fn(), navigate = vi.fn()) => {
  const view = renderWithProviders(
    <NavigationGuardProvider>
      <Editor dirty={dirty} go={go} />
      <ShellLink onNavigate={navigate} />
    </NavigationGuardProvider>,
  );
  return { ...view, go, navigate };
};

describe("useLeaveGuard", () => {
  it("goes at once when nothing is unsaved", async () => {
    const { user, go } = renderGuard(false);
    await user.click(screen.getByRole("button", { name: "Switch" }));
    expect(go).toHaveBeenCalledOnce();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("asks before discarding, and Keep editing stays", async () => {
    const { user, go } = renderGuard(true);
    await user.click(screen.getByRole("button", { name: "Switch" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Discard changes to TEST bill?");
    await user.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(go).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Switch" }));
    await user.click(screen.getByRole("button", { name: "Discard" }));
    expect(go).toHaveBeenCalledOnce();
  });

  // Rail and menu links are client-side moves: no beforeunload fires.
  it("stops a shell link while unsaved and follows it on Discard", async () => {
    const { user, navigate } = renderGuard(true);
    await user.click(screen.getByRole("link", { name: "Overview" }));
    expect(navigate).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Discard" }));
    expect(router.push).toHaveBeenCalledWith("/dashboard");
  });

  it("lets a shell link through when nothing is unsaved", async () => {
    const { user, navigate } = renderGuard(false);
    await user.click(screen.getByRole("link", { name: "Overview" }));
    expect(navigate).toHaveBeenCalledWith("/dashboard");
  });

  // Back cannot be cancelled: stop the router's own listener, step forward
  // to the record's entry again, then ask.
  it("holds Back while unsaved: the router never sees it, then it asks", () => {
    const go = vi.spyOn(window.history, "go").mockImplementation(() => {});
    renderGuard(true);
    const router = vi.fn();
    window.addEventListener("popstate", router);
    act(() => {
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(router).not.toHaveBeenCalled();
    expect(go).toHaveBeenCalledWith(1);
    expect(screen.getByRole("dialog")).toHaveTextContent("Discard changes to TEST bill?");
    // The forward step that restores the entry is the guard's own: swallowed.
    act(() => {
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(router).not.toHaveBeenCalled();
    window.removeEventListener("popstate", router);
    go.mockRestore();
  });

  it("lets Back through after Discard", async () => {
    const go = vi.spyOn(window.history, "go").mockImplementation(() => {});
    const back = vi.spyOn(window.history, "back").mockImplementation(() => {});
    const { user } = renderGuard(true);
    const router = vi.fn();
    window.addEventListener("popstate", router);
    act(() => {
      window.dispatchEvent(new PopStateEvent("popstate"));
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await user.click(screen.getByRole("button", { name: "Discard" }));
    expect(back).toHaveBeenCalledOnce();
    act(() => {
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(router).toHaveBeenCalledOnce();
    window.removeEventListener("popstate", router);
    go.mockRestore();
    back.mockRestore();
  });

  it("leaves Back alone when nothing is unsaved", () => {
    const go = vi.spyOn(window.history, "go");
    renderGuard(false);
    const router = vi.fn();
    window.addEventListener("popstate", router);
    act(() => {
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(go).not.toHaveBeenCalled();
    expect(router).toHaveBeenCalledOnce();
    window.removeEventListener("popstate", router);
    go.mockRestore();
  });

  // Review Important 2: a save in flight carries on and commits, so it is
  // never offered as discardable; the move is simply held.
  it("holds a move during a save without offering Discard", async () => {
    const go = vi.fn();
    function Saving() {
      const guard = useLeaveGuard(false, "TEST bill", true);
      return (
        <button type="button" onClick={() => guard.confirm(go)}>
          Switch
        </button>
      );
    }
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <Saving />
      </NavigationGuardProvider>,
    );
    await user.click(screen.getByRole("button", { name: "Switch" }));
    expect(go).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("asks the browser before a reload only while unsaved", () => {
    const { rerender } = renderGuard(false);
    const clean = new Event("beforeunload", { cancelable: true });
    fireEvent(window, clean);
    expect(clean.defaultPrevented).toBe(false);
    rerender(
      <NavigationGuardProvider>
        <Editor dirty go={vi.fn()} />
      </NavigationGuardProvider>,
    );
    const dirty = new Event("beforeunload", { cancelable: true });
    fireEvent(window, dirty);
    expect(dirty.defaultPrevented).toBe(true);
  });
});
