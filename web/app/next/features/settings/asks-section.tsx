// Settings → Notifications → «Questions to people»: whether agents may ask, through which chat, how long they wait,
// how often they may ask, the quiet hours, what the message tells about the chat, and the people agents may tag (who
// may approve, which one is the person themselves). Every change goes to the machine whole and shows once it answers.
import { useId, useState } from 'react';
import type { IAskBot, IAskContact, IAskSettings } from '@/next/features/asks/api';
import { FormError, Group, MenuRow, Note, SegRow, SetButton, SwitchRow } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { useLeavingList } from '@/next/ui/presence';
import Switch from '@/next/ui/switch';
import '@/next/styles/asks.css';

const WAIT_CHOICES = [15, 30, 60, 240] as const;
const MAX_CHOICES = [240, 1440, 4320] as const;
const RATE_CHOICES = [3, 10, 30] as const;

const NO_CONTACTS: readonly IAskContact[] = [];
const contactKey = (c: IAskContact) => c.id;
/** A removed person's row folds in this long (.hx-reveal in settings.css). */
const CONTACT_EXIT_MS = 320;

const minutesLabel = (min: number) =>
  min < 60 ? t('next.asks.set.min', { n: min }) : t('next.asks.set.hours', { n: min / 60 });

export interface IAsksSectionProps {
  settings: IAskSettings | null;
  bots: readonly IAskBot[];
  failed: { reason: string; onRetry: () => void } | null;
  /** The field being saved ('' for none). */
  saving: string;
  /** Saves the whole settings; resolves to an error's words or null. */
  onSave: (next: IAskSettings, field: string) => Promise<string | null>;
}

const ContactRow = ({
  contact,
  busy,
  onChange,
  onRemove,
}: {
  contact: IAskContact;
  busy: boolean;
  onChange: (next: IAskContact) => void;
  onRemove: () => void;
}) => (
  <div className="hx-askp">
    <div className="hx-askp__text">
      <span className="hx-askp__name">{contact.name}</span>
      <span className="hx-askp__user">
        {contact.username ? `@${contact.username}` : `id ${contact.telegramId}`}
      </span>
      {contact.note && <span className="hx-askp__note">{contact.note}</span>}
    </div>
    <label className="hx-askp__flag">
      <Switch
        label={t('next.asks.set.canApprove')}
        checked={contact.canApprove}
        disabled={busy}
        onChange={(on) => onChange({ ...contact, canApprove: on })}
      />
      {t('next.asks.set.canApprove')}
    </label>
    <label className="hx-askp__flag">
      <Switch
        label={t('next.asks.set.me')}
        checked={contact.me}
        disabled={busy}
        onChange={(on) => onChange({ ...contact, me: on })}
      />
      {t('next.asks.set.me')}
    </label>
    <button
      type="button"
      className="hx-head-btn hx-askp__remove"
      aria-label={t('next.asks.set.remove', { name: contact.name })}
      disabled={busy}
      onClick={onRemove}
    >
      <Icon name="trash-2" size="sm" />
    </button>
  </div>
);

const AddContact = ({
  busy,
  onAdd,
}: {
  busy: boolean;
  onAdd: (c: IAskContact) => Promise<string | null>;
}) => {
  const ids = useId();
  const [name, setName] = useState('');
  const [username, setUsername] = useState('');
  const [note, setNote] = useState('');
  const [error, setError] = useState<string | null>(null);
  const add = () => {
    if (!name.trim() || !username.trim()) return;
    void onAdd({
      id: '',
      name: name.trim(),
      username: username.trim(),
      telegramId: 0,
      me: false,
      canApprove: false,
      note: note.trim(),
    }).then((err) => {
      setError(err);
      if (err === null) {
        setName('');
        setUsername('');
        setNote('');
      }
    });
  };
  return (
    <div className="hx-askp-add">
      <div className="hx-askp-add__fields">
        <label className="hx-askp-add__field" htmlFor={`${ids}-name`}>
          <span className="hx-nb__label">{t('next.asks.set.name')}</span>
          <input
            id={`${ids}-name`}
            className="hx-set__input"
            value={name}
            disabled={busy}
            placeholder={t('next.asks.set.namePlaceholder')}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label className="hx-askp-add__field" htmlFor={`${ids}-user`}>
          <span className="hx-nb__label">{t('next.asks.set.username')}</span>
          <input
            id={`${ids}-user`}
            className="hx-set__input"
            value={username}
            disabled={busy}
            placeholder="@username"
            onChange={(e) => setUsername(e.target.value)}
          />
        </label>
      </div>
      <label className="hx-askp-add__field" htmlFor={`${ids}-note`}>
        <span className="hx-nb__label">{t('next.asks.set.note')}</span>
        <input
          id={`${ids}-note`}
          className="hx-set__input"
          value={note}
          disabled={busy}
          placeholder={t('next.asks.set.notePlaceholder')}
          onChange={(e) => setNote(e.target.value)}
        />
      </label>
      {error && <FormError>{error}</FormError>}
      <div className="hx-preset__row">
        <SetButton
          tone="primary"
          icon="plus"
          busy={busy}
          disabled={!name.trim() || !username.trim()}
          onClick={add}
        >
          {t('next.asks.set.add')}
        </SetButton>
      </div>
    </div>
  );
};

const AsksSection = ({ settings, bots, failed, saving, onSave }: IAsksSectionProps) => {
  const [error, setError] = useState<string | null>(null);
  const contacts = useLeavingList(settings?.contacts ?? NO_CONTACTS, contactKey, CONTACT_EXIT_MS);
  if (failed)
    return (
      <Group title={t('next.asks.set.title')}>
        <LoadFailed
          size={LOAD_FAILED_SIZE.inline}
          title={t('next.asks.set.title')}
          reason={failed.reason}
          onRetry={failed.onRetry}
        />
      </Group>
    );
  if (!settings) return null;
  const save = (next: IAskSettings, field: string) =>
    onSave(next, field).then((err) => {
      setError(err);
      return err;
    });
  const bound = bots.filter((b) => b.bound);
  const botChoices = [
    { value: '', label: t('next.asks.set.firstBot'), sub: null },
    ...bound.map((b) => ({ value: b.id, label: b.name, sub: b.chat })),
  ];
  const off = !settings.enabled;
  const busy = (field: string) => saving === field;
  return (
    <>
      <Group title={t('next.asks.set.title')} foot={t('next.asks.set.foot')}>
        <SwitchRow
          label={t('next.asks.set.enabled')}
          hint={bound.length === 0 ? t('next.asks.set.noBot') : null}
          on={settings.enabled}
          busy={busy('enabled')}
          onChange={(on) => void save({ ...settings, enabled: on }, 'enabled')}
        />
        <MenuRow
          label={t('next.asks.set.defaultBot')}
          value={settings.defaultBot}
          choices={botChoices}
          busy={busy('defaultBot')}
          off={off}
          onPick={(value) => void save({ ...settings, defaultBot: String(value ?? '') }, 'defaultBot')}
        />
        <SegRow
          label={t('next.asks.set.wait')}
          value={settings.defaultTimeoutMin}
          choices={WAIT_CHOICES.map((n) => ({ value: n, label: minutesLabel(n) }))}
          busy={busy('wait')}
          off={off}
          onPick={(n) =>
            void save(
              { ...settings, defaultTimeoutMin: n, maxTimeoutMin: Math.max(n, settings.maxTimeoutMin) },
              'wait',
            )
          }
        />
        <SegRow
          label={t('next.asks.set.maxWait')}
          value={settings.maxTimeoutMin}
          choices={MAX_CHOICES.map((n) => ({ value: n, label: minutesLabel(n) }))}
          busy={busy('maxWait')}
          off={off}
          onPick={(n) =>
            void save(
              { ...settings, maxTimeoutMin: n, defaultTimeoutMin: Math.min(n, settings.defaultTimeoutMin) },
              'maxWait',
            )
          }
        />
        <SegRow
          label={t('next.asks.set.perHour')}
          hint={t('next.asks.set.perHourHint')}
          value={settings.perHour}
          choices={RATE_CHOICES.map((n) => ({ value: n, label: String(n) }))}
          busy={busy('perHour')}
          off={off}
          onPick={(n) => void save({ ...settings, perHour: n }, 'perHour')}
        />
        <SwitchRow
          label={t('next.asks.set.quiet', { from: settings.quiet.from, to: settings.quiet.to })}
          hint={t('next.asks.set.quietHint')}
          on={settings.quiet.enabled}
          busy={busy('quiet')}
          onChange={(on) => void save({ ...settings, quiet: { ...settings.quiet, enabled: on } }, 'quiet')}
        />
        <SwitchRow
          label={t('next.asks.set.showSource')}
          hint={t('next.asks.set.showSourceHint')}
          on={settings.showSource}
          busy={busy('showSource')}
          onChange={(on) => void save({ ...settings, showSource: on }, 'showSource')}
        />
        <SwitchRow
          label={t('next.asks.set.browser')}
          hint={t('next.asks.set.browserHint')}
          on={settings.browserNotify}
          busy={busy('browser')}
          onChange={(on) => void save({ ...settings, browserNotify: on }, 'browser')}
        />
      </Group>
      {error && <FormError>{error}</FormError>}
      <Group title={t('next.asks.set.people')} foot={t('next.asks.set.peopleFoot')}>
        <SwitchRow
          label={t('next.asks.set.onlyContacts')}
          hint={t('next.asks.set.onlyContactsHint')}
          on={settings.onlyContacts}
          busy={busy('onlyContacts')}
          onChange={(on) => void save({ ...settings, onlyContacts: on }, 'onlyContacts')}
        />
        {contacts.length === 0 && <Note>{t('next.asks.set.noPeople')}</Note>}
        {contacts.map(({ item, leaving }) => (
          <div key={item.id} className="hx-reveal" data-open={!leaving}>
            <div className="hx-reveal__in">
              <ContactRow
                contact={item}
                busy={busy(`contact:${item.id}`)}
                onChange={(next) =>
                  void save(
                    { ...settings, contacts: settings.contacts.map((c) => (c.id === item.id ? next : c)) },
                    `contact:${item.id}`,
                  )
                }
                onRemove={() =>
                  void save(
                    { ...settings, contacts: settings.contacts.filter((c) => c.id !== item.id) },
                    `contact:${item.id}`,
                  )
                }
              />
            </div>
          </div>
        ))}
        <AddContact
          busy={busy('add')}
          onAdd={(c) => save({ ...settings, contacts: [...settings.contacts, c] }, 'add')}
        />
      </Group>
    </>
  );
};

export default AsksSection;
