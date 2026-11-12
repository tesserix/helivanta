import assert from "node:assert/strict";
import net from "node:net";
import test from "node:test";
import { createEvidenceScanner } from "./lib/e2e-evidence-scanner.mjs";

async function session(t, content) {
  const server = createEvidenceScanner();
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(() => server.close());
  const socket = net.connect(server.address().port, "127.0.0.1");
  t.after(() => socket.destroy());
  socket.setTimeout(2000, () => socket.destroy(new Error("TEST protocol deadline")));
  const responses = [];
  const complete = new Promise((resolve, reject) => {
    let buffer = "";
    socket.on("error", reject);
    socket.on("data", (part) => {
      buffer += part.toString();
      while (buffer.includes("\0")) {
        const end = buffer.indexOf("\0");
        responses.push(buffer.slice(0, end));
        buffer = buffer.slice(end + 1);
        if (responses.length === 1) {
          const bytes = Buffer.from(content);
          const length = Buffer.alloc(4);
          length.writeUInt32BE(bytes.length);
          socket.write("zINSTREAM\0");
          socket.write(length.subarray(0, 2));
          socket.write(Buffer.concat([length.subarray(2), bytes, Buffer.alloc(4)]));
        } else if (responses.length === 2) socket.write("zVERSION\0");
        else {
          socket.end("zEND\0");
          resolve(responses);
        }
      }
    });
  });
  socket.write("zIDSESSION\0zVERSION\0");
  return complete;
}

test("synthetic scanner binds clean stream and stable definitions to one numbered session", async (t) => {
  const messages = await session(t, "TEST PNG");
  assert.equal(messages[1], "2: stream: OK");
  assert.equal(messages[0].slice(3), messages[2].slice(3));
  assert.match(messages[0], /^1: ClamAV /);
  assert.match(messages[2], /^3: ClamAV /);
});
test("synthetic scanner refuses its explicit TEST infection marker", async (t) => {
  const messages = await session(t, "TEST EICAR marker");
  assert.equal(messages[1], "2: stream: TEST-signature FOUND");
});
