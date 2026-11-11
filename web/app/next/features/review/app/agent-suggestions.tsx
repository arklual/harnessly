// What the agent suggested in «Пройти ревью», as a layer of its own over the diff («предложено агентом»): a card under
// the lines it is about, with the code it would put there, taken into the person's draft, applied, or thrown away. And
// the panel's part: the button that asks for the pass, what is awaited, and the list to walk the suggestions by.
import { useLayoutEffect, useMemo, useState, useSyncExternalStore } from 'react';
import { createPortal } from 'react-dom';
import type { IAgentSuggestion } from '@/next/features/review/app/agent-review';
import { LAYER_OWNER, type ILayerMarks, type NotesLayer } from '@/next/features/review/app/notes-layer';
import { docsOf, EXIT_MS, linesLabel } from '@/next/features/review/app/review-notes';
import { AGENT_STATE, type IAgentReviewApi } from '@/next/features/review/app/use-agent-review';
import { LINE_SIDE, type IFileContent, type IReviewFile, type TLineSide } from '@/next/features/review/model';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { useLeavingList } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

export interface ISuggestionActions {
  /** Into the person's draft: a remark for the agent, or a draft note of the merge request. */
  take?: (s: IAgentSuggestion) => Promise<boolean>;
  /** The replacement written into the file (the person's own work). */
  apply?: (s: IAgentSuggestion) => Promise<boolean>;
  takeLabel: (s: IAgentSuggestion) => string;
}

const KEY = 'ag:';

const SuggestionCard = ({
  s,
  api,
  actions,
  leaving,
}: {
  s: IAgentSuggestion;
  api: IAgentReviewApi;
  actions: ISuggestionActions;
  leaving: boolean;
}) => {
  const [busy, setBusy] = useState<'take' | 'apply' | null>(null);
  const run = async (which: 'take' | 'apply', job: (s: IAgentSuggestion) => Promise<boolean>) => {
    setBusy(which);
    const ok = await job(s);
    setBusy(null);
    if (ok) api.settle(s.id);
    else toast(t('next.reviewApp.agent.failed'), { tone: 'error' });
  };
  return (
    <div className="hx-rvnote hx-rvagent" data-state={leaving ? 'closing' : 'open'}>
      <div className="hx-rvnote__head">
        <Icon name="sparkles" size="sm" />
        <span className="hx-rvnote__meta">
          <span className="hx-rvnote__who">{t('next.reviewApp.agent.by')}</span> · {linesLabel(s.from, s.to)}
        </span>
      </div>
      <p className="hx-rvnote__text">{s.text}</p>
      {s.replacement !== undefined && (
        <pre className="hx-rvagent__code" aria-label={t('next.reviewApp.agent.code')}>
          {s.replacement}
        </pre>
      )}
      <div className="hx-rvnote__actions">
        <Button size="sm" variant="ghost" disabled={busy !== null} onClick={() => api.settle(s.id)}>
          {t('next.reviewApp.agent.drop')}
        </Button>
        {actions.take && (
          <Button
            size="sm"
            icon="message-square"
            busy={busy === 'take'}
            disabled={busy !== null}
            onClick={() => actions.take && void run('take', actions.take)}
          >
            {actions.takeLabel(s)}
          </Button>
        )}
        {actions.apply && s.replacement !== undefined && (
          <Button
            size="sm"
            variant="primary"
            icon="check"
            busy={busy === 'apply'}
            disabled={busy !== null}
            onClick={() => actions.apply && void run('apply', actions.apply)}
          >
            {t('next.reviewApp.agent.apply')}
          </Button>
        )}
      </div>
    </div>
  );
};

interface IDiffSuggestionsProps {
  layer: NotesLayer;
  api: IAgentReviewApi;
  actions: ISuggestionActions;
  file: IReviewFile;
  content: IFileContent;
  sides: readonly TLineSide[];
}

export const DiffSuggestions = ({ layer, api, actions, file, content, sides }: IDiffSuggestionsProps) => {
  const slots = useSyncExternalStore(layer.subscribe, layer.getSlots, layer.getSlots);
  const docs = useMemo(() => docsOf(content), [content]);
  const mine = useMemo(
    () =>
      api.suggestions.filter(
        (s) => s.path === file.path && sides.includes(s.side) && s.to <= Math.max(1, docs[s.side].length),
      ),
    [api.suggestions, file.path, sides, docs],
  );
  const shown = useLeavingList(mine, (s) => s.id, EXIT_MS);

  useLayoutEffect(() => {
    for (const side of [LINE_SIDE.after, LINE_SIDE.before]) {
      const here = shown.filter(({ item }) => item.side === side);
      const marks: ILayerMarks = {
        cards: here.map(({ item }) => ({ key: KEY + item.id, line: item.to })),
        ranges: here.map(({ item }) => ({ from: item.from, to: item.to, kind: 'agent' as const })),
      };
      layer.set(side, marks, LAYER_OWNER.agent);
    }
  }, [layer, shown]);
  useLayoutEffect(
    () => () => {
      for (const side of [LINE_SIDE.after, LINE_SIDE.before])
        layer.set(side, { cards: [], ranges: [] }, LAYER_OWNER.agent);
    },
    [layer],
  );

  return (
    <>
      {slots.map((slot) => {
        if (!slot.key.startsWith(KEY)) return null;
        const entry = shown.find(({ item }) => KEY + item.id === slot.key && item.side === slot.side);
        return entry
          ? createPortal(
              <SuggestionCard s={entry.item} api={api} actions={actions} leaving={entry.leaving} />,
              slot.el,
              `${slot.side}:${slot.key}`,
            )
          : null;
      })}
    </>
  );
};

// ── the panel ──

interface IAgentPanelProps {
  api: IAgentReviewApi;
  files: readonly IReviewFile[];
  onAsk: () => void;
  onOpen: (file: IReviewFile, line: number) => void;
}

export const AgentPanel = ({ api, files, onAsk, onOpen }: IAgentPanelProps) => {
  const asking = api.state === AGENT_STATE.asking;
  const sorted = [...api.suggestions].sort((a, b) => a.path.localeCompare(b.path) || a.from - b.from);
  return (
    <section className="hx-rvnotes hx-rvagent-panel" aria-label={t('next.reviewApp.agent.title')}>
      <div className="hx-rvagent-panel__head">
        <h3 className="hx-rvnotes__title">
          {t('next.reviewApp.agent.title')}{' '}
          {sorted.length > 0 && <span className="hx-rvnotes__count">{sorted.length}</span>}
        </h3>
        <Button size="sm" icon="sparkles" busy={asking} disabled={asking} onClick={onAsk}>
          {t('next.reviewApp.agent.ask')}
        </Button>
      </div>
      {asking && <p className="hx-rvagent-panel__note">{t('next.reviewApp.agent.asking')}</p>}
      {!asking && api.state === AGENT_STATE.failed && (
        <p className="hx-rvagent-panel__note">{t('next.reviewApp.agent.unread')}</p>
      )}
      {!asking && api.done && (
        <p className="hx-rvagent-panel__note">{t('next.reviewApp.agent.allSettled')}</p>
      )}
      {sorted.length > 0 && (
        <ol className="hx-rvnotes__list">
          {sorted.map((s) => {
            const file = files.find((f) => f.path === s.path);
            return (
              <li key={s.id}>
                <button
                  type="button"
                  className="hx-rvnotes__row"
                  disabled={!file}
                  onClick={() => file && onOpen(file, s.from)}
                >
                  <span className="hx-rvnotes__where">
                    <Icon name="sparkles" size="sm" />
                    <span className="hx-rvnotes__path">{s.path.split('/').at(-1)}</span>
                    <span>{linesLabel(s.from, s.to)}</span>
                  </span>
                  <span className="hx-rvnotes__text">{s.text}</span>
                </button>
              </li>
            );
          })}
        </ol>
      )}
    </section>
  );
};
