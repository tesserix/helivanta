"use client";

import { setNonce } from "get-nonce";

// Gives react-style-singleton (Radix's scroll lock) the request nonce for the
// <style> it injects when a dialog opens (#1113, spec D5). Set during render so
// it is in place before any child can open one.
export function StyleNonce({ nonce }: { nonce: string }) {
  setNonce(nonce);
  return null;
}
