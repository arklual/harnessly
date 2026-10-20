import { t } from '@/lib/i18n';
import { appFetch, scopedAppPath } from '@/lib/app-path';
import { getDeviceId } from '@/lib/push-device';
import type { ICheckReport } from '@/types/server/check';
import type {
  IBoardHistoryResponse,
  ILibraryBoard,
  ILibraryCreateResponse,
  ILibraryImportResponse,
  ILibraryResponse,
} from '@/lib/hl-design-library/types';
import type { IResolvedMatch } from '@/types/server/resolve';

/** A refusal of the runtime worded for the person; `body` is the whole answer (a partly done request names what it made). */
export class DesignRequestError extends Error {
  constructor(
    message: string,
    readonly body: unknown,
  ) {
    super(message);
  }
}

/** Every design request names its machine explicitly; without one it goes to the page's own runtime. */
const designRequest = async <T>(
  machineId: string | undefined,
  path: string,
  init?: RequestInit,
): Promise<T> => {
  const response = await appFetch(scopedAppPath(path, machineId), init);
  const body: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const error =
      typeof body === 'object' && body !== null && 'error' in body && typeof body.error === 'string'
        ? body.error
        : '';
    throw new DesignRequestError(
      error || t('lib.hlDesignLibraryClient.serviceUnavailable', { status: response.status }),
      body,
    );
  }
  return body as T;
};

const jsonInit = (method: string, body: unknown): RequestInit => ({
  method,
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});

/** The design library with the agents working on each artifact; `tabId` marks this conversation's own. */
export const fetchDesignLibrary = (
  machineId: string | undefined,
  tabId: string | null,
  options: { lite?: boolean; signal?: AbortSignal } = {},
) => {
  const params = new URLSearchParams({
    ...(tabId ? { tab: tabId } : {}),
    ...(options.lite ? { lite: '1' } : {}),
  });
  const query = params.toString();
  return designRequest<ILibraryResponse>(
    machineId,
    `/api/platform/design/library${query ? `?${query}` : ''}`,
    { signal: options.signal },
  );
};

/** A Claude Design export dropped on the tab: the archive itself is the body, its name goes in a header. */
export const uploadDesignProject = (machineId: string | undefined, file: File) =>
  designRequest<ILibraryImportResponse>(machineId, '/api/platform/design/library/import', {
    method: 'POST',
    headers: { 'Content-Type': 'application/zip', 'X-Design-Filename': encodeURIComponent(file.name) },
    body: file,
  });

/**
 * The same upload with the share of the archive received, which `fetch` cannot tell: the runtime answers only after it
 * has unpacked the project, so a progress of 1 means «unpacking».
 */
export const uploadDesignProjectWithProgress = (
  machineId: string | undefined,
  file: File,
  onProgress: (share: number) => void,
): Promise<ILibraryImportResponse> =>
  new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open('POST', scopedAppPath('/api/platform/design/library/import', machineId));
    request.setRequestHeader('Content-Type', 'application/zip');
    request.setRequestHeader('X-Design-Filename', encodeURIComponent(file.name));
    request.upload.onprogress = (event) => {
      if (event.lengthComputable && event.total > 0) onProgress(event.loaded / event.total);
    };
    request.onerror = () => reject(new Error(t('lib.hlDesignLibraryClient.uploadOffline')));
    request.onload = () => {
      let body: unknown = null;
      try {
        body = JSON.parse(request.responseText);
      } catch {
        body = null;
      }
      if (request.status >= 200 && request.status < 300) return resolve(body as ILibraryImportResponse);
      const error =
        typeof body === 'object' && body !== null && 'error' in body && typeof body.error === 'string'
          ? body.error
          : '';
      reject(
        new DesignRequestError(
          error || t('lib.hlDesignLibraryClient.serviceUnavailable', { status: request.status }),
          body,
        ),
      );
    };
    request.send(file);
  });

/** The file and line of an element picked on a page of the library server: `path` is the page's path there. */
export const locateDesignElement = async (
  machineId: string | undefined,
  path: string,
  text: string,
): Promise<IResolvedMatch[]> =>
  (
    await designRequest<{ matches: IResolvedMatch[] }>(
      machineId,
      '/api/platform/design/library/locate',
      jsonInit('POST', { path, text }),
    )
  ).matches;

/** Rename, reorder, duplicate or remove a board of a library design, or rename the design itself. */
export const editDesignBoards = (
  machineId: string | undefined,
  artifact: string,
  edit: {
    action: 'rename' | 'move' | 'duplicate' | 'remove' | 'title';
    path?: string;
    title?: string;
    delta?: number;
  },
) =>
  designRequest<{ boards: ILibraryBoard[] }>(
    machineId,
    '/api/platform/design/library/boards',
    jsonInit('POST', { artifact, ...edit }),
  );

/** The saved versions of a board file, the latest first. */
export const fetchBoardHistory = (
  machineId: string | undefined,
  artifact: string,
  board: string,
  signal?: AbortSignal,
) =>
  designRequest<IBoardHistoryResponse>(
    machineId,
    `/api/platform/design/library/history?artifact=${encodeURIComponent(artifact)}&board=${encodeURIComponent(board)}`,
    { signal },
  );

/** "Restore this version": answers when the board is back as it was; the version it had is kept in the history. */
export const restoreBoardVersion = (
  machineId: string | undefined,
  artifact: string,
  board: string,
  revision: string,
): Promise<void> =>
  designRequest<void>(
    machineId,
    '/api/platform/design/library/history',
    jsonInit('POST', { artifact, board, revision }),
  );

/** The last check of a design, filling while it runs. */
export const fetchDesignCheck = (machineId: string | undefined, artifact: string, signal?: AbortSignal) =>
  designRequest<{ report: ICheckReport | null }>(
    machineId,
    `/api/platform/design/library/check?artifact=${encodeURIComponent(artifact)}`,
    { signal },
  );

/** "Check": every board opened as a desktop and a phone, with screenshots and console errors. */
export const startDesignCheck = (machineId: string | undefined, artifact: string) =>
  designRequest<{ report: ICheckReport }>(
    machineId,
    '/api/platform/design/library/check',
    jsonInit('POST', { artifact }),
  );

/** "Continue in a design conversation": a new agent conversation on the design, working the way Claude Design does; `prompt` is what it does first. */
export const continueDesignDialog = (machineId: string | undefined, artifact: string, prompt?: string) =>
  designRequest<{ tabId: string }>(
    machineId,
    '/api/platform/design/library/continue',
    jsonInit('POST', { artifact, ...(prompt ? { prompt } : {}) }),
  );

/** "Message the agent": the text goes into that agent's conversation with the design named; `blocks` pastes several lines (picked-element comments). */
export const messageDesignAgent = (
  machineId: string | undefined,
  tabId: string,
  artifact: string,
  text: string,
  options: { blocks?: boolean } = {},
): Promise<void> =>
  designRequest<void>(
    machineId,
    '/api/platform/design/library/message',
    jsonInit('POST', { tabId, artifact, text, ...(options.blocks ? { blocks: true } : {}) }),
  );

/** "New project": the template copied to a project of its own and a conversation started in it with the brief. */
export const createDesignProject = (
  machineId: string | undefined,
  request: { title: string; brief: string },
) =>
  designRequest<ILibraryCreateResponse>(
    machineId,
    '/api/platform/design/library/create',
    jsonInit('POST', request),
  );

/** "Send to phone": a push to the person's other devices that opens the board alone in the window. */
export const sendBoardToPhone = (
  machineId: string | undefined,
  request: { artifact: string; board: string; title?: string },
) =>
  designRequest<{ sent: number }>(
    machineId,
    '/api/platform/design/library/send',
    jsonInit('POST', { ...request, deviceId: getDeviceId() }),
  );
