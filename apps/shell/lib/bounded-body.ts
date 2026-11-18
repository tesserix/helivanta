type BodyResult = { ok: true; text: string } | { ok: false; status: 400 | 408 | 413 };

export async function readBoundedBody(request: Request, maxBytes: number): Promise<BodyResult> {
  const length = request.headers.get("content-length");
  if (length !== null && (!/^\d+$/.test(length) || !Number.isSafeInteger(Number(length)))) {
    if (request.body) void request.body.cancel().catch(() => {});
    return { ok: false, status: 400 };
  }
  if (length !== null && Number(length) > maxBytes) {
    if (request.body) void request.body.cancel().catch(() => {});
    return { ok: false, status: 413 };
  }
  if (!request.body) return { ok: true, text: "" };
  let reader: ReadableStreamDefaultReader<Uint8Array>;
  try {
    reader = request.body.getReader();
  } catch {
    return { ok: false, status: 400 };
  }
  const timeout = Symbol("body timeout");
  let timer: ReturnType<typeof setTimeout> | undefined;
  const deadline = new Promise<never>((_resolve, reject) => {
    timer = setTimeout(() => {
      reject(timeout);
      // Cancellation must not wait on a stalled producer.
      void reader.cancel().catch(() => {});
    }, 2000);
  });
  const consume = async (): Promise<BodyResult> => {
    const chunks: Uint8Array[] = [];
    let size = 0;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > maxBytes) {
        void reader.cancel().catch(() => {});
        return { ok: false, status: 413 };
      }
      chunks.push(value);
    }
    const bytes = new Uint8Array(size);
    let offset = 0;
    for (const chunk of chunks) {
      bytes.set(chunk, offset);
      offset += chunk.byteLength;
    }
    return { ok: true, text: new TextDecoder("utf-8", { fatal: true }).decode(bytes) };
  };
  try {
    return await Promise.race([consume(), deadline]);
  } catch (error) {
    return { ok: false, status: error === timeout ? 408 : 400 };
  } finally {
    if (timer !== undefined) clearTimeout(timer);
    reader.releaseLock();
  }
}
