// The IImageEditHost of a Sinopia design (features/image-edit/host.ts): the pictures live in the project, a change is a
// job of `<platform>/design/image` that draws a new file into assets/, and the version the person chooses is written
// into the element of the board by the patch op `image`. A version is named by its path in the project. What the element
// remembers (data-gen-job, data-gen-from in the board's source) is what makes the versions outlive a reload.
import { RuntimeError } from '@/next/core/net/runtime-json';
import { t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';
import type { MachineId } from '@/next/protocol/v2';
import {
  EDIT_MODE,
  type IEditProgress,
  type IImageEdit,
  type IImageEditHost,
  type IImageVersion,
} from '@/next/features/image-edit/host';
import {
  JOB_STATE,
  projectPictureUrl,
  readGenJob,
  readGenVariants,
  startDesignEdit,
} from '@/next/features/designly/image-gen/api';
import {
  latestGenWas,
  recordGenDone,
  veilEdit,
  type IGenApplied,
} from '@/next/features/designly/image-gen/gen-store';

/** A change of the element's picture, as the board's patch takes it. */
export interface IImageChange {
  op: 'image';
  tpl: number;
  was: string;
  src: string;
  /** Absent: the original put back (the element forgets what it was drawn from). */
  job?: string;
  prompt?: string;
  from?: string;
}

/** The element a picture is changed in, as the page told it when the person pressed the button. */
export interface IDesignPicture {
  machine: MachineId;
  artifact: string;
  /** `genKey(artifact, board, tpl)`. */
  key: string;
  tpl: number;
  /** The element's fingerprint when it was pressed; a later write uses the latest one the machine gave. */
  was: string;
  /** What the element was drawn from (its brief and job), the pictures it was changed from, its name. */
  gen: { prompt: string; job: string } | null;
  from: readonly string[];
  label: string;
  /** Writes the change into the board; null when it was refused (the person was told). */
  write: (change: IImageChange) => Promise<IGenApplied | null>;
}

const GEN_FILE = /^assets\/(gen-[a-z0-9]{4,32})-\d{1,2}\.(?:png|jpe?g|webp)$/i;
const RUN_SEPARATOR = '|';

/** A picture of the project as a version of the editor. */
export const versionOf = (p: Pick<IDesignPicture, 'machine' | 'artifact'>, path: string): IImageVersion => ({
  id: path,
  src: projectPictureUrl(p.machine, p.artifact, path),
});

/** The words an element remembers of a change: the person's own, else what the tool was. */
const briefOf = (edit: IImageEdit): string => {
  const own = edit.prompt.trim();
  return own !== '' ? own : t(`next.imageEdit.tool.${edit.mode === EDIT_MODE.text ? 'mark' : edit.mode}`);
};

interface IMade {
  prompt: string;
  from: string;
}

export const designImageHost = (p: IDesignPicture): IImageEditHost => {
  /** What each change in this host was made from, by run and then by the picture that came out of it. */
  const runs = new Map<string, IMade>();
  const made = new Map<string, IMade>();
  /** Writes go one after another: each is made against the fingerprint the one before gave. */
  let queue: Promise<unknown> = Promise.resolve();

  const trailWith = (source: string): string[] => {
    const trail = p.from.filter((path) => path !== source);
    return [...trail, source];
  };

  return {
    start: async (edit) => {
      const started = await startDesignEdit(p.machine, {
        artifact: p.artifact,
        source: edit.from.id,
        mode: edit.mode,
        prompt: edit.prompt,
        format: edit.format,
        overlay: edit.overlay,
      });
      const run = `${started.job.id}${RUN_SEPARATOR}${started.name}`;
      runs.set(run, { prompt: briefOf(edit), from: edit.from.id });
      veilEdit(p.machine, p.key, p.artifact, p.tpl, started.job.id, started.name);
      return run;
    },

    read: async (run, signal): Promise<IEditProgress> => {
      const at = run.indexOf(RUN_SEPARATOR);
      if (at <= 0) throw new RuntimeError(t('next.failure.http'));
      const job = await readGenJob(p.machine, run.slice(0, at), run.slice(at + 1), signal);
      if (job.state === JOB_STATE.failed) {
        return { state: 'failed', versions: [], error: job.error ?? undefined };
      }
      const pic = job.pics[job.pics.length - 1];
      if (job.state === JOB_STATE.running && pic === undefined) return { state: 'running', versions: [] };
      if (pic === undefined) return { state: 'failed', versions: [], error: t('next.imageEdit.noPicture') };
      const known = runs.get(run);
      if (known) made.set(pic.src, known);
      return { state: 'done', versions: [versionOf(p, pic.src)] };
    },

    versions: async (original, signal) => {
      const list = p.from.map((path) => versionOf(p, path));
      if (p.gen) {
        const pics = await readGenVariants(p.machine, p.artifact, p.gen.job, signal);
        for (const pic of pics) list.push(versionOf(p, pic.src));
      }
      // The pictures it was changed from come first (the oldest at the left), then what the job drew.
      const seen = new Set<string>();
      return [...list, original].filter((version) => {
        if (seen.has(version.id)) return false;
        seen.add(version.id);
        return true;
      });
    },

    chosen: (version) => {
      queue = queue
        .then(async () => {
          const was = latestGenWas(p.key) ?? p.was;
          const job = GEN_FILE.exec(version.id)?.[1]?.toLowerCase();
          const by = made.get(version.id);
          const change: IImageChange =
            job === undefined
              ? { op: 'image', tpl: p.tpl, was, src: version.id }
              : {
                  op: 'image',
                  tpl: p.tpl,
                  was,
                  src: version.id,
                  job,
                  prompt: by?.prompt ?? p.gen?.prompt ?? (p.label || t('next.imagehover.picture')),
                  ...(by
                    ? { from: trailWith(by.from).join(' ') }
                    : p.from.length > 0
                      ? { from: p.from.join(' ') }
                      : {}),
                };
          const applied = await p.write(change);
          if (applied !== null && change.job !== undefined && change.prompt !== undefined) {
            recordGenDone(p.key, { prompt: change.prompt, job: change.job }, applied.was);
          }
        })
        .catch((failure: Error) =>
          toast(t('next.deck.edit.failed'), { tone: 'error', description: failure.message }),
        );
    },
  };
};
