// A file to the machine ahead of send (spec §3), any size: the runtime opens a session (POST /api/v2/uploads), takes
// the file in chunks (PATCH with Upload-Offset), and closes it (POST …/complete). Nothing is held in memory: a chunk is
// a slice of the file, the runtime writes it to a temporary file and renames it into place at the end. After a break
// the page asks the machine where it is (HEAD) and goes on from there; a try that gives up keeps its session in
// `IUploadSession`, so «Повторить» goes on from the bytes the machine has. XHR, because only it reports how much of a
// chunk has left.
import { deviceId } from '@/next/core/device';
import { gatewayPath, machineApi } from '@/next/core/net/machine';
import { appConfig } from '@/platform/config';
import {
  DEVICE_HEADER,
  parsePage,
  type AttachmentRef,
  type MachineId,
  type RefuseCode,
} from '@/next/protocol/v2';

// An upload's id, or the file of one an agent was given (<uploads>/<id>.<ext>): what GET /api/v2/uploads/{id} serves.
const UPLOAD_ID = /(?:^|\/)(up_[A-Z2-7]+)(?:\.[a-z0-9]{1,8})?$/;

/** Where the picture of an attachment is on its machine; null for one the runtime did not take as an upload. */
export const uploadSrc = (m: MachineId, item: { id: string; mime?: string }): string | null => {
  // A picture pasted into the agent's own terminal lives in the transcript itself.
  if (item.id.startsWith('data:image/')) return item.id;
  const kept = keptSha(item.id);
  if (kept) {
    const type = item.mime ? `&type=${encodeURIComponent(item.mime)}` : '';
    return `${gatewayPath(`/uploads/blobs/${kept}`)}?machine=${encodeURIComponent(m)}${type}`;
  }
  const id = UPLOAD_ID.exec(item.id)?.[1];
  if (!id) return null;
  // HRN-879: under a hub the upload is read from the hub's store (it sends the machine's copy on for older ones).
  if (appConfig().centralUploads)
    return `${gatewayPath(`/uploads/files/${id}`)}?machine=${encodeURIComponent(m)}`;
  return machineApi(m).http(`/uploads/${id}`);
};

/** The largest file the page hashes and puts into the hub's store: a hash needs the whole file in memory. */
export const MIRROR_MAX_BYTES = 64 * 1024 * 1024;

const hex = (buf: ArrayBuffer) =>
  [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, '0')).join('');

/**
 * HRN-879. Puts what the machine took into the hub's store as well: the bytes under their sha256, then which upload of
 * the machine they are. The chat then reads it from the hub, with the machine off too. Best effort: a failure leaves the
 * upload on the machine only, as before, and is not the person's to see.
 */
export async function mirrorUpload(m: MachineId, file: Blob, ref: AttachmentRef): Promise<boolean> {
  const id = UPLOAD_ID.exec(ref.id)?.[1];
  if (
    !appConfig().centralUploads ||
    !id ||
    file.size > MIRROR_MAX_BYTES ||
    typeof crypto === 'undefined' ||
    !crypto.subtle
  )
    return false;
  try {
    const sha = hex(await crypto.subtle.digest('SHA-256', await file.arrayBuffer()));
    const q = `?machine=${encodeURIComponent(m)}`;
    const put = await fetch(`${gatewayPath(`/uploads/blobs/${sha}`)}${q}`, {
      method: 'PUT',
      body: file,
      credentials: 'same-origin',
    });
    if (!put.ok) return false;
    const named = await fetch(`${gatewayPath(`/uploads/refs/${id}`)}${q}`, {
      method: 'PUT',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sha256: sha, mime: ref.mime, name: ref.name, size: file.size }),
    });
    return named.ok;
  } catch {
    return false;
  }
}

/**
 * HRN-879. An attachment put into the hub while its machine was off: `hub:<sha256>`. The message waits in the outbox,
 * and when the machine is back it takes its copy from the hub (takeKept) before the message goes.
 */
const KEPT = /^hub:([0-9a-f]{64})$/;
export const keptSha = (id: string): string | null => KEPT.exec(id)?.[1] ?? null;

const IMAGE_MIMES: ReadonlySet<string> = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp']);

/** Puts a file into the hub's store for a machine that is off; the attachment names it by its hash. */
export async function keepInHub(
  m: MachineId,
  file: Blob & { name?: string },
  onProgress: (done: number, total: number) => void,
): Promise<UploadOutcome> {
  if (file.size > MIRROR_MAX_BYTES) return { ok: false, code: UPLOAD_FAILURE.tooLarge };
  if (typeof crypto === 'undefined' || !crypto.subtle) return { ok: false, code: UPLOAD_FAILURE.network };
  const name = file.name || `paste-${Date.now()}.${file.type.split('/')[1] || 'bin'}`;
  const mime = file.type || 'application/octet-stream';
  try {
    const sha = hex(await crypto.subtle.digest('SHA-256', await file.arrayBuffer()));
    const put = await fetch(`${gatewayPath(`/uploads/blobs/${sha}`)}?machine=${encodeURIComponent(m)}`, {
      method: 'PUT',
      body: file,
      credentials: 'same-origin',
    });
    if (!put.ok) return { ok: false, code: UPLOAD_FAILURE.http };
    onProgress(file.size, file.size);
    return {
      ok: true,
      ref: { id: `hub:${sha}`, name, kind: IMAGE_MIMES.has(mime) ? 'image' : 'file', mime },
    };
  } catch {
    return { ok: false, code: UPLOAD_FAILURE.network };
  }
}

/**
 * The attachments as the machine knows them: one kept in the hub is read from there (the hub gives it only to the
 * person who put it) and uploaded to the machine as any other file, which names it in the hub too. null when that
 * cannot be done now: the message waits and goes again.
 */
/** Whether any attachment is kept in the hub: only then does a message wait for takeKept before it goes. */
export function anyKept(attachments: readonly AttachmentRef[]): boolean {
  return attachments.some((a) => keptSha(a.id));
}

export async function takeKept(
  m: MachineId,
  attachments: readonly AttachmentRef[],
): Promise<AttachmentRef[] | null> {
  const out: AttachmentRef[] = [];
  for (const a of attachments) {
    const sha = keptSha(a.id);
    if (!sha) {
      out.push(a);
      continue;
    }
    try {
      const res = await fetch(`${gatewayPath(`/uploads/blobs/${sha}`)}?machine=${encodeURIComponent(m)}`, {
        credentials: 'same-origin',
      });
      if (!res.ok) return null;
      const file = new File([await res.blob()], a.name, { type: a.mime });
      const sent = await uploadFile(m, file, () => undefined);
      if (!sent.ok) return null;
      out.push(sent.ref);
    } catch {
      return null;
    }
  }
  return out;
}

export const UPLOAD_FAILURE = {
  network: 'network',
  http: 'http',
  aborted: 'aborted',
  noSpace: 'no-space',
  tooLarge: 'too-large',
  /** The runtime said why in its own words (`message`). */
  refused: 'refused',
} as const;
type TUploadFailure = (typeof UPLOAD_FAILURE)[keyof typeof UPLOAD_FAILURE];

export type UploadOutcome =
  | { ok: true; ref: AttachmentRef }
  | {
      ok: false;
      code: RefuseCode | TUploadFailure | 'timeout';
      /** The runtime's own words, in Russian, for `refused`. */
      message?: string;
      /** For `no-space`: what the file needs and what the disk has. */
      need?: number;
      free?: number;
    };

/** The session a try keeps for the next one: where the machine holds the bytes already sent. */
export interface IUploadSession {
  id: string | null;
  chunk: number;
}

export const newUploadSession = (): IUploadSession => ({ id: null, chunk: 0 });

/** A link silent for this long is a broken one; a chunk goes again from the offset the machine has. */
const IDLE_MS = 60_000;
const RETRY_FIRST_MS = 1000;
const RETRY_MAX_MS = 15_000;
/** Breaks in a row before the try gives up and offers «Повторить» (about a minute and a half of trying). */
const RETRY_LIMIT = 8;
const DEFAULT_RETRY_AFTER_S = 2;

interface IAnswer {
  status: number;
  body: Record<string, unknown>;
  header: (name: string) => string | null;
}

const record = (v: unknown): Record<string, unknown> =>
  typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {};

const num = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null);

/** One request; null when there was no answer (a break, or silence for IDLE_MS) and `aborted` when the signal ended it. */
const exchange = (
  m: MachineId,
  method: 'POST' | 'PATCH' | 'HEAD',
  path: `/${string}`,
  init: {
    headers?: Record<string, string>;
    json?: unknown;
    body?: Blob;
    signal: AbortSignal;
    onSent?: (n: number) => void;
  },
): Promise<IAnswer | null | 'aborted'> =>
  new Promise((resolve) => {
    if (init.signal.aborted) {
      resolve('aborted');
      return;
    }
    const xhr = new XMLHttpRequest();
    xhr.open(method, machineApi(m).http(path));
    xhr.setRequestHeader(DEVICE_HEADER, deviceId());
    if (init.json !== undefined) xhr.setRequestHeader('Content-Type', 'application/json');
    for (const [name, value] of Object.entries(init.headers ?? {})) xhr.setRequestHeader(name, value);
    xhr.timeout = 0;
    let idle: ReturnType<typeof setTimeout> | undefined;
    let silent = false;
    const arm = () => {
      clearTimeout(idle);
      idle = setTimeout(() => {
        silent = true;
        xhr.abort();
      }, IDLE_MS);
    };
    const done = (value: IAnswer | null | 'aborted') => {
      clearTimeout(idle);
      init.signal.removeEventListener('abort', onAbort);
      resolve(value);
    };
    const onAbort = () => xhr.abort();
    init.signal.addEventListener('abort', onAbort, { once: true });
    xhr.upload.onprogress = (e) => {
      arm();
      init.onSent?.(e.loaded);
    };
    xhr.onload = () => {
      let body: Record<string, unknown> = {};
      try {
        body = record(JSON.parse(xhr.responseText));
      } catch {
        // an answer without a body (HEAD, 204)
      }
      done({ status: xhr.status, body, header: (name) => xhr.getResponseHeader(name) });
    };
    xhr.onerror = () => done(null);
    xhr.onabort = () => done(silent || !init.signal.aborted ? null : 'aborted');
    arm();
    xhr.send(init.json === undefined ? (init.body ?? null) : JSON.stringify(init.json));
  });

const pause = (ms: number, signal: AbortSignal) =>
  new Promise<void>((resolve) => {
    const timer = setTimeout(done, ms);
    function done() {
      clearTimeout(timer);
      signal.removeEventListener('abort', done);
      resolve();
    }
    signal.addEventListener('abort', done, { once: true });
  });

const retryDelay = (attempt: number) => Math.min(RETRY_MAX_MS, RETRY_FIRST_MS * 2 ** (attempt - 1));

const failureOf = (a: IAnswer): UploadOutcome => {
  const message = typeof a.body.message === 'string' && a.body.message ? a.body.message : undefined;
  if (a.status === 507 || a.body.error === 'no-space')
    return {
      ok: false,
      code: UPLOAD_FAILURE.noSpace,
      message,
      need: num(a.body.need) ?? undefined,
      free: num(a.body.free) ?? undefined,
    };
  if (a.status === 413) return { ok: false, code: UPLOAD_FAILURE.tooLarge, message };
  if (message) return { ok: false, code: UPLOAD_FAILURE.refused, message };
  return { ok: false, code: UPLOAD_FAILURE.http };
};

const isServerFault = (status: number) => status >= 500 && status !== 507;

export async function uploadFile(
  m: MachineId,
  file: Blob & { name?: string },
  onProgress: (done: number, total: number) => void,
  signal?: AbortSignal,
  session: IUploadSession = newUploadSession(),
): Promise<UploadOutcome> {
  const own = new AbortController();
  if (signal?.aborted) own.abort();
  signal?.addEventListener('abort', () => own.abort(), { once: true });
  const stop = own.signal;
  // A pasted screenshot comes without a name; the runtime refuses a file without one.
  const name = file.name || `paste-${Date.now()}.${file.type.split('/')[1] || 'bin'}`;
  const total = file.size;
  let offset = 0;
  let attempt = 0;
  let resync = session.id !== null;

  // A break: wait, then ask the machine where it is. After RETRY_LIMIT in a row the try ends; the session stays.
  const brokeOff = async (): Promise<UploadOutcome | null> => {
    attempt += 1;
    if (attempt > RETRY_LIMIT) return { ok: false, code: UPLOAD_FAILURE.network };
    await pause(retryDelay(attempt), stop);
    resync = true;
    return null;
  };
  const aborted = async (): Promise<UploadOutcome> => {
    // Cancelled: the temporary file on the machine goes too.
    if (session.id) {
      const id = session.id;
      session.id = null;
      const cleanup = new XMLHttpRequest();
      cleanup.open('DELETE', machineApi(m).http(`/uploads/${id}`));
      cleanup.setRequestHeader(DEVICE_HEADER, deviceId());
      cleanup.send();
    }
    return { ok: false, code: UPLOAD_FAILURE.aborted };
  };

  for (;;) {
    if (stop.aborted) return aborted();
    if (session.id === null) {
      const made = await exchange(m, 'POST', '/uploads', {
        json: { name, size: total, type: file.type || 'application/octet-stream' },
        signal: stop,
      });
      if (made === 'aborted') return aborted();
      if (made === null || isServerFault(made.status)) {
        const gave = await brokeOff();
        if (gave) return gave;
        continue;
      }
      const id = made.body.id;
      const chunk = num(made.body.chunkSize);
      if (made.status !== 201 || typeof id !== 'string' || !chunk) return failureOf(made);
      session.id = id;
      session.chunk = chunk;
      offset = 0;
      attempt = 0;
      resync = false;
    }
    const base: `/${string}` = `/uploads/${session.id}`;
    if (resync) {
      const head = await exchange(m, 'HEAD', base, { signal: stop });
      if (head === 'aborted') return aborted();
      if (head === null || isServerFault(head.status)) {
        const gave = await brokeOff();
        if (gave) return gave;
        continue;
      }
      if (head.status === 404) {
        // The machine lost the session (a day passed): the file goes again from the start.
        session.id = null;
        continue;
      }
      const known = Number(head.header('Upload-Offset'));
      if (head.status !== 200 || !Number.isFinite(known)) return { ok: false, code: UPLOAD_FAILURE.http };
      offset = known;
      resync = false;
    }
    onProgress(offset, total);
    if (offset >= total) {
      const closed = await exchange(m, 'POST', `${base}/complete`, { signal: stop });
      if (closed === 'aborted') return aborted();
      if (closed === null || isServerFault(closed.status)) {
        const gave = await brokeOff();
        if (gave) return gave;
        continue;
      }
      if (closed.status === 409 && num(closed.body.offset) !== null) {
        offset = num(closed.body.offset) as number;
        continue;
      }
      if (closed.status === 404) {
        session.id = null;
        continue;
      }
      if (closed.status !== 200) return failureOf(closed);
      try {
        const ref = parsePage.upload(closed.body);
        session.id = null;
        void mirrorUpload(m, file, ref);
        return { ok: true, ref };
      } catch {
        return { ok: false, code: UPLOAD_FAILURE.http };
      }
    }
    const end = Math.min(total, offset + session.chunk);
    const sent = await exchange(m, 'PATCH', base, {
      headers: { 'Upload-Offset': String(offset) },
      body: file.slice(offset, end),
      signal: stop,
      onSent: (n) => onProgress(offset + n, total),
    });
    if (sent === 'aborted') return aborted();
    if (sent === null || isServerFault(sent.status) || sent.body.error === 'interrupted') {
      const gave = await brokeOff();
      if (gave) return gave;
      continue;
    }
    if (sent.status === 200 && num(sent.body.offset) !== null) {
      offset = num(sent.body.offset) as number;
      attempt = 0;
    } else if (
      sent.status === 409 &&
      sent.body.error === 'offset-mismatch' &&
      num(sent.body.offset) !== null
    ) {
      offset = num(sent.body.offset) as number;
    } else if (sent.status === 429) {
      const seconds = Number(sent.header('Retry-After'));
      await pause((Number.isFinite(seconds) && seconds > 0 ? seconds : DEFAULT_RETRY_AFTER_S) * 1000, stop);
      resync = true;
    } else if (sent.status === 404) {
      session.id = null;
    } else {
      return failureOf(sent);
    }
  }
}
