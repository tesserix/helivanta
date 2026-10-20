"use client";

import { setNonce } from "get-nonce";

// The nonce the document's policy names (#1113, spec D5). Latched on first
// render: a later render may carry another request's nonce, which this
// page's policy never allowed.
let documentNonce: string | undefined;

// Gives client-created <style> elements the page's nonce. react-style-singleton
// (Radix's scroll lock) reads it from get-nonce. @tesserix/web's sign-in
// components render <style> with no nonce prop, some of it built from tenant
// branding so no hash can name it, and React creates those through
// document.createElement. Stamping that call is 'strict-dynamic' for styles:
// code already running may add styles, while injected markup (the parser does
// not call createElement) stays blocked. Runs during render, before any child
// can open a dialog or render a style.
export function StyleNonce({ nonce }: { nonce: string }) {
  if (documentNonce === undefined) {
    documentNonce = nonce;
    setNonce(nonce);
    if (typeof document !== "undefined") stampStyleElements(document, nonce);
  }
  return null;
}

function stampStyleElements(doc: Document, nonce: string) {
  const create = doc.createElement.bind(doc);
  doc.createElement = ((tagName: string, options?: ElementCreationOptions) => {
    const element = create(tagName, options);
    if (element instanceof HTMLStyleElement) element.nonce = nonce;
    return element;
  }) as Document["createElement"];
}
