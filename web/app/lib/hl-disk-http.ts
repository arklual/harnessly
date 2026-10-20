/** The browser side of `IDiskDeps`: XHR, because only it reports how much of a chunk has left. */
import { scopedAppPath } from '@/lib/app-path';
import type { IDiskDeps, IDiskRequest, IDiskResponse } from '@/lib/hl-disk-uploader';

const abortError = (): Error => Object.assign(new Error('aborted'), { name: 'AbortError' });

const parseJson = (text: string): unknown => {
  try {
    return text ? JSON.parse(text) : null;
  } catch {
    return null;
  }
};

export const xhrRequest = ({
  method,
  path,
  headers,
  json,
  body,
  signal,
  onProgress,
}: IDiskRequest): Promise<IDiskResponse> =>
  new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(abortError());
      return;
    }
    const xhr = new XMLHttpRequest();
    xhr.open(method, scopedAppPath(path));
    xhr.responseType = 'text';
    for (const [name, value] of Object.entries(headers ?? {})) xhr.setRequestHeader(name, value);
    const onAbort = () => xhr.abort();
    signal.addEventListener('abort', onAbort, { once: true });
    const finish = () => signal.removeEventListener('abort', onAbort);
    if (onProgress) xhr.upload.onprogress = (event) => onProgress(event.loaded);
    xhr.onload = () => {
      finish();
      resolve({
        status: xhr.status,
        json: parseJson(xhr.responseText),
        header: (name) => xhr.getResponseHeader(name),
      });
    };
    xhr.onerror = () => {
      finish();
      reject(new Error('network'));
    };
    xhr.ontimeout = xhr.onerror;
    xhr.onabort = () => {
      finish();
      reject(abortError());
    };
    xhr.send(json === undefined ? (body ?? null) : JSON.stringify(json));
  });

export const browserSleep = (ms: number, signal: AbortSignal): Promise<void> =>
  new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(abortError());
      return;
    }
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      reject(abortError());
    };
    signal.addEventListener('abort', onAbort, { once: true });
  });

export const browserDiskDeps = (): IDiskDeps => ({
  request: xhrRequest,
  sleep: browserSleep,
  now: () => Date.now(),
});
