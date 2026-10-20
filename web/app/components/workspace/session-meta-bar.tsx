// What the composer's session chip opens: the model, the context in use and where the transcript lives.
import type { ISessionMeta } from '@/hooks/use-tab-store';
import { t } from '@/lib/i18n';

const tokens = (n: number | null | undefined) =>
  typeof n === 'number' ? (n >= 1000 ? `${Math.round(n / 1000)}k` : String(n)) : '—';

export function SessionMetaDetail({
  meta,
  sessionName,
  sessionId,
  jsonlPath,
}: {
  meta: ISessionMeta;
  sessionName?: string;
  sessionId: string | null;
  jsonlPath: string | null;
}) {
  const rows: [string, string][] = [
    [t('workspace.sessionMetaBar.model'), String(meta.model ?? '—')],
    [t('workspace.sessionMetaBar.effort'), String(meta.effort ?? '—')],
    [
      t('workspace.sessionMetaBar.context'),
      `${t('workspace.sessionMetaBar.contextOf', { used: tokens(meta.currentContextTokens), total: tokens(meta.contextWindowSize) })}${typeof meta.usedPercentage === 'number' ? ` · ${Math.round(meta.usedPercentage)}%` : ''}`,
    ],
    [t('workspace.sessionMetaBar.session'), sessionId ?? '—'],
    [t('workspace.sessionMetaBar.terminal'), sessionName ?? '—'],
    [t('workspace.sessionMetaBar.transcript'), jsonlPath ?? '—'],
  ];
  return (
    <dl className="tw-session-meta">
      {rows.map(([k, v]) => (
        <div key={k} className="tw-session-meta-row">
          <dt>{k}</dt>
          <dd title={v}>{v}</dd>
        </div>
      ))}
    </dl>
  );
}
