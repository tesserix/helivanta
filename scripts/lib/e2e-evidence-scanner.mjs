// TEST ONLY: bounded ClamAV wire fixture. It does not load malware signatures.
import net from "node:net";

function version() {
  const now = new Date();
  const days = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  const months = [
    "Jan",
    "Feb",
    "Mar",
    "Apr",
    "May",
    "Jun",
    "Jul",
    "Aug",
    "Sep",
    "Oct",
    "Nov",
    "Dec",
  ];
  return `ClamAV 1.5.4/1/${days[now.getUTCDay()]} ${months[now.getUTCMonth()]} ${String(now.getUTCDate()).padStart(2, " ")} ${now.toISOString().slice(11, 19)} ${now.getUTCFullYear()}`;
}

export function createEvidenceScanner() {
  return net.createServer((socket) => {
    socket.setTimeout(15_000, () => socket.destroy());
    socket.on("error", () => {});
    let buffer = Buffer.alloc(0),
      session = false,
      streaming = false,
      sequence = 0,
      size = 0,
      frames = 0;
    const content = [];
    // One stable synthetic definition generation belongs to the whole session.
    const definitions = version();
    const reply = (message) => {
      const value = `${session ? `${++sequence}: ` : ""}${message}\0`;
      if (session) socket.write(value);
      else socket.end(value);
    };
    socket.on("data", (part) => {
      buffer = Buffer.concat([buffer, part]);
      for (;;) {
        if (!streaming) {
          const end = buffer.indexOf(0);
          if (end < 0) {
            if (buffer.length > 32) socket.destroy();
            return;
          }
          const name = buffer.subarray(0, end).toString();
          buffer = buffer.subarray(end + 1);
          if (name === "zIDSESSION" && !session && sequence === 0) session = true;
          else if (name === "zVERSION") reply(definitions);
          else if (name === "zINSTREAM") streaming = true;
          else if (name === "zEND" && session) {
            socket.end();
            return;
          } else {
            socket.destroy();
            return;
          }
          continue;
        }
        if (buffer.length < 4) return;
        const length = buffer.readUInt32BE();
        if (length > 10 * 1024 * 1024 || size + length > 10 * 1024 * 1024 || frames > 1024) {
          socket.destroy();
          return;
        }
        if (buffer.length < length + 4) return;
        buffer = buffer.subarray(4);
        if (!length) {
          reply(
            Buffer.concat(content).includes(Buffer.from("EICAR"))
              ? "stream: TEST-signature FOUND"
              : "stream: OK",
          );
          streaming = false;
          continue;
        }
        content.push(buffer.subarray(0, length));
        size += length;
        frames++;
        buffer = buffer.subarray(length);
      }
    });
  });
}
