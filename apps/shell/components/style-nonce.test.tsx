import { render } from "@testing-library/react";
import { getNonce } from "get-nonce";
import { expect, it } from "vitest";

import { StyleNonce } from "./style-nonce";

function ReadsNonce({ seen }: { seen: (nonce: string | undefined) => void }) {
  seen(getNonce());
  return null;
}

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
