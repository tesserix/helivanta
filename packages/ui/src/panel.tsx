"use client";

import {
  createContext,
  useContext,
  type AnchorHTMLAttributes,
  type HTMLAttributes,
  type Ref,
} from "react";
import Link from "next/link";
import { cn } from "@tesserix/web";

/**
 * The one card surface of the workspace (#1275, #1255 spec D3). A panel
 * inside a panel renders as a muted inset instead of a second bordered card,
 * so a component written to stand alone (RequestFailure, a list) cannot
 * produce a card inside a card wherever it is placed.
 */
const InPanel = createContext(false);

const CARD = "rounded-lg border bg-card shadow-xs";
const INSET = "rounded-md bg-muted";
const LINK_STATES =
  "block transition-colors duration-150 hover:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring motion-reduce:transition-none";

function surface(nested: boolean, flush: boolean) {
  const base = nested ? INSET : CARD;
  if (flush) return `${base} overflow-hidden`;
  return `${base} ${nested ? "p-4" : "p-5 sm:p-6"}`;
}

export function Panel({
  as: Tag = "section",
  flush = false,
  ref,
  className,
  children,
  ...rest
}: HTMLAttributes<HTMLElement> & {
  as?: "section" | "div" | "article" | "aside" | "li";
  ref?: Ref<HTMLElement>;
  /** No padding, content clipped: for a table or divided list edge to edge. */
  flush?: boolean;
}) {
  const nested = useContext(InPanel);
  return (
    // One ref type for every allowed tag; TS cannot narrow the union of element refs.
    <Tag {...rest} ref={ref as never} className={cn(surface(nested, flush), className)}>
      <InPanel.Provider value={true}>{children}</InPanel.Provider>
    </Tag>
  );
}

// Props are declared, not taken from ComponentProps<typeof Link>: this
// package and the shell can resolve different next versions, whose Link
// types do not unify.
export function PanelLink({
  href,
  className,
  children,
  ...rest
}: Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href"> & { href: string }) {
  const nested = useContext(InPanel);
  return (
    <Link {...rest} href={href} className={cn(surface(nested, false), LINK_STATES, className)}>
      {children}
    </Link>
  );
}
