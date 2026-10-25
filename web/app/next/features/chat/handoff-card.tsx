// A context block in the feed (features/chat/handoff.ts): «Контекст из «Контракт v2»» with how it came (an excerpt
// or the agent's note), the text folded under it, and a link back to the chat it came from while that is here.
import { memo, useContext, useState, type ReactNode } from 'react';
import Md from '@/next/features/chat/md';
import {
  BLOCK_KIND,
  BLOCK_MODE,
  ChatLinks,
  type IContextBlock,
  type TBlockKind,
} from '@/next/features/chat/handoff';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/handoff.css';

/** A text longer than this many lines or characters starts folded. */
const FOLD_LINES = 10;
const FOLD_CHARS = 900;

const TITLE: Record<TBlockKind, string> = {
  [BLOCK_KIND.handoff]: 'next.handoff.card.handoff',
  [BLOCK_KIND.fork]: 'next.handoff.card.fork',
  [BLOCK_KIND.result]: 'next.handoff.card.result',
} as const;

const ICON: Record<TBlockKind, string> = {
  [BLOCK_KIND.handoff]: 'arrow-left-right',
  [BLOCK_KIND.fork]: 'git-fork',
  [BLOCK_KIND.result]: 'git-merge',
} as const;

const MODE_LABEL = {
  [BLOCK_MODE.excerpt]: 'next.handoff.mode.excerpt',
  [BLOCK_MODE.note]: 'next.handoff.mode.note',
} as const;

/** «Выдержка · Claude · claude-opus-5-5» without the parts the block does not say. */
const lineOf = (block: IContextBlock): string =>
  [
    block.mode ? t(MODE_LABEL[block.mode]) : null,
    block.agent ? t(`next.agent.${block.agent}`) : null,
    block.model,
  ]
    .filter((part): part is string => part !== null && part.length > 0)
    .join(' · ');

export interface IContextCardProps {
  block: IContextBlock;
  /** How far the message got while it goes out (the bubble's own line); absent once it is in the transcript. */
  state?: ReactNode;
}

export const ContextCard = memo(({ block, state }: IContextCardProps) => {
  const links = useContext(ChatLinks);
  const [open, setOpen] = useState(false);
  const long = block.text.split('\n').length > FOLD_LINES || block.text.length > FOLD_CHARS;
  const title = block.title || t('next.shellData.untitled');
  const line = lineOf(block);
  const canOpen = links?.has(block.fromId) ?? false;
  return (
    <div className="hx-msg" data-kind="context">
      <div className="hx-ctx" data-kind={block.kind}>
        <div className="hx-ctx__head">
          <Icon name={ICON[block.kind]} size="sm" className="hx-ctx__ic" />
          <span className="hx-ctx__title">{t(TITLE[block.kind], { title })}</span>
          {line && <span className="hx-ctx__line">{line}</span>}
        </div>
        <div className="hx-ctx__body" data-open={open || !long || undefined}>
          <div className="hx-ctx__text">
            <Md md={block.text} paths />
          </div>
        </div>
        <div className="hx-ctx__foot">
          {long && (
            <button type="button" className="hx-ctx__act" onClick={() => setOpen((value) => !value)}>
              {open ? t('next.message.pastedLess') : t('next.message.pastedMore')}
            </button>
          )}
          <span className="hx-spacer" />
          {canOpen && links && (
            <button type="button" className="hx-ctx__act" onClick={() => links.open(block.fromId)}>
              <Icon name="external-link" size="sm" />
              {t('next.handoff.openSource')}
            </button>
          )}
        </div>
        {state}
      </div>
    </div>
  );
});
