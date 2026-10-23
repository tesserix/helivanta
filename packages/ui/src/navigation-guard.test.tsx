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

  // Back cannot be cancelled: put the entry back, then ask.
  it("restores the page on Back while unsaved, then asks", () => {
    const push = vi.spyOn(window.history, "pushState");
    renderGuard(true);
    act(() => {
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(push).toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toHaveTextContent("Discard changes to TEST bill?");
    push.mockRestore();
  });

  it("leaves Back alone when nothing is unsaved", () => {
    const push = vi.spyOn(window.history, "pushState");
    renderGuard(false);
    act(() => {
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(push).not.toHaveBeenCalled();
    push.mockRestore();
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
