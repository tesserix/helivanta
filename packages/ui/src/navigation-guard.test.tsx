import { useLayoutEffect, useState, type AnchorHTMLAttributes } from "react";
import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
const router = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));

vi.mock("next/link", () => ({
  default: function MockLink({
    onNavigate,
    onClick,
    href,
    children,
    ...props
  }: AnchorHTMLAttributes<HTMLAnchorElement> & {
    onNavigate?: (event: { preventDefault: () => void }) => void;
  }) {
    return (
      <a
        {...props}
        href={href}
        onClick={(event) => {
          onClick?.(event);
          if (
            !event.defaultPrevented &&
            !event.metaKey &&
            !event.ctrlKey &&
            !event.shiftKey &&
            !event.altKey &&
            event.button === 0
          ) {
            onNavigate?.({ preventDefault: () => event.preventDefault() });
          }
        }}
      >
        {children}
      </a>
    );
  },
}));
import {
  NavigationGuardProvider,
  GuardedLink,
  useNavigationPending,
  useConfirmLeave,
  useGuardedLinkClick,
  useLeaveGuard,
} from "./navigation-guard";

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

  // #1303 E7: Cmd/Ctrl/Shift and middle clicks open another tab or window;
  // nothing in this one is lost, so they are not held.
  it.each([
    ["Cmd", { metaKey: true }],
    ["Ctrl", { ctrlKey: true }],
    ["Shift", { shiftKey: true }],
    ["middle", { button: 1 }],
  ])("lets a %s click on a shell link through while unsaved", (_, modifier) => {
    const { navigate } = renderGuard(true);
    const link = screen.getByRole("link", { name: "Overview" });
    const passed = fireEvent.click(link, modifier);
    expect(passed).toBe(true);
    expect(navigate).toHaveBeenCalledWith("/dashboard");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("useConfirmLeave asks while another component is unsaved", async () => {
    const go = vi.fn();
    function Picker() {
      const confirm = useConfirmLeave();
      return (
        <button type="button" onClick={() => confirm(go)}>
          Choose outlet
        </button>
      );
    }
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <Editor dirty go={vi.fn()} />
        <Picker />
      </NavigationGuardProvider>,
    );
    await user.click(screen.getByRole("button", { name: "Choose outlet" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Discard changes to TEST bill?");
    expect(go).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Discard" }));
    expect(go).toHaveBeenCalledOnce();
  });

  // Back cannot be cancelled: stop the router's own listener, step forward
  // to the record's entry again, then ask.
  it("guards Back in the commit that first makes an editor dirty", () => {
    const go = vi.spyOn(window.history, "go").mockImplementation(() => {});
    const router = vi.fn();
    function EditAndBack() {
      const [dirty, setDirty] = useState(false);
      useLeaveGuard(dirty, "TEST bill");
      useLayoutEffect(() => {
        if (dirty) window.dispatchEvent(new PopStateEvent("popstate"));
      }, [dirty]);
      return <button onClick={() => setDirty(true)}>Edit</button>;
    }
    renderWithProviders(
      <NavigationGuardProvider>
        <EditAndBack />
      </NavigationGuardProvider>,
    );
    window.addEventListener("popstate", router);
    try {
      fireEvent.click(screen.getByRole("button", { name: "Edit" }));
      expect(router).not.toHaveBeenCalled();
      expect(go).toHaveBeenCalledWith(1);
      expect(screen.getByRole("dialog")).toHaveTextContent("Discard changes to TEST bill?");
    } finally {
      window.removeEventListener("popstate", router);
      go.mockRestore();
    }
  });

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

it("tracks an approved guarded link until its transition settles", async () => {
  let finish!: () => void;
  const pending = new Promise<void>((resolve) => {
    finish = resolve;
  });
  router.push.mockReset();
  router.push.mockReturnValueOnce(pending);
  const clicked = vi.fn();
  function Status() {
    const navigation = useNavigationPending();
    return (
      <output aria-label="Navigation status">{navigation.pending ? "pending" : "idle"}</output>
    );
  }
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GuardedLink href="/lab" onClick={clicked}>
        Open lab
      </GuardedLink>
      <Status />
    </NavigationGuardProvider>,
  );
  await user.click(screen.getByRole("link", { name: "Open lab" }));
  expect(clicked).toHaveBeenCalledOnce();
  expect(router.push).toHaveBeenCalledExactlyOnceWith("/lab");
  expect(screen.getByLabelText("Navigation status")).toHaveTextContent("pending");
  await act(async () => finish());
  expect(screen.getByLabelText("Navigation status")).toHaveTextContent("idle");
});
