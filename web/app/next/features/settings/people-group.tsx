// «Люди» (Settings → General): the people who share this machine and who sits at this browser (features/people). The
// list is the machine's (PersonSettings.people), the choice of this browser is its own (stores/whoami.ts): two people on
// one account each name themselves on their devices. Another person's projects and chats then fold under their name in
// the sidebar, and what a chat answers is news only for the one who wrote to it last.
import { useId, useState } from 'react';
import { FormError, Group, MenuRow, Row, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';

/** The longest name and how many people, as the runtime keeps them (internal/wire: PersonNameMax, PeopleMax). */
export const PERSON_NAME_MAX = 40;
export const PEOPLE_MAX = 8;

interface IPeopleGroupProps {
  people: readonly string[];
  /** The name this browser gave its person, null when none. */
  me: string | null;
  busy: boolean;
  onMe: (name: string | null) => void;
  onAdd: (name: string) => void;
  onRemove: (name: string) => void;
}

export const PeopleGroup = ({ people, me, busy, onMe, onAdd, onRemove }: IPeopleGroupProps) => {
  const id = useId();
  const [text, setText] = useState('');
  const [wrong, setWrong] = useState<string | null>(null);

  const submit = () => {
    const name = text.trim().replace(/\s+/g, ' ');
    if (name === '') return;
    if ([...name].length > PERSON_NAME_MAX) return setWrong(t('next.people.tooLong', { n: PERSON_NAME_MAX }));
    if (people.includes(name)) return setWrong(t('next.people.twice'));
    if (people.length >= PEOPLE_MAX) return setWrong(t('next.people.tooMany', { n: PEOPLE_MAX }));
    onAdd(name);
    setText('');
  };

  return (
    <Group title={t('next.people.title')} foot={t('next.people.foot')}>
      {people.length > 0 && (
        <MenuRow
          label={t('next.people.me')}
          hint={t('next.people.meHint')}
          value={me !== null && people.includes(me) ? me : ''}
          choices={[
            { value: '', label: t('next.people.nobody'), sub: null },
            ...people.map((name) => ({ value: name, label: name, sub: null })),
          ]}
          onPick={(value) => onMe(typeof value === 'string' && value !== '' ? value : null)}
        />
      )}
      {people.map((name) => (
        <Row key={name} type="link" label={name} hint={name === me ? t('next.people.itsMe') : null}>
          <SetButton tone="ghost" disabled={busy} onClick={() => onRemove(name)}>
            {t('next.people.remove')}
          </SetButton>
        </Row>
      ))}
      <form
        className="hx-key-form hx-voice-words"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <label className="hx-voice-words__field" htmlFor={id}>
          <span className="hx-nb__label">{t('next.people.add.label')}</span>
          <input
            id={id}
            className="hx-set__input"
            value={text}
            autoComplete="off"
            maxLength={PERSON_NAME_MAX + 10}
            placeholder={t('next.people.add.placeholder')}
            aria-invalid={wrong !== null || undefined}
            onChange={(e) => {
              setText(e.target.value);
              setWrong(null);
            }}
          />
        </label>
        {wrong && <FormError>{wrong}</FormError>}
        <div className="hx-key-form__row">
          <SetButton tone="primary" disabled={busy || text.trim() === ''} onClick={submit}>
            {t('next.people.add')}
          </SetButton>
        </div>
      </form>
    </Group>
  );
};

export default PeopleGroup;
