// The places a message points at, as chips: in the field before it goes (with ×), under the person's bubble and in the
// queued row after. A chip shows the kind's icon or the design's picture and the name; pressed, it opens the place in
// the chat's right pane (features/pane/open-attached.ts). A chip taken out of the field leaves with its own animation.
import { useState } from 'react';
import { REF_ICON, refKey } from '@/next/features/chat/attached';
import { useStable } from '@/next/features/composer/use-stable';
import { openAttached } from '@/next/features/pane/open-attached';
import type { MessageRef } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { useLeavingList } from '@/next/ui/presence';
import { t } from '@/next/i18n';

/** A chip's exit, as long as hx-ref-out in composer.css. */
export const REF_EXIT_MS = 160;

const KIND_KEYS: Record<MessageRef['kind'], string> = {
  design: 'next.attach.kindDesign',
  doc: 'next.attach.kindDoc',
  board: 'next.attach.kindBoard',
  file: 'next.attach.kindFile',
  mr: 'next.attach.kindMr',
  task: 'next.attach.kindTask',
} as const;

export interface IRefChip extends Pick<MessageRef, 'kind' | 'id' | 'title'> {
  /** A design's picture; null draws the kind's icon. */
  thumb?: string | null;
}

const Thumb = ({ chip }: { chip: IRefChip }) => {
  const [broken, setBroken] = useState(false);
  if (chip.thumb && !broken) {
    return (
      <span className="hx-attach__thumb" data-pic="true">
        <img src={chip.thumb} alt="" onError={() => setBroken(true)} />
      </span>
    );
  }
  return (
    <span className="hx-attach__thumb">
      <Icon name={REF_ICON[chip.kind]} size="sm" />
    </span>
  );
};

interface IRefChipsProps {
  refs: readonly IRefChip[];
  /** In the field: each chip has its ×. */
  onRemove?: (ref: IRefChip) => void;
}

const NONE: readonly IRefChip[] = [];

/** The chips themselves, without a box of their own: they stand among the field's other chips. */
export const RefChips = ({ refs, onRemove }: IRefChipsProps) => {
  // A list built anew on every render is the same list while its places are: the leaving list compares by identity.
  const signature = refs.map((r) => `${refKey(r)}\t${r.title}\t${r.thumb ?? ''}`).join('\n');
  const list = useStable(refs, signature) ?? NONE;
  const shown = useLeavingList(list, refKey, REF_EXIT_MS);
  return (
    <>
      {shown.map(({ item: chip, leaving }) => {
        const name = chip.title || chip.id;
        return (
          <span
            key={refKey(chip)}
            className="hx-attach hx-refchip"
            data-kind="ref"
            data-ref={chip.kind}
            data-leaving={leaving || undefined}
            title={`${t(KIND_KEYS[chip.kind])}: ${name}`}
          >
            <button
              type="button"
              className="hx-refchip__open"
              aria-label={t('next.attach.open', { kind: t(KIND_KEYS[chip.kind]), name })}
              disabled={leaving}
              onClick={() => openAttached(chip)}
            >
              <Thumb chip={chip} />
              <span className="hx-attach__name">{name}</span>
            </button>
            {onRemove && (
              <button
                type="button"
                className="hx-attach__x"
                aria-label={t('next.composer.removeAttachment', { name })}
                disabled={leaving}
                onClick={() => onRemove(chip)}
              >
                <Icon name="x" size="sm" />
              </button>
            )}
          </span>
        );
      })}
    </>
  );
};
