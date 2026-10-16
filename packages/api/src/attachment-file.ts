export async function readAttachmentFile(file: File, signal: AbortSignal): Promise<ArrayBuffer> {
  if (typeof file.arrayBuffer === "function") return file.arrayBuffer();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    const abort = () => reader.abort();
    const finish = () => signal.removeEventListener("abort", abort);
    reader.onload = () => {
      finish();
      if (reader.result instanceof ArrayBuffer) resolve(reader.result);
      else reject(new Error("The selected file could not be read safely."));
    };
    reader.onerror = () => {
      finish();
      reject(new Error("The selected file could not be read safely."));
    };
    reader.onabort = () => {
      finish();
      reject(new DOMException("File read cancelled", "AbortError"));
    };
    signal.addEventListener("abort", abort, { once: true });
    reader.readAsArrayBuffer(file);
  });
}
