// Drawing a picture into a block picked on a canvas board (and the veil over it while it is drawn). A canvas board has
// no deck page: it is named by its file, and the block by the template number and fingerprint the board gave when it
// was picked. The card at the picked block shows `CardImageGen`; the site view puts the veils of `useBoardVeils` on the
// page.
import { useState } from 'react';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';
import { patchDeck } from '@/next/features/designly/deck/api';
import type { IPickedDraw } from '@/next/features/designly/page-bridge';
import ImageGenPanel, { imageTargetOf } from '@/next/features/designly/image-gen/gen-panel';
import type { IImageChange } from '@/next/features/designly/image-gen/design-host';
import {
  genKey,
  latestGenWas,
  useGenDone,
  useGenRun,
  useGenRuns,
  type IGenApplied,
  type TGenApply,
} from '@/next/features/designly/image-gen/gen-store';

const PANEL_EXIT_MS = 160;
const BOARD_SUFFIX = '.dc.html';

/**
 * The board file of a page address of a design (`/a/<id>/p/<file>`): only a board at the root of the project can be
 * written; null for anything else (a site, a wrapper's page).
 */
export const boardFileOf = (artifact: string, pageUrl: string): string | null => {
  const prefix = `/a/${artifact}/p/`;
  if (!pageUrl.startsWith(prefix)) return null;
  const rest = pageUrl.slice(prefix.length).split(/[?#]/)[0] ?? '';
  let file: string;
  try {
    file = decodeURIComponent(rest);
  } catch {
    return null;
  }
  return file.endsWith(BOARD_SUFFIX) && !file.includes('/') ? file : null;
};

/** The blocks of the board on screen a picture is being drawn into now. */
export const useBoardVeils = (artifact: string, pageUrl: string): readonly number[] => {
  const runs = useGenRuns();
  const file = boardFileOf(artifact, pageUrl);
  if (file === null) return [];
  return runs.filter((run) => run.key === genKey(artifact, file, run.tpl)).map((run) => run.tpl);
};

/** Writes a change of a picture into a board named by its file; null once refused (the person was told). */
export const fileWriter =
  (machine: MachineId, artifact: string, file: string) =>
  (change: IImageChange): Promise<IGenApplied | null> =>
    patchDeck(machine, artifact, { file }, change).then(
      (result) => {
        if (result.ok) return { was: result.was };
        toast(t(result.code === 'dynamic' ? 'next.deck.edit.dynamic' : 'next.deck.edit.changed'), {
          tone: 'warning',
        });
        return null;
      },
      (failure: Error) => {
        toast(t('next.deck.edit.failed'), { tone: 'error', description: failure.message });
        return null;
      },
    );

const applyOn =
  (machine: MachineId, artifact: string, file: string, draw: IPickedDraw): TGenApply =>
  (pic, drawn) =>
    fileWriter(
      machine,
      artifact,
      file,
    )({
      op: 'image',
      tpl: draw.tpl,
      was: latestGenWas(genKey(artifact, file, draw.tpl)) ?? draw.was,
      src: pic.src,
      job: drawn.job,
      prompt: drawn.prompt,
    });

export interface ICardImageGenProps {
  machine: MachineId;
  artifact: string;
  /** The board file the block was picked on. */
  file: string;
  draw: IPickedDraw;
}

/** In the card at a picked block: «Сгенерировать картинку» (or «Перегенерировать») opening the generator under it. */
export const CardImageGen = ({ machine, artifact, file, draw }: ICardImageGenProps) => {
  const [open, setOpen] = useState(false);
  const shown = usePresence(open ? draw : null, PANEL_EXIT_MS);
  const target = imageTargetOf(artifact, file, draw);
  const run = useGenRun(target.key);
  // What the block was drawn from, as of the last write of this session: the pick's own word is older.
  const drawn = useGenDone(target.key) ?? draw.gen;
  return (
    <div className="hx-ann__gen">
      <button
        type="button"
        className="hx-ann__gen-btn"
        aria-expanded={open}
        data-busy={run !== null ? 'true' : undefined}
        onClick={() => setOpen(!open)}
      >
        <Icon name="sparkles" size="sm" />
        {drawn ? t('next.imagegen.regenerate') : t('next.imagegen.generate')}
      </button>
      {shown.value !== null && (
        <div className="hx-ann__gen-panel" data-closing={shown.state === PRESENCE.closing ? '1' : undefined}>
          <ImageGenPanel
            key={`${draw.tpl}:${draw.gen?.job ?? ''}`}
            machine={machine}
            target={target}
            apply={applyOn(machine, artifact, file, draw)}
            embedded
          />
        </div>
      )}
    </div>
  );
};
