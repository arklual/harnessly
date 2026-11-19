// The description of a service of the person's own, written by hand on its page: the name, the group an app's
// `category` finds it in, what it is, what an agent can do with it and the steps to get its key. The text is kept in
// the person's language; the other language's text stays as the agent or an earlier edit left it. The connection itself
// (address, keys) is not changed here.
import { useId, useState, type ReactNode } from 'react';
import {
  MCP_KINDS,
  type IMcpServer,
  type IOwnDescription,
  type TMcpKind,
} from '@/next/features/connections/mcp-api';
import { FormError, SetButton } from '@/next/features/settings/rows';
import { locale, t } from '@/next/i18n';

const lines = (text: string): string[] =>
  text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '');

const Row = ({
  id,
  label,
  hint,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  children: ReactNode;
}) => (
  <div className="hx-conn-form__field">
    <label className="hx-conn-form__label" htmlFor={id}>
      {label}
    </label>
    {children}
    {hint !== undefined && <span className="hx-mcp-env__hint">{hint}</span>}
  </div>
);

interface IOwnDescribeProps {
  server: IMcpServer;
  onSave: (id: string, description: IOwnDescription) => Promise<string | null>;
}

/** «Edit the description»: folded until pressed; a refused save says why under the fields. */
const OwnDescribe = ({ server, onSave }: IOwnDescribeProps) => {
  const own = server.custom;
  const ru = locale() === 'ru';
  const ids = useId();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(server.name);
  const [category, setCategory] = useState<TMcpKind>(own?.category ?? 'utility');
  const [about, setAbout] = useState(own === null ? '' : ru ? own.aboutRu : own.about);
  const [can, setCan] = useState((own === null ? [] : ru ? own.canRu : own.can).join('\n'));
  const [steps, setSteps] = useState((own === null ? [] : ru ? own.stepsRu : own.steps).join('\n'));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  if (own === null) return null;
  if (!open)
    return (
      <div className="hx-own__edit">
        <SetButton tone="ghost" icon="pencil" onClick={() => setOpen(true)}>
          {t('next.connections.own.edit')}
        </SetButton>
      </div>
    );

  const save = async () => {
    if (busy || name.trim() === '') return;
    setBusy(true);
    setError(null);
    const refused = await onSave(server.id, {
      name: name.trim(),
      category,
      letter: own.letter,
      about: ru ? own.about : about.trim(),
      aboutRu: ru ? about.trim() : own.aboutRu,
      can: ru ? own.can : lines(can),
      canRu: ru ? lines(can) : own.canRu,
      steps: ru ? own.steps : lines(steps),
      stepsRu: ru ? lines(steps) : own.stepsRu,
    });
    setBusy(false);
    if (refused === null) setOpen(false);
    else setError(refused);
  };

  return (
    <div className="hx-svc-card hx-own__editor">
      <Row id={`${ids}-name`} label={t('next.connections.own.name')}>
        <input
          id={`${ids}-name`}
          className="hx-set__input"
          value={name}
          maxLength={60}
          disabled={busy}
          onChange={(event) => setName(event.target.value)}
        />
      </Row>
      <Row id={`${ids}-kind`} label={t('next.connections.own.group')}>
        <select
          id={`${ids}-kind`}
          className="hx-set__input"
          value={category}
          disabled={busy}
          onChange={(event) => setCategory(MCP_KINDS.find((k) => k === event.target.value) ?? 'utility')}
        >
          {MCP_KINDS.map((k) => (
            <option key={k} value={k}>
              {t(`next.connections.mcp.kind.${k}`)}
            </option>
          ))}
        </select>
      </Row>
      <Row id={`${ids}-about`} label={t('next.connections.own.about')}>
        <textarea
          id={`${ids}-about`}
          className="hx-set__input hx-own__text"
          value={about}
          rows={4}
          maxLength={1500}
          disabled={busy}
          onChange={(event) => setAbout(event.target.value)}
        />
      </Row>
      <Row id={`${ids}-can`} label={t('next.connections.own.can')} hint={t('next.connections.own.perLine')}>
        <textarea
          id={`${ids}-can`}
          className="hx-set__input hx-own__text"
          value={can}
          rows={4}
          disabled={busy}
          onChange={(event) => setCan(event.target.value)}
        />
      </Row>
      <Row
        id={`${ids}-steps`}
        label={t('next.connections.own.steps')}
        hint={t('next.connections.own.perLine')}
      >
        <textarea
          id={`${ids}-steps`}
          className="hx-set__input hx-own__text"
          value={steps}
          rows={4}
          disabled={busy}
          onChange={(event) => setSteps(event.target.value)}
        />
      </Row>
      <div className="hx-svc-form__go">
        <SetButton tone="primary" busy={busy} disabled={name.trim() === ''} onClick={() => void save()}>
          {t('next.connections.save')}
        </SetButton>
        <SetButton tone="ghost" disabled={busy} onClick={() => setOpen(false)}>
          {t('next.connections.cancel')}
        </SetButton>
      </div>
      {error !== null && <FormError>{error}</FormError>}
    </div>
  );
};

export default OwnDescribe;
