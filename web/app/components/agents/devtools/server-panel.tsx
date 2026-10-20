import { useState, type ReactNode } from 'react';
import type { IServerLog } from '@/lib/hl-preview/devtools/types';
import { useServerLog } from '@/hooks/use-preview-devtools';
import { useStickToBottom } from '@/components/agents/devtools/use-stick-to-bottom';
import { t } from '@/lib/i18n';

const sourceText = (source: NonNullable<IServerLog['source']>): string =>
  source.kind === 'tmux'
    ? `tmux ${source.session}:${source.pane} · ${source.command}`
    : `${source.path} · ${source.command}`;

const emptyText = (filtered: boolean, all: boolean): string => {
  if (filtered) return t('devtools.serverPanel.noMatch');
  return all ? t('devtools.serverPanel.nothingYet') : t('devtools.serverPanel.noProblems');
};

interface IServerPanelProps {
  slot: string;
  machineId?: string;
  query: string;
  search: ReactNode;
}

/** The dev server's terminal: only the lines that look like problems, or all of it on request. */
export const ServerPanel = ({ slot, machineId, query, search }: IServerPanelProps) => {
  const [all, setAll] = useState(false);
  const { log, error } = useServerLog({ slot, machineId, active: true, all });
  const needle = query.trim().toLowerCase();
  const lines = (log?.lines ?? []).filter((line) => !needle || line.text.toLowerCase().includes(needle));
  const scrollRef = useStickToBottom<HTMLDivElement>(`${all}:${lines.length}:${lines.at(-1)?.text ?? ''}`);

  return (
    <div className="tw-devtools-server">
      <div className="tw-devtools-filters">
        <button
          type="button"
          className="tw-devtools-filter"
          aria-pressed={!all}
          onClick={() => setAll(false)}
        >
          {t('devtools.serverPanel.problems')}
        </button>
        <button type="button" className="tw-devtools-filter" aria-pressed={all} onClick={() => setAll(true)}>
          {t('devtools.serverPanel.showAll')}
        </button>
        {search}
      </div>
      {log?.source && (
        <p className="tw-devtools-server-source" title={sourceText(log.source)}>
          {sourceText(log.source)}
        </p>
      )}
      <div ref={scrollRef} className="tw-devtools-scroll">
        {error && (
          <p className="tw-devtools-empty tw-devtools-danger" role="alert">
            {error}
          </p>
        )}
        {!error && log === null && <p className="tw-devtools-empty">{t('devtools.serverPanel.reading')}</p>}
        {!error && log && !log.source && (
          <p className="tw-devtools-empty">{log.reason ?? t('devtools.serverPanel.notFound')}</p>
        )}
        {!error && log?.source && lines.length === 0 && (
          <p className="tw-devtools-empty">{emptyText(needle !== '', all)}</p>
        )}
        {lines.length > 0 && (
          <pre className="tw-devtools-terminal" aria-label={t('devtools.serverPanel.output')}>
            {lines.map((line, index) => (
              <span key={index} className="tw-devtools-terminal-line" data-level={line.level}>
                {line.text}
                {'\n'}
              </span>
            ))}
          </pre>
        )}
      </div>
    </div>
  );
};
