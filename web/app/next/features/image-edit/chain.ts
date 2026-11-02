// The versions of a picture, read from the jobs of a chat: every finished job whose source is the original or a version
// already in the chain adds its last picture. Ordered by when the job started, the original first.
import type { IImageVersion } from '@/next/features/image-edit/host';

/** What the chain needs of a job (chat/image-jobs.ts has the rest). */
export interface IChainJob {
  state: string;
  paths: readonly string[];
  source?: string;
  mode?: string;
  startedAt: number;
}

export const CUT_MODE = 'cut';

export const chainVersions = (
  original: IImageVersion,
  jobs: readonly IChainJob[],
  srcOf: (path: string) => string,
): IImageVersion[] => {
  const out: IImageVersion[] = [original];
  const known = new Set([original.id]);
  const ordered = [...jobs].sort((a, b) => a.startedAt - b.startedAt);
  for (const job of ordered) {
    if (job.state !== 'done' || job.source === undefined || !known.has(job.source)) continue;
    const path = job.paths[job.paths.length - 1];
    if (path === undefined || known.has(path)) continue;
    known.add(path);
    out.push({ id: path, src: srcOf(path), clear: job.mode === CUT_MODE ? true : undefined });
  }
  return out;
};
