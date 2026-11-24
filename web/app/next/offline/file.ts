import { limit } from './storage';

// The response need not have Content-Length (chunked transfer). Stop reading once the cache budget is reached.
export async function boundedBlob(response: Response, signal?: AbortSignal) {
  const reader = response.body?.getReader();
  if (!reader) throw new Error('Empty file');
  const chunks: ArrayBuffer[] = [];
  let size = 0;
  try {
    if (!response.ok || response.redirected || Number(response.headers.get('content-length')) > limit)
      throw new Error('File unavailable or too large');
    for (;;) {
      signal?.throwIfAborted();
      const { done, value } = await reader.read();
      if (done) break;
      size += value.length;
      if (size > limit) throw new Error('Offline copy exceeds 32 MiB');
      chunks.push(new Uint8Array(value).buffer);
    }
    return new Blob(chunks, { type: response.headers.get('content-type')?.split(';')[0] });
  } finally {
    await reader.cancel().catch(() => undefined);
  }
}
