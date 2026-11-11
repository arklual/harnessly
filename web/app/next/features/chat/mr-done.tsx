// The chat's merge requests under a finished task: when the agent is done and the chat opened or changed merge requests
// (pull requests), one block lists them, each with «Ревью» (the change in the right pane) and «Открыть в «Ревью кода»»
// (the whole screen). It leaves, with its exit, when a new task starts. The person turns it off in Settings → General.
import type { FeedItem, TurnOutput } from '@/next/features/chat/types';
import { Button, RoundButton } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';

/** The block's exit, as long as hx-mrdone-out in chat.css. */
const EXIT_MS = 180;

/** A merge request of the block; `from` names the subchat or subagent that opened it, empty for the chat's own. */
export interface IDoneMr {
  output: TurnOutput;
  from: string;
}

/**
 * Every merge request the chat's turns made, once each, the latest name kept, in the order they first came; then those
 * its subchats and subagents made (the chat inherits them), each once and not again when the chat itself has it.
 */
export const chatMrs = (items: readonly FeedItem[], inherited: readonly IDoneMr[] = []): IDoneMr[] => {
  const byRef = new Map<string, IDoneMr>();
  for (const item of items) {
    if (item.kind !== 'turn') continue;
    for (const output of item.turn.outputs ?? [])
      if (output.kind === 'mr') byRef.set(output.ref, { output, from: '' });
  }
  for (const mr of inherited) if (!byRef.has(mr.output.ref)) byRef.set(mr.output.ref, mr);
  return [...byRef.values()];
};

/** «acme/runtime !12» or «acme/runtime #12» of a merge or pull request link; the link itself when it is neither. */
export const mrPlace = (url: string): string => {
  const mr = /^https?:\/\/[^/]+\/(.+?)\/-\/merge_requests\/(\d+)/.exec(url);
  if (mr) return `${mr[1]} !${mr[2]}`;
  const pr = /^https?:\/\/[^/]+\/([^/]+\/[^/]+)\/pull\/(\d+)/.exec(url);
  if (pr) return `${pr[1]} #${pr[2]}`;
  return url;
};

interface IMrDoneProps {
  /** The chat's merge requests; null while a task runs (the block leaves). */
  mrs: readonly IDoneMr[] | null;
  /** The change in the right pane. */
  onOpen: (output: TurnOutput) => void;
  /** The change in the «Ревью кода» app. */
  onReview?: (url: string) => void;
}

const MrDone = ({ mrs, onOpen, onReview }: IMrDoneProps) => {
  const shown = usePresence(mrs !== null && mrs.length > 0 ? mrs : null, EXIT_MS);
  if (shown.value === null) return null;
  const list = shown.value;
  return (
    <section
      className="hx-mrdone"
      data-state={shown.state}
      inert={shown.state === PRESENCE.closing || undefined}
      aria-label={t('next.mrDone.title')}
    >
      <h3 className="hx-mrdone__title">
        <Icon name="git-pull-request" size="sm" />
        {list.length === 1 ? t('next.mrDone.one') : t('next.mrDone.many', { n: list.length })}
      </h3>
      <ul className="hx-mrdone__list">
        {list.map(({ output: mr, from }) => (
          <li key={mr.ref} className="hx-mrdone__row">
            <span className="hx-mrdone__text">
              <span className="hx-mrdone__name">{mr.title}</span>
              <span className="hx-mrdone__place">{[mrPlace(mr.ref), from].filter(Boolean).join(' · ')}</span>
            </span>
            <Button size="sm" icon="git-compare" onClick={() => onOpen(mr)}>
              {t('next.mrDone.review')}
            </Button>
            {onReview && (
              <RoundButton
                icon="maximize-2"
                label={t('next.mrDone.full')}
                size="sm"
                variant="ghost"
                onClick={() => onReview(mr.ref)}
              />
            )}
          </li>
        ))}
      </ul>
    </section>
  );
};

export default MrDone;
