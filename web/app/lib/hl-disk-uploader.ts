/**
 * The upload queue of the Disk tab, no React and no browser objects but `File`/`Blob` slices: one file at a time,
 * chunks of the size the server gave, `Upload-Offset` taken from the server after any break. The network is `IDiskDeps`.
 */
import { joinDiskPath } from '@/lib/hl-disk-format';
import { t } from '@/lib/i18n';

export const UPLOAD_STATE = {
  queued: 'queued',
  uploading: 'uploading',
  retrying: 'retrying',
  paused: 'paused',
  conflict: 'conflict',
  done: 'done',
  failed: 'failed',
  canceled: 'canceled',
  skipped: 'skipped',
} as const;
export type TUploadState = (typeof UPLOAD_STATE)[keyof typeof UPLOAD_STATE];

export const CONFLICT_CHOICE = { replace: 'replace', keepBoth: 'keep-both', skip: 'skip' } as const;
export type TConflictChoice = (typeof CONFLICT_CHOICE)[keyof typeof CONFLICT_CHOICE];

const POLICY_ASK = 'ask' as const;
type TPolicy = typeof POLICY_ASK | typeof CONFLICT_CHOICE.replace | typeof CONFLICT_CHOICE.keepBoth;

const ACTIVE_STATES: readonly TUploadState[] = [
  UPLOAD_STATE.queued,
  UPLOAD_STATE.uploading,
  UPLOAD_STATE.retrying,
  UPLOAD_STATE.paused,
  UPLOAD_STATE.conflict,
];
export const isActiveUpload = (state: TUploadState): boolean => ACTIVE_STATES.includes(state);

export const RETRY_BASE_MS = 1000;
export const RETRY_MAX_MS = 60_000;
export const RETRY_LIMIT = 30;
const DEFAULT_RETRY_AFTER_S = 2;
const EMIT_EVERY_MS = 100;
const SPEED_WINDOW_MS = 500;
const SPEED_SMOOTHING = 0.7;

/** 1 s, 2 s, 4 s… up to a minute. */
export const retryDelay = (attempt: number): number =>
  Math.min(RETRY_MAX_MS, RETRY_BASE_MS * 2 ** Math.max(0, attempt - 1));

export interface IDiskRequest {
  method: 'GET' | 'HEAD' | 'POST' | 'PUT' | 'DELETE';
  path: string;
  headers?: Record<string, string>;
  json?: unknown;
  body?: Blob;
  signal: AbortSignal;
  /** Bytes of `body` sent so far. */
  onProgress?: (loaded: number) => void;
}

export interface IDiskResponse {
  status: number;
  json: unknown;
  header: (name: string) => string | null;
}

export interface IDiskDeps {
  /** Resolves with any HTTP answer; rejects when there was none (network) or the signal aborted (`AbortError`). */
  request: (request: IDiskRequest) => Promise<IDiskResponse>;
  /** Rejects with `AbortError` when the signal aborts. */
  sleep: (ms: number, signal: AbortSignal) => Promise<void>;
  now: () => number;
}

export interface IDiskUpload {
  id: number;
  name: string;
  /** Folder of the file inside what was dropped, `a/b`; empty for a lone file. */
  dir: string;
  size: number;
  sent: number;
  speed: number;
  state: TUploadState;
  /** Why it failed, or what it is waiting for (`retrying`). */
  note: string | null;
}

export interface IDiskTarget {
  root: string;
  path: string;
}

interface IRuntime {
  file: File;
  target: IDiskTarget;
  policy: TPolicy;
  uploadId: string | null;
  controller: AbortController;
  paused: boolean;
  canceled: boolean;
  stopped: boolean;
  attempt: number;
  wake: (() => void) | null;
  choose: ((choice: TConflictChoice | null) => void) | null;
  mark: { at: number; sent: number } | null;
}

class UploadFailure extends Error {}
class UploadStopped extends Error {}
class UploadSkipped extends Error {}

const STATUS_NO_SPACE = 507;

/** A fault that may pass by itself; a full disk does not. */
const isServerFault = (status: number): boolean => status >= 500 && status !== STATUS_NO_SPACE;

const isAbort = (error: unknown): boolean => error instanceof Error && error.name === 'AbortError';

const asRecord = (value: unknown): Record<string, unknown> =>
  typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {};

const numberOf = (value: unknown): number | null =>
  typeof value === 'number' && Number.isFinite(value) ? value : null;

/** The server's own `message` wins; the rest is by status. */
export const uploadFailureText = (status: number, json: unknown): string => {
  const { message, error } = asRecord(json);
  if (typeof message === 'string' && message) return message;
  if (status === STATUS_NO_SPACE || error === 'no-space') return t('lib.hlDiskUploader.noSpace');
  if (status === 403) return t('lib.hlDiskUploader.forbidden');
  if (status === 404) return t('lib.hlDiskUploader.uploadNotFound');
  if (status === 413) return t('lib.hlDiskUploader.chunkTooLarge');
  return t('lib.hlDiskUploader.machineError', { status });
};

export class DiskUploadQueue {
  private items: IDiskUpload[] = [];
  private runtimes = new Map<number, IRuntime>();
  private listeners = new Set<() => void>();
  private nextId = 1;
  private running = false;
  private forAll: TConflictChoice | null = null;
  private lastEmit = 0;

  constructor(private readonly deps: IDiskDeps) {}

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getSnapshot = (): readonly IDiskUpload[] => this.items;

  enqueue(target: IDiskTarget, picks: readonly { file: File; dir: string }[]): void {
    const added = picks.map(({ file, dir }): IDiskUpload => {
      const id = this.nextId++;
      const policy: TPolicy =
        this.forAll === CONFLICT_CHOICE.replace || this.forAll === CONFLICT_CHOICE.keepBoth
          ? this.forAll
          : POLICY_ASK;
      this.runtimes.set(id, {
        file,
        target,
        policy,
        uploadId: null,
        controller: new AbortController(),
        paused: false,
        canceled: false,
        stopped: false,
        attempt: 0,
        wake: null,
        choose: null,
        mark: null,
      });
      return {
        id,
        name: file.name,
        dir,
        size: file.size,
        sent: 0,
        speed: 0,
        state: UPLOAD_STATE.queued,
        note: null,
      };
    });
    if (added.length === 0) return;
    this.items = [...this.items, ...added];
    this.emit(true);
    void this.pump();
  }

  pause(id: number): void {
    const rt = this.runtimes.get(id);
    const state = this.item(id)?.state;
    if (!rt || rt.paused || (state !== UPLOAD_STATE.uploading && state !== UPLOAD_STATE.retrying)) return;
    rt.paused = true;
    rt.controller.abort();
    this.patch(id, { state: UPLOAD_STATE.paused, speed: 0, note: null });
  }

  resume(id: number): void {
    const rt = this.runtimes.get(id);
    if (!rt?.paused) return;
    rt.paused = false;
    const wake = rt.wake;
    rt.wake = null;
    if (this.item(id)?.state === UPLOAD_STATE.paused) this.patch(id, { state: UPLOAD_STATE.uploading });
    wake?.();
  }

  cancel(id: number): void {
    const rt = this.runtimes.get(id);
    const item = this.item(id);
    if (!rt || !item || !isActiveUpload(item.state)) return;
    rt.canceled = true;
    rt.controller.abort();
    rt.wake?.();
    rt.choose?.(null);
    if (item.state === UPLOAD_STATE.queued) this.patch(id, { state: UPLOAD_STATE.canceled, speed: 0 });
    if (rt.uploadId) {
      const controller = new AbortController();
      void this.deps
        .request({ method: 'DELETE', path: `/api/fs/uploads/${rt.uploadId}`, signal: controller.signal })
        .catch(() => {});
    }
  }

  resolveConflict(id: number, choice: TConflictChoice, forAll: boolean): void {
    const rt = this.runtimes.get(id);
    if (!rt?.choose) return;
    if (forAll) this.forAll = choice;
    const choose = rt.choose;
    rt.choose = null;
    choose(choice);
  }

  /** The tab closed or the page is leaving: nothing keeps running, the `.part` files stay on the machine for a later try. */
  stopAll(): void {
    for (const item of this.items) {
      const rt = this.runtimes.get(item.id);
      if (!rt || !isActiveUpload(item.state)) continue;
      rt.stopped = true;
      rt.controller.abort();
      rt.wake?.();
      rt.choose?.(null);
      if (item.state === UPLOAD_STATE.queued)
        this.patch(item.id, { state: UPLOAD_STATE.failed, note: t('lib.hlDiskUploader.stoppedTabClosed') });
    }
  }

  clearFinished(): void {
    const kept = this.items.filter((item) => isActiveUpload(item.state));
    for (const item of this.items) if (!isActiveUpload(item.state)) this.runtimes.delete(item.id);
    this.items = kept;
    this.emit(true);
  }

  private item(id: number): IDiskUpload | undefined {
    return this.items.find((item) => item.id === id);
  }

  private patch(id: number, change: Partial<IDiskUpload>, force = true): void {
    this.items = this.items.map((item) => (item.id === id ? { ...item, ...change } : item));
    this.emit(force);
  }

  private emit(force: boolean): void {
    const now = this.deps.now();
    if (!force && now - this.lastEmit < EMIT_EVERY_MS) return;
    this.lastEmit = now;
    for (const listener of this.listeners) listener();
  }

  private async pump(): Promise<void> {
    if (this.running) return;
    this.running = true;
    try {
      for (let next = this.nextQueued(); next; next = this.nextQueued()) await this.process(next);
    } finally {
      this.running = false;
    }
  }

  private nextQueued(): IDiskUpload | undefined {
    return this.items.find((item) => item.state === UPLOAD_STATE.queued);
  }

  private async process(item: IDiskUpload): Promise<void> {
    const rt = this.runtimes.get(item.id);
    if (!rt) return;
    this.patch(item.id, { state: UPLOAD_STATE.uploading });
    try {
      await this.upload(item.id, rt, item.size);
      this.patch(item.id, { state: UPLOAD_STATE.done, sent: item.size, speed: 0, note: null });
    } catch (error) {
      if (error instanceof UploadSkipped)
        this.patch(item.id, { state: UPLOAD_STATE.skipped, speed: 0, note: null });
      else if (error instanceof UploadFailure)
        this.patch(item.id, { state: UPLOAD_STATE.failed, speed: 0, note: error.message });
      else if (error instanceof UploadStopped) {
        if (rt.stopped)
          this.patch(item.id, {
            state: UPLOAD_STATE.failed,
            speed: 0,
            note: t('lib.hlDiskUploader.stoppedTabClosed'),
          });
        else this.patch(item.id, { state: UPLOAD_STATE.canceled, speed: 0, note: null });
      } else {
        this.patch(item.id, {
          state: UPLOAD_STATE.failed,
          speed: 0,
          note: error instanceof Error ? error.message : t('lib.hlDiskUploader.failed'),
        });
      }
    }
  }

  /** Waits while paused; leaves with `UploadStopped` once canceled or the tab is gone. */
  private async gate(rt: IRuntime): Promise<void> {
    while (rt.paused && !rt.canceled && !rt.stopped) {
      await new Promise<void>((resolve) => {
        rt.wake = resolve;
      });
    }
    if (rt.canceled || rt.stopped) throw new UploadStopped();
    rt.controller = new AbortController();
    rt.mark = null;
  }

  /** One try: the answer, or null when there was none (network) or the request was cut for a pause. */
  private async once(rt: IRuntime, request: Omit<IDiskRequest, 'signal'>): Promise<IDiskResponse | null> {
    try {
      const response = await this.deps.request({ ...request, signal: rt.controller.signal });
      return response;
    } catch {
      if (rt.canceled || rt.stopped) throw new UploadStopped();
      return null;
    }
  }

  private async wait(id: number, rt: IRuntime, ms: number, note: string): Promise<void> {
    this.patch(id, { state: UPLOAD_STATE.retrying, speed: 0, note });
    try {
      await this.deps.sleep(ms, rt.controller.signal);
    } catch (error) {
      if (rt.canceled || rt.stopped) throw new UploadStopped();
      if (!isAbort(error)) throw error;
    }
    if (!rt.paused) this.patch(id, { state: UPLOAD_STATE.uploading, note: null });
  }

  private async backoff(id: number, rt: IRuntime): Promise<void> {
    rt.attempt += 1;
    if (rt.attempt > RETRY_LIMIT) throw new UploadFailure(t('lib.hlDiskUploader.offlineStopped'));
    const ms = retryDelay(rt.attempt);
    await this.wait(id, rt, ms, t('lib.hlDiskUploader.offlineRetry', { seconds: Math.ceil(ms / 1000) }));
  }

  /** A request that is repeated as it is on a network break, a 5xx and a 429; anything else is the answer. */
  private async call(
    id: number,
    rt: IRuntime,
    request: Omit<IDiskRequest, 'signal'>,
  ): Promise<IDiskResponse> {
    for (;;) {
      await this.gate(rt);
      const response = await this.once(rt, request);
      if (!response) {
        if (!rt.paused) await this.backoff(id, rt);
      } else if (response.status === 429) {
        await this.rateLimited(id, rt, response);
      } else if (isServerFault(response.status)) {
        await this.backoff(id, rt);
      } else {
        return response;
      }
    }
  }

  private async rateLimited(id: number, rt: IRuntime, response: IDiskResponse): Promise<void> {
    const seconds = Number(response.header('Retry-After'));
    const wait = Number.isFinite(seconds) && seconds > 0 ? seconds : DEFAULT_RETRY_AFTER_S;
    await this.wait(id, rt, wait * 1000, t('lib.hlDiskUploader.machineBusy'));
  }

  private progress(id: number, rt: IRuntime, sent: number): void {
    const now = this.deps.now();
    rt.mark ??= { at: now, sent };
    const elapsed = now - rt.mark.at;
    const item = this.item(id);
    if (elapsed < SPEED_WINDOW_MS || !item) {
      this.patch(id, { sent }, false);
      return;
    }
    const instant = ((sent - rt.mark.sent) / elapsed) * 1000;
    const speed = item.speed > 0 ? item.speed * SPEED_SMOOTHING + instant * (1 - SPEED_SMOOTHING) : instant;
    rt.mark = { at: now, sent };
    this.patch(id, { sent, speed }, false);
  }

  private async open(
    id: number,
    rt: IRuntime,
    size: number,
    name: string,
  ): Promise<{ uploadId: string; offset: number; chunkSize: number }> {
    for (;;) {
      const path = joinDiskPath(rt.target.path, this.item(id)?.dir ?? '');
      const response = await this.call(id, rt, {
        method: 'POST',
        path: '/api/fs/uploads',
        headers: { 'Content-Type': 'application/json' },
        json: { root: rt.target.root, path, name, size, onConflict: rt.policy },
      });
      const body = asRecord(response.json);
      if (response.status === 201) {
        const uploadId = body.uploadId;
        const chunkSize = numberOf(body.chunkSize);
        if (typeof uploadId !== 'string' || !chunkSize || chunkSize <= 0)
          throw new UploadFailure(t('lib.hlDiskUploader.incompleteCreate'));
        rt.attempt = 0;
        if (typeof body.name === 'string' && body.name !== name) this.patch(id, { name: body.name });
        return { uploadId, offset: numberOf(body.offset) ?? 0, chunkSize };
      }
      if (response.status !== 409 || body.error !== 'exists')
        throw new UploadFailure(uploadFailureText(response.status, response.json));
      const choice = this.forAll ?? (await this.askConflict(id, rt));
      if (choice === CONFLICT_CHOICE.skip) throw new UploadSkipped();
      rt.policy = choice;
    }
  }

  private async askConflict(id: number, rt: IRuntime): Promise<TConflictChoice> {
    this.patch(id, { state: UPLOAD_STATE.conflict, note: null });
    const choice = await new Promise<TConflictChoice | null>((resolve) => {
      rt.choose = resolve;
    });
    if (!choice) throw new UploadStopped();
    this.patch(id, { state: UPLOAD_STATE.uploading });
    return choice;
  }

  private async upload(id: number, rt: IRuntime, size: number): Promise<void> {
    const name = rt.file.name;
    const session = await this.open(id, rt, size, name);
    rt.uploadId = session.uploadId;
    const base = `/api/fs/uploads/${session.uploadId}`;
    let { offset } = session;
    let resync = false;
    for (;;) {
      if (resync) {
        const head = await this.call(id, rt, { method: 'HEAD', path: base });
        if (head.status !== 200) throw new UploadFailure(uploadFailureText(head.status, head.json));
        const known = Number(head.header('Upload-Offset'));
        if (!Number.isFinite(known)) throw new UploadFailure(t('lib.hlDiskUploader.missingOffset'));
        offset = known;
        resync = false;
      }
      this.patch(id, { sent: offset }, false);
      if (offset >= size) {
        const done = await this.call(id, rt, { method: 'POST', path: `${base}/complete` });
        if (done.status === 200) return;
        const known = numberOf(asRecord(done.json).offset);
        if (done.status === 409 && known !== null) {
          offset = known;
          continue;
        }
        throw new UploadFailure(uploadFailureText(done.status, done.json));
      }
      await this.gate(rt);
      const end = Math.min(size, offset + session.chunkSize);
      const response = await this.once(rt, {
        method: 'PUT',
        path: base,
        headers: { 'Upload-Offset': String(offset) },
        body: rt.file.slice(offset, end),
        onProgress: (loaded) => this.progress(id, rt, offset + loaded),
      });
      if (!response || isServerFault(response.status)) {
        resync = true;
        if (!rt.paused) await this.backoff(id, rt);
        continue;
      }
      const body = asRecord(response.json);
      if (response.status === 200) {
        rt.attempt = 0;
        offset = numberOf(body.offset) ?? end;
      } else if (
        response.status === 409 &&
        body.error === 'offset-mismatch' &&
        numberOf(body.offset) !== null
      ) {
        offset = numberOf(body.offset) as number;
      } else if (response.status === 429) {
        await this.rateLimited(id, rt, response);
      } else {
        throw new UploadFailure(uploadFailureText(response.status, response.json));
      }
    }
  }
}
