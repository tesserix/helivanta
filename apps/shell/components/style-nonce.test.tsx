import { render } from "@testing-library/react";
import { getNonce } from "get-nonce";
import { expect, it } from "vitest";

import { StyleNonce } from "./style-nonce";

function ReadsNonce({ seen }: { seen: (nonce: string | undefined) => void }) {
  seen(getNonce());
  return null;
}

// These tests share one document, as a page does: the first nonce latches.

// react-style-singleton (Radix's scroll lock) asks get-nonce for the nonce when
// a dialog opens; it must already be set by the time any child renders.
it("hands the nonce to get-nonce before children render", () => {
  let seen: string | undefined;
  render(
    <>
      <StyleNonce nonce="n1" />
      <ReadsNonce seen={(nonce) => (seen = nonce)} />
    </>,
  );
  expect(seen).toBe("n1");
});

// @tesserix/web's sign-in components render <style> elements on the client
// without a nonce prop; React creates them through document.createElement.
it("stamps the nonce on style elements created by script", () => {
  render(<StyleNonce nonce="n1" />);
  expect(document.createElement("style").nonce).toBe("n1");
  expect(document.createElement("div").nonce).toBe("");
});

it("leaves parsed markup alone", () => {
  render(<StyleNonce nonce="n1" />);
  const holder = document.createElement("div");
  holder.innerHTML = "<style>p{}</style>";
  expect(holder.querySelector("style")!.nonce).toBe("");
});

// The page's policy is fixed at load. A later render (a refetched layout carries
// the RSC request's own nonce) must not swap in a nonce that policy never named.
it("keeps the nonce the document loaded with", () => {
  let seen: string | undefined;
  render(
    <>
      <StyleNonce nonce="n2" />
      <ReadsNonce seen={(nonce) => (seen = nonce)} />
    </>,
  );
  expect(seen).toBe("n1");
  expect(document.createElement("style").nonce).toBe("n1");
});
