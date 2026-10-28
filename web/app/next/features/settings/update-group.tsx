// «Обновления» and «Справка» of «General». A machine a hub carries: whether its programs are the hub's latest, a button
// that checks and updates now, and — where it cannot replace its own programs (a system install, a container) — the
// command that does it. A standalone runtime: its version, the newer release, and how to update for each way of
// installing. «Справка» leads to the guides of the repository.
import { Group, LinkRow, Row, SetButton } from '@/next/features/settings/rows';
import type { IMachineUpdate, TUpdateInfo } from '@/next/features/update/machine-update';
import { UPDATE_GUIDE_URL } from '@/next/features/update/release';
import { t } from '@/next/i18n';

const DOCS = 'https://github.com/ly-solutions/harnessly/blob/main/docs';

/** The guides «Справка» lists, by the key of their label. */
const GUIDES = [
  { key: 'start', url: `${DOCS}/getting-started.md` },
  { key: 'update', url: UPDATE_GUIDE_URL },
  { key: 'machine', url: `${DOCS}/hub/machine.md` },
  { key: 'voice', url: `${DOCS}/voice.md` },
  { key: 'config', url: `${DOCS}/configuration.md` },
] as const;

/** The ways of installing a standalone runtime, in the order of docs/deploy.md#updates. */
const WAYS = ['binary', 'make', 'compose', 'docker', 'helm'] as const;

const open = (url: string) => window.open(url, '_blank', 'noopener,noreferrer');

export interface IUpdateGroupProps {
  info: TUpdateInfo;
  /** The button was pressed and the machine has not answered yet. */
  busy: boolean;
  onUpdate: () => void;
  onCopy: () => void;
}

const stateText = (u: IMachineUpdate, busy: boolean): string => {
  if (busy || u.applying) return t('next.settings.update.applying');
  if (u.error) return t('next.settings.update.failed', { error: u.error });
  if (!u.checked) return t('next.settings.update.unknown');
  if (u.upToDate) return t('next.settings.update.latest');
  return t('next.settings.update.available');
};

type THubUpdateProps = { update: IMachineUpdate } & Omit<IUpdateGroupProps, 'info'>;

const HubUpdate = ({ update, busy, onUpdate, onCopy }: THubUpdateProps) => (
  <Group
    title={t('next.settings.update.title')}
    foot={update.mode === 'auto' ? t('next.settings.update.autoFoot') : t('next.settings.update.manualFoot')}
  >
    <Row type="link" label={stateText(update, busy)} hint={null}>
      {update.mode === 'auto' && (
        <SetButton
          tone={update.checked && !update.upToDate ? 'primary' : 'ghost'}
          busy={busy || update.applying}
          disabled={busy || update.applying}
          onClick={onUpdate}
        >
          {update.checked && !update.upToDate
            ? t('next.settings.update.now')
            : t('next.settings.update.check')}
        </SetButton>
      )}
    </Row>
    {update.mode === 'manual' && (
      <Row type="link" label={t('next.settings.update.command')} hint={update.command}>
        <SetButton tone="ghost" icon="copy" onClick={onCopy}>
          {t('next.settings.update.copy')}
        </SetButton>
      </Row>
    )}
  </Group>
);

const StandaloneUpdate = ({ current, latest, url }: Extract<TUpdateInfo, { kind: 'standalone' }>) => (
  <Group title={t('next.settings.update.title')} foot={t('next.settings.update.standaloneFoot')}>
    <Row
      type="link"
      label={
        latest
          ? t('next.settings.update.newer', { version: latest })
          : t('next.settings.update.version', { version: current })
      }
      hint={latest ? t('next.settings.update.version', { version: current }) : null}
    >
      {latest && url && (
        <SetButton tone="primary" onClick={() => open(url)}>
          {t('next.settings.update.notes')}
        </SetButton>
      )}
    </Row>
    {WAYS.map((way) => (
      <Row
        key={way}
        type="link"
        label={t(`next.settings.update.way.${way}`)}
        hint={t(`next.settings.update.way.${way}.how`)}
      >
        {null}
      </Row>
    ))}
  </Group>
);

const UpdateGroup = ({ info, busy, onUpdate, onCopy }: IUpdateGroupProps) => (
  <>
    {info.kind === 'hub' ? (
      <HubUpdate update={info.machine} busy={busy} onUpdate={onUpdate} onCopy={onCopy} />
    ) : (
      <StandaloneUpdate {...info} />
    )}
    <Group title={t('next.settings.help.title')}>
      {GUIDES.map((g) => (
        <LinkRow
          key={g.key}
          label={t(`next.settings.help.${g.key}`)}
          hint={t(`next.settings.help.${g.key}.hint`)}
          action={t('next.settings.help.open')}
          onAction={() => open(g.url)}
        />
      ))}
    </Group>
  </>
);

export default UpdateGroup;
