import { sha256Hex } from "./attachment-file";

export async function verifiedReviewEvidenceBlob(
  response: Response,
  file: { content_type: string; size: number; sha256: string },
  signal: AbortSignal,
) {
  const message = "The downloaded evidence could not be verified.";
  const mime = response.headers.get("Content-Type")?.split(";", 1)[0].trim().toLowerCase();
  const length = response.headers.get("Content-Length");
  if (
    mime !== file.content_type ||
    !Number.isSafeInteger(file.size) ||
    file.size < 1 ||
    file.size > 10 * 1024 * 1024 ||
    (length !== null && (!/^\d+$/.test(length) || Number(length) !== file.size))
  ) {
    void response.body?.cancel().catch(() => {});
    throw new Error(message);
  }
  const reader = response.body?.getReader();
  if (!reader) throw new Error(message);
  const cancel = () => {
    void reader.cancel().catch(() => {});
  };
  signal.addEventListener("abort", cancel, { once: true });
  let completed = false;
  try {
    const data = new Uint8Array(file.size);
    let size = 0;
    while (true) {
      signal.throwIfAborted();
      const chunk = await reader.read();
      signal.throwIfAborted();
      if (chunk.done) break;
      if (chunk.value.byteLength > file.size - size) throw new Error(message);
      data.set(chunk.value, size);
      size += chunk.value.byteLength;
    }
    completed = true;
    if (size !== file.size) throw new Error(message);
    if ((await sha256Hex(data)) !== file.sha256) throw new Error(message);
    signal.throwIfAborted();
    return new Blob([data], { type: file.content_type });
  } finally {
    signal.removeEventListener("abort", cancel);
    if (!completed) cancel();
    reader.releaseLock();
  }
}
