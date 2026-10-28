// A deck's export, start to finish: ask the runtime for the file, follow the job until it is done, hand the file to
// the browser's download, and say so in a notice. The menu (deck-menu) and the share popover (a PDF for guests) call it.
import { deckExportHref, fetchDeckExport, startDeckExport } from '@/next/features/designly/deck/api';
import {
  DECK_EXPORT,
  DECK_JOB,
  type IDeckExportJob,
  type TDeckExport,
} from '@/next/features/designly/deck/types';
import { RuntimeError } from '@/next/core/net/runtime-json';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';

export const DECK_EXPORT_POLL_MS = 800;
/** A job that has not finished in this long is given up on here (the runtime stops it at 40 minutes). */
export const DECK_EXPORT_GIVE_UP_MS = 45 * 60 * 1000;
/** Polls that may fail in a row (a network blip) before the export is called lost. */
const POLL_FAILURES = 3;

const STARTED_TEXT = {
  [DECK_EXPORT.pdf]: 'next.deck.menu.pdfToast',
  [DECK_EXPORT.pptx]: 'next.deck.menu.pptxToast',
  [DECK_EXPORT.mp4]: 'next.deck.toast.videoBuilding',
  [DECK_EXPORT.png]: 'next.deck.menu.pngToast',
  [DECK_EXPORT.html]: 'next.deck.menu.htmlToast',
} as const satisfies Record<TDeckExport, string>;

export interface IDeckExportOptions {
  /** The recording to render (mp4). */
  rec?: string;
  /** A PDF also kept for the guests. */
  publish?: boolean;
  /** Called with 0..1 as the job moves. */
  onProgress?: (progress: number) => void;
  /** A guest-facing or background export says nothing and downloads nothing. */
  quiet?: boolean;
  /** An mp4 is not downloaded when it finishes: the person gets a notification and picks it up in the montage. */
  download?: boolean;
  signal?: AbortSignal;
}

const wait = (ms: number, signal?: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason ?? new DOMException('Aborted', 'AbortError'));
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      reject(signal?.reason ?? new DOMException('Aborted', 'AbortError'));
    };
    signal?.addEventListener('abort', onAbort, { once: true });
  });

/** Hands a finished file to the browser: an anchor the page clicks, the server names the file. */
export const downloadDeckFile = (href: string) => {
  const a = document.createElement('a');
  a.href = href;
  a.download = '';
  a.rel = 'noopener';
  document.body.appendChild(a);
  a.click();
  a.remove();
};

/** Follows a job to its end; resolves with the finished job, throws with the runtime's reason when it failed. */
export const followDeckExport = async (
  m: MachineId,
  artifact: string,
  first: IDeckExportJob,
  opts: Pick<IDeckExportOptions, 'onProgress' | 'signal'> = {},
): Promise<IDeckExportJob> => {
  const started = Date.now();
  let job = first;
  let failures = 0;
  for (;;) {
    opts.onProgress?.(job.progress);
    if (job.state === DECK_JOB.done) return job;
    if (job.state === DECK_JOB.failed) throw new RuntimeError(job.error ?? t('next.deck.menu.failed'));
    if (Date.now() - started > DECK_EXPORT_GIVE_UP_MS) throw new RuntimeError(t('next.deck.menu.failed'));
    await wait(DECK_EXPORT_POLL_MS, opts.signal);
    try {
      job = await fetchDeckExport(m, artifact, first.id, opts.signal);
      failures = 0;
    } catch (error) {
      if (opts.signal?.aborted) throw error;
      failures += 1;
      if (failures >= POLL_FAILURES) throw error;
    }
  }
};

/**
 * Builds one export and downloads it. A notice says it began («PDF собирается»); a failure says why in the runtime's
 * words. Resolves with the finished job; rejects when it failed or was aborted (and has already said so).
 */
export const runDeckExport = async (
  m: MachineId,
  artifact: string,
  format: TDeckExport,
  opts: IDeckExportOptions = {},
): Promise<IDeckExportJob> => {
  try {
    const first = await startDeckExport(m, artifact, format, opts.rec, opts.publish);
    if (!opts.quiet) toast(t(STARTED_TEXT[format]));
    const done = await followDeckExport(m, artifact, first, opts);
    if (opts.download !== false && format !== DECK_EXPORT.mp4 && !opts.quiet) {
      downloadDeckFile(deckExportHref(m, artifact, done.id));
    }
    return done;
  } catch (error) {
    if (!opts.signal?.aborted && !opts.quiet) {
      toast(t('next.deck.menu.failed'), {
        tone: 'error',
        description: error instanceof Error ? error.message : undefined,
      });
    }
    throw error;
  }
};
