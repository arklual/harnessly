import { useState, type FormEvent } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import { useProjects } from '@/next/stores/projects';
import { formatAgoFull } from '@/next/i18n/format';
import Modal from '@/next/features/shell/modal';
import { Group } from '@/next/features/settings/rows';
import { useProfiles } from '@/next/features/browser/use-profiles';
import {
  changeProfile,
  type IBrowserProfile,
  type TProfileAction,
} from '@/next/features/browser/profiles-api';
import { bp, ProfileFields, profileName } from '@/next/features/browser/profile-fields';
import '@/next/styles/browser-profiles.css';

export function BrowserPage({ machine, now }: { machine: MachineId; now: number }) {
  return <BrowserSettings key={machine} machine={machine} now={now} />;
}
function BrowserSettings({ machine, now }: { machine: MachineId; now: number }) {
  const { data, error, retry } = useProfiles(machine, true);
  const projects = useProjects(machine);
  const projectName = (id: string) => projects.find((p) => p.id === id)?.name ?? id;
  const [busy, setBusy] = useState(false),
    [failure, setFailure] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<
    { action: 'stop' | 'clear' | 'delete'; profile: IBrowserProfile } | 'sandbox' | null
  >(null);
  const [warning, setWarning] = useState<string | null>(null);
  const [mode, setMode] = useState('create');
  const [name, setName] = useState(''),
    [color, setColor] = useState('#6688cc'),
    [path, setPath] = useState('');
  const run = async (action: TProfileAction) => {
    if (busy) return false;
    setBusy(true);
    setFailure(null);
    setWarning(null);
    try {
      await changeProfile(machine, action);
      setWarning(action.action === 'import' ? bp('importNote') : null);
      setConfirm(null);
      return true;
    } catch (e) {
      setFailure(e instanceof Error ? e.message : String(e));
      return false;
    } finally {
      setBusy(false);
    }
  };
  const create = async (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim() || (mode === 'import' && !path.trim())) return;
    if (
      await run(
        mode === 'import'
          ? { action: 'import', name: name.trim(), color, path: path.trim() }
          : { action: 'create', name: name.trim(), color },
      )
    ) {
      setName('');
      setPath('');
    }
  };
  return (
    <div className="hx-browser-profiles">
      {(error || failure) && (
        <p role="alert" className="hx-create__err">
          {failure || error}{' '}
          <button type="button" className="hx-btn" onClick={retry}>
            {bp('retry')}
          </button>
        </p>
      )}
      {warning && (
        <p role="status" className="hx-browser-profiles__warning">
          {warning}
        </p>
      )}
      {!data && !error && <p role="status">{bp('loading')}</p>}
      <Group title={bp('profiles')}>
        {data?.profiles.length === 0 && <p>{bp('empty')}</p>}
        {data?.profiles.map((profile) => (
          <ProfileRow
            key={profile.id}
            profile={profile}
            now={now}
            projectNames={profile.projects.map(projectName)}
            busy={busy}
            onSave={(name, color) => run({ action: 'update', id: profile.id, name, color })}
            onAction={(action) => setConfirm({ action, profile })}
          />
        ))}
      </Group>
      <Group title={bp('add')}>
        <form className="hx-browser-profiles__form" onSubmit={(e) => void create(e)}>
          <label className="hx-create__f">
            <span className="hx-create__l">{bp('source')}</span>
            <select
              className="hx-create__in"
              value={mode}
              onChange={(e) => setMode(e.target.value)}
              disabled={busy}
            >
              <option value="create">{bp('create')}</option>
              <option value="import">{bp('import')}</option>
            </select>
          </label>
          <ProfileFields name={name} color={color} onName={setName} onColor={setColor} disabled={busy} />
          {mode === 'import' && (
            <>
              <label className="hx-create__f">
                <span className="hx-create__l">{bp('path')}</span>
                <input
                  className="hx-create__in"
                  required
                  value={path}
                  placeholder="/path/to/chrome-data"
                  onChange={(e) => setPath(e.target.value)}
                  disabled={busy}
                />
              </label>
              <p>{bp('importSourceNote')}</p>
            </>
          )}
          <button
            className="hx-btn"
            data-variant="primary"
            type="submit"
            disabled={busy || !name.trim() || (mode === 'import' && !path.trim())}
          >
            {bp(mode === 'import' ? 'import' : 'create')}
          </button>
        </form>
      </Group>
      <Group title={bp('sandbox')}>
        <div className="hx-browser-profiles__form">
          <p>{bp('sandboxNote')}</p>
          <label className="hx-browser-profiles__check">
            <input
              type="checkbox"
              checked={data?.noSandbox ?? false}
              disabled={!data || busy}
              onChange={(e) => {
                if (e.target.checked) setConfirm('sandbox');
                else void run({ action: 'sandbox', noSandbox: false });
              }}
            />
            {bp('noSandbox')}
          </label>
          {data?.noSandbox && (
            <p role="status" className="hx-browser-profiles__warning">
              {bp('sandboxWarning')}
            </p>
          )}
          <p>{bp('isolationNote')}</p>
        </div>
      </Group>
      <Modal
        boxClass="hx-browser-profiles"
        open={confirm !== null}
        onClose={() => {
          if (!busy) setConfirm(null);
        }}
        title={confirm === 'sandbox' ? bp('noSandbox') : bp(`${confirm?.action ?? 'delete'}Title`)}
        lead={
          confirm === 'sandbox'
            ? bp('sandboxWarning')
            : bp(`${confirm?.action ?? 'delete'}Warning`, {
                name: confirm ? profileName(confirm.profile) : '',
              })
        }
      >
        <div className="hx-modal__body">
          {confirm && confirm !== 'sandbox' && (
            <p>
              {bp('projects')}: {confirm.profile.projects.map(projectName).join(', ') || bp('none')}
            </p>
          )}
          {failure && (
            <p role="alert" className="hx-create__err">
              {failure}
            </p>
          )}
        </div>
        <div className="hx-modal__foot">
          <button type="button" className="hx-btn" disabled={busy} onClick={() => setConfirm(null)}>
            {bp('cancel')}
          </button>
          <button
            type="button"
            className="hx-btn"
            data-variant="primary"
            disabled={busy}
            onClick={() => {
              if (confirm === 'sandbox') void run({ action: 'sandbox', noSandbox: true });
              else if (confirm) void run({ action: confirm.action, id: confirm.profile.id });
            }}
          >
            {bp('confirm')}
          </button>
        </div>
      </Modal>
    </div>
  );
}
function ProfileRow({
  profile,
  now,
  projectNames,
  busy,
  onSave,
  onAction,
}: {
  profile: IBrowserProfile;
  now: number;
  projectNames: string[];
  busy: boolean;
  onSave: (name: string, color: string) => Promise<boolean>;
  onAction: (action: 'stop' | 'clear' | 'delete') => void;
}) {
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(profile.name),
    [color, setColor] = useState(profile.color);
  return (
    <article className="hx-browser-profiles__row">
      <div className="hx-browser-profiles__heading">
        <span className="hx-browser-profiles__dot" style={{ background: profile.color }} />
        <strong>{profileName(profile)}</strong>
        <span>{bp(profile.running ? 'running' : 'stopped')}</span>
      </div>
      <dl>
        <dt>{bp('path')}</dt>
        <dd>
          <code>{profile.path}</code>
        </dd>
        <dt>{bp('kind')}</dt>
        <dd>{bp(profile.kind === 'attached' ? 'attached' : 'managed')}</dd>
        <dt>{bp('projects')}</dt>
        <dd>{projectNames.join(', ') || bp('none')}</dd>
        <dt>{bp('lastUse')}</dt>
        <dd>{profile.lastUsedAt ? formatAgoFull(profile.lastUsedAt, now) : bp('never')}</dd>
      </dl>
      {profile.usedBy?.length > 0 && (
        <details>
          <summary>{bp('recentChats')}</summary>
          <ul>
            {profile.usedBy.map((use) => (
              <li key={`${use.conv}:${use.at}`}>
                {use.title || use.conv} · {formatAgoFull(use.at, now)}
              </li>
            ))}
          </ul>
        </details>
      )}
      {editing && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim())
              void onSave(name.trim(), color).then((ok) => {
                if (ok) setEditing(false);
              });
          }}
        >
          <ProfileFields name={name} color={color} onName={setName} onColor={setColor} disabled={busy} />
          <div className="hx-browser-profiles__actions">
            <button type="submit" className="hx-btn" disabled={busy || !name.trim()}>
              {bp('save')}
            </button>
            <button type="button" className="hx-btn" disabled={busy} onClick={() => setEditing(false)}>
              {bp('cancel')}
            </button>
          </div>
        </form>
      )}
      <div className="hx-browser-profiles__actions">
        <button
          type="button"
          className="hx-btn"
          disabled={busy || editing}
          onClick={() => {
            setName(profile.name);
            setColor(profile.color);
            setEditing(true);
          }}
        >
          {bp('edit')}
        </button>
        <button
          type="button"
          className="hx-btn"
          disabled={busy || !profile.running}
          onClick={() => onAction('stop')}
        >
          {bp('stop')}
        </button>
        <button type="button" className="hx-btn" disabled={busy} onClick={() => onAction('clear')}>
          {bp('clear')}
        </button>
        <button type="button" className="hx-btn" disabled={busy} onClick={() => onAction('delete')}>
          {bp('delete')}
        </button>
      </div>
    </article>
  );
}
