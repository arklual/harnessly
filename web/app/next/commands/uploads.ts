// PUT /api/v2/uploads ahead of send (spec §3): XHR for the progress the composer shows. The runtime checks type and
// size against welcome.limits; its refusal comes back as a RefuseCode, never as text.

import { deviceId } from '@/next/core/device';
import { machineApi } from '@/next/core/net/machine';
import { limitsOf } from '@/next/stores/machines';
import {
  DEVICE_HEADER,
  parsePage,
  parseRefusal,
  type AttachmentRef,
  type MachineId,
  type RefuseCode,
} from '@/next/protocol/v2';

// An upload's id, or the file of one an agent was given (<uploads>/<id>.<ext>): what GET /api/v2/uploads/{id} serves.
const UPLOAD_ID = /(?:^|\/)(up_[A-Z2-7]+)(?:\.[a-z0-9]{1,8})?$/;

/** Where the picture of an attachment is on its machine; null for one the runtime did not take as an upload. */
export const uploadSrc = (m: MachineId, item: { id: string }): string | null => {
  // A picture pasted into the agent's own terminal lives in the transcript itself.
  if (item.id.startsWith('data:image/')) return item.id;
  const id = UPLOAD_ID.exec(item.id)?.[1];
  return id ? machineApi(m).http(`/uploads/${id}`) : null;
};

export type UploadOutcome =
  | { ok: true; ref: AttachmentRef }
  | { ok: false; code: RefuseCode | 'network' | 'timeout' | 'http' | 'aborted' };

export function uploadFile(
  m: MachineId,
  file: Blob & { name?: string },
  onProgress: (done: number, total: number) => void,
  signal?: AbortSignal,
): Promise<UploadOutcome> {
  return new Promise((resolve) => {
    const xhr = new XMLHttpRequest();
    // A pasted screenshot comes without a name; the runtime refuses a file without one.
    const name = file.name || `paste-${Date.now()}.${file.type.split('/')[1] || 'bin'}`;
    xhr.open('PUT', machineApi(m).http(`/uploads?name=${encodeURIComponent(name)}`));
    xhr.setRequestHeader(DEVICE_HEADER, deviceId());
    xhr.setRequestHeader('Content-Type', file.type || 'application/octet-stream');
    // A file takes as long as it takes; only silence ends it: no progress for cmdTimeoutMs.
    xhr.timeout = 0;
    let idle: ReturnType<typeof setTimeout> | undefined;
    const arm = () => {
      clearTimeout(idle);
      idle = setTimeout(() => {
        xhr.abort();
        resolve({ ok: false, code: 'timeout' });
      }, limitsOf(m).cmdTimeoutMs);
    };
    xhr.upload.onprogress = (e) => {
      arm();
      onProgress(e.loaded, e.lengthComputable ? e.total : file.size);
    };
    xhr.onload = () => {
      clearTimeout(idle);
      try {
        const doc: unknown = JSON.parse(xhr.responseText);
        if (typeof doc === 'object' && doc !== null && 'r' in doc)
          resolve({ ok: false, code: parseRefusal(doc).code });
        else resolve({ ok: true, ref: parsePage.upload(doc) });
      } catch {
        resolve({ ok: false, code: 'http' });
      }
    };
    xhr.onerror = () => {
      clearTimeout(idle);
      resolve({ ok: false, code: 'network' });
    };
    xhr.onabort = () => {
      clearTimeout(idle);
      resolve({ ok: false, code: 'aborted' });
    };
    signal?.addEventListener('abort', () => xhr.abort());
    arm();
    xhr.send(file);
  });
}
