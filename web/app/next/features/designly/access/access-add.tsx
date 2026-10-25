// The one field of the share dialog: «Добавьте людей по имени или почте» with the role (and its term, under «Доступ
// до…») at the field's right end; Enter or «Пригласить» gives it. The suggestions are the people search's.
import { useState, type RefObject } from 'react';
import { EMAIL, putPerson, type TGrantRole, type TTtlValue } from '@/next/features/designly/access/api';
import { grantRoles } from '@/next/features/designly/access/labels';
import PersonField from '@/next/features/designly/access/person-field';
import RoleMenu from '@/next/features/designly/access/role-menu';
import { change, isManager, type ISectionProps } from '@/next/features/designly/access/section';
import { usePresence } from '@/next/ui/presence';
import { usePhone } from '@/next/ui/use-media';
import { t } from '@/next/i18n';

const SEND_OUT_MS = 180;

interface IAccessAddProps extends ISectionProps {
  /** The field, for the dialog's first focus on a wide window. */
  inputRef?: RefObject<HTMLInputElement | null>;
}

const AccessAdd = (props: IAccessAddProps) => {
  const { project, access, action } = props;
  const [who, setWho] = useState('');
  const [role, setRole] = useState<TGrantRole>('view');
  const [ttl, setTtl] = useState<TTtlValue>(null);
  const [bad, setBad] = useState(false);
  const idle = action.busy === null;
  // The phone's field is narrow: the short words fit beside the role.
  const placeholder = t(usePhone() ? 'next.access.addPlaceholder' : 'next.access.addField');
  // «Пригласить» comes in once there is someone to invite: the field has the whole width until then.
  const send = usePresence(who.trim() === '' ? null : true, SEND_OUT_MS);

  const add = () => {
    const email = who.trim().toLowerCase();
    if (!EMAIL.test(email)) {
      setBad(true);
      return;
    }
    setBad(false);
    change(props, 'add', async () => {
      const next = await putPerson(project, email, role, ttl);
      setWho('');
      setTtl(null);
      return next;
    });
  };

  return (
    <form
      className="hx-acc-add"
      data-section="add"
      onSubmit={(event) => {
        event.preventDefault();
        add();
      }}
    >
      <div className="hx-acc-field" data-bad={bad || undefined}>
        <PersonField
          bare
          inputRef={props.inputRef}
          label={t('next.access.addField')}
          placeholder={placeholder}
          value={who}
          disabled={!idle}
          onChange={(next) => {
            setBad(false);
            setWho(next);
          }}
        />
        <RoleMenu
          label={t('next.access.newRole')}
          role={role}
          roles={grantRoles(isManager(access))}
          onRole={setRole}
          term={{ value: ttl, onChange: setTtl }}
          showTerm
          disabled={!idle}
        />
      </div>
      {send.value && (
        <button
          type="submit"
          className="hx-btn hx-acc-add__go"
          data-variant="primary"
          data-state={send.state}
          data-busy={action.busy === 'add'}
          disabled={!idle}
        >
          <span>{t('next.access.invite')}</span>
          <span className="hx-gate__spin" aria-hidden="true" />
        </button>
      )}
      {bad && (
        <p className="hx-create__err hx-acc-add__err" role="alert">
          {t('next.access.needEmail')}
        </p>
      )}
    </form>
  );
};

export default AccessAdd;
