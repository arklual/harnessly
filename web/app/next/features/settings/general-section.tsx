// «General»: how the app looks and how the field sends, and the account. The theme lives only here (the sidebar has no
// switch of its own) and is this browser's; the text size and
// the Enter key are the person's settings and follow them to every device.
import { useEffect, useState } from 'react';
import { Group, LinkRow, MenuRow, SegRow, SwitchRow } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { PersonSettings } from '@/next/stores/me';
import type { TShownKind, TShownKinds } from '@/next/features/chat/outputs';
import {
  DELEGATION_MAX_CHILDREN,
  IDLE_STOPS,
  type DelegationSettings,
  type IdleStop,
  type OnOff,
} from '@/next/protocol/v2';

/** The kinds of «Что показывать в ленте», in the order the rows go. */
export const FEED_ROWS = [
  'artifact',
  'design',
  'doc',
  'mr',
  'stand',
  'file',
] as const satisfies readonly TShownKind[];

export const THEMES = ['light', 'dark', 'system'] as const;
export type ThemeChoice = (typeof THEMES)[number];
const THEME_ICON = { light: 'sun', dark: 'moon', system: 'monitor' } as const satisfies Record<
  ThemeChoice,
  string
>;
export const TEXT_SIZES = ['s', 'm', 'l'] as const satisfies readonly PersonSettings['textSize'][];

/** «Really sign out?» stands this long, then goes back to the plain link. */
const ASK_MS = 3000;

export interface IGeneralSectionProps {
  theme: ThemeChoice;
  textSize: PersonSettings['textSize'];
  enterSends: boolean;
  /** The tabs over the window are on (this browser's, as the theme). */
  topTabs: boolean;
  /** The pane opens by itself what the agent works on: a design, a document, a board (this browser's). */
  paneAuto: boolean;
  /** Which things a turn made get a card under its answer (this browser's). */
  feedKinds: TShownKinds;
  /** The language of the page; it is the installation's, so the row only shows it. */
  language: 'ru' | 'en';
  account: {
    name: string;
    /** The line under the name: the e-mail, or how the person gets in. */
    hint: string | null;
    /** The runtime signs the person in with a password, so there is a session to end. */
    canSignOut: boolean;
  };
  /** When the runtime stops a terminal that stands idle; null when the machine cannot, so the row is not there. */
  idleStop: IdleStop | null;
  /** Whether a chat held on its usage limit goes on by itself at the reset; null when the runtime has no such switch. */
  limitContinue: OnOff | null;
  /** Whether a chat a restart cut short goes on by itself; null when the runtime has no such switch. */
  restartContinue: OnOff | null;
  /** What the machine's agents may delegate; null when the machine cannot say, so the group is not there. */
  delegation: DelegationSettings | null;
  /** The keys of the rows waiting for the machine. */
  busy: ReadonlySet<string>;
  onTheme: (theme: ThemeChoice) => void;
  onTextSize: (size: PersonSettings['textSize']) => void;
  onEnterSends: (on: boolean) => void;
  onTopTabs: (on: boolean) => void;
  onPaneAuto: (on: boolean) => void;
  onFeedKind: (kind: TShownKind, on: boolean) => void;
  onIdleStop: (value: IdleStop) => void;
  onLimitContinue: (on: boolean) => void;
  onRestartContinue: (on: boolean) => void;
  onDelegation: (value: DelegationSettings) => void;
  onSignOut: () => void;
}

const SignOutRow = ({ account, onSignOut }: Pick<IGeneralSectionProps, 'account' | 'onSignOut'>) => {
  const [asking, setAsking] = useState(false);
  useEffect(() => {
    if (!asking) return;
    const timer = setTimeout(() => setAsking(false), ASK_MS);
    return () => clearTimeout(timer);
  }, [asking]);
  if (!account.canSignOut)
    return (
      <div className="hx-set__row" data-type="link">
        <div className="hx-set__row-text">
          <span className="hx-set__row-label">{account.name}</span>
          {account.hint && <span className="hx-set__row-hint">{account.hint}</span>}
        </div>
      </div>
    );
  return (
    <LinkRow
      label={account.name}
      hint={account.hint}
      action={asking ? t('next.settings.general.signOutSure') : t('next.settings.general.signOut')}
      onAction={() => (asking ? onSignOut() : setAsking(true))}
    />
  );
};

const GeneralSection = ({
  theme,
  textSize,
  enterSends,
  topTabs,
  paneAuto,
  feedKinds,
  language,
  account,
  idleStop,
  limitContinue,
  restartContinue,
  busy,
  onTheme,
  onTextSize,
  onEnterSends,
  onTopTabs,
  onPaneAuto,
  onFeedKind,
  onIdleStop,
  onLimitContinue,
  onRestartContinue,
  delegation,
  onDelegation,
  onSignOut,
}: IGeneralSectionProps) => (
  <>
    <Group title={t('next.settings.general.look')}>
      <SegRow
        label={t('next.settings.general.theme')}
        hint={t('next.settings.general.theme.hint')}
        value={theme}
        choices={THEMES.map((value) => ({
          value,
          label: t(`next.settings.general.theme.${value}`),
          icon: THEME_ICON[value],
        }))}
        onPick={onTheme}
      />
      <SegRow
        label={t('next.settings.general.textSize')}
        hint={t('next.settings.general.textSize.hint')}
        value={textSize}
        choices={TEXT_SIZES.map((value) => ({ value, label: t(`next.settings.general.textSize.${value}`) }))}
        busy={busy.has('textSize')}
        onPick={onTextSize}
      />
      <SwitchRow
        label={t('next.settings.general.topTabs')}
        hint={t('next.settings.general.topTabs.hint')}
        on={topTabs}
        onChange={onTopTabs}
      />
      <SwitchRow
        label={t('next.settings.general.paneAuto')}
        hint={t('next.settings.general.paneAuto.hint')}
        on={paneAuto}
        onChange={onPaneAuto}
      />
      <MenuRow
        label={t('next.settings.general.language')}
        hint={t('next.settings.general.language.hint')}
        value={language}
        choices={[
          { value: 'ru', label: 'Русский', sub: null },
          { value: 'en', label: 'English', sub: null },
        ]}
        off
        onPick={() => undefined}
      />
    </Group>
    <Group title={t('next.settings.general.work')}>
      <MenuRow
        label={t('next.settings.general.editor')}
        hint={t('next.settings.general.editor.hint')}
        value={null}
        choices={[]}
        off
        onPick={() => undefined}
      />
      <SwitchRow
        label={t('next.settings.general.enter')}
        hint={t('next.settings.general.enter.hint')}
        on={enterSends}
        busy={busy.has('enterSends')}
        onChange={onEnterSends}
      />
      {idleStop !== null && (
        <SegRow
          label={t('next.settings.general.idleStop')}
          hint={t('next.settings.general.idleStop.hint')}
          value={idleStop}
          choices={IDLE_STOPS.map((value) => ({
            value,
            label: t(`next.settings.general.idleStop.${value}`),
          }))}
          busy={busy.has('idleStop')}
          onPick={onIdleStop}
        />
      )}
      {limitContinue !== null && (
        <SwitchRow
          label={t('next.settings.general.limitContinue')}
          hint={t('next.settings.general.limitContinue.hint')}
          on={limitContinue === 'on'}
          busy={busy.has('limitContinue')}
          onChange={onLimitContinue}
        />
      )}
      {restartContinue !== null && (
        <SwitchRow
          label={t('next.settings.general.restartContinue')}
          hint={t('next.settings.general.restartContinue.hint')}
          on={restartContinue === 'on'}
          busy={busy.has('restartContinue')}
          onChange={onRestartContinue}
        />
      )}
    </Group>
    {delegation !== null && (
      <Group title={t('next.settings.delegation.title')}>
        <SegRow
          label={t('next.settings.delegation.maxChildren')}
          hint={t('next.settings.delegation.hint')}
          value={delegation.maxChildren}
          choices={DELEGATION_MAX_CHILDREN.map((value) => ({ value, label: String(value) }))}
          busy={busy.has('delegation')}
          onPick={(maxChildren) => onDelegation({ ...delegation, maxChildren })}
        />
        <SwitchRow
          label={t('next.settings.delegation.anyChat')}
          hint={t('next.settings.delegation.anyChatHint')}
          on={delegation.anyChat}
          busy={busy.has('delegation')}
          onChange={(anyChat) => onDelegation({ ...delegation, anyChat })}
        />
      </Group>
    )}
    <Group title={t('next.settings.general.feed')}>
      {FEED_ROWS.map((kind) => (
        <SwitchRow
          key={kind}
          label={t(`next.settings.general.feed.${kind}`)}
          hint={t(`next.settings.general.feed.${kind}.hint`)}
          on={feedKinds[kind]}
          onChange={(on) => onFeedKind(kind, on)}
        />
      ))}
    </Group>
    <Group title={t('next.settings.general.account')}>
      <SignOutRow account={account} onSignOut={onSignOut} />
    </Group>
  </>
);

export default GeneralSection;
