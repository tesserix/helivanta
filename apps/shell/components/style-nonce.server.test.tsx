// @vitest-environment node
import { renderToString } from "react-dom/server";
import { getNonce } from "get-nonce";
import { expect, it } from "vitest";

import { StyleNonce } from "./style-nonce";

// On the server this module is shared by every request; a latched nonce would
// be one request's state leaking into the next.
it("keeps no nonce between server renders", () => {
  renderToString(<StyleNonce nonce="request-1" />);
  renderToString(<StyleNonce nonce="request-2" />);
  expect(getNonce()).toBeUndefined();
});
