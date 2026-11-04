import { useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import { useProfiles } from './use-profiles';
import { changeProfile, type IProfileAccess, type IProfileChoice } from './profiles-api';
import { bp, ProfileOptions, profileName } from './profile-fields';
import '@/next/styles/browser-profiles.css';

export function ProjectBrowserSettings({ machine, project }: { machine: MachineId; project: string }) {
  const { data, error, retry } = useProfiles(machine);
  if (error)
    return (
      <p role="alert" className="hx-create__err">
        {error}{' '}
        <button type="button" className="hx-btn" onClick={retry}>
          {bp('retry')}
        </button>
      </p>
    );
  if (!data) return <p role="status">{bp('loading')}</p>;
  const access = data.projects[project] ?? { default: '', granted: [] };
  return (
    <ProjectAccess
      key={`${machine}:${project}:${JSON.stringify(access)}`}
      machine={machine}
      project={project}
      access={access}
      profiles={data.profiles}
    />
  );
}
function ProjectAccess({
  machine,
  project,
  access,
  profiles,
}: {
  machine: MachineId;
  project: string;
  access: IProfileAccess;
  profiles: IProfileChoice[];
}) {
  const [id, setId] = useState(access.default),
    [granted, setGranted] = useState(access.granted ?? []);
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<string | null>(null),
    [saved, setSaved] = useState(false);
  const save = async () => {
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      await changeProfile(machine, {
        action: 'project',
        project,
        default: id,
        granted: granted.filter((g) => g !== id),
      });
      setSaved(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <fieldset className="hx-browser-profiles__project" disabled={busy}>
      <legend>{bp('projectSettings')}</legend>
      <p>{bp('projectNote')}</p>
      <label className="hx-create__f">
        <span className="hx-create__l">{bp('default')}</span>
        <select
          className="hx-create__in"
          value={id}
          onChange={(e) => {
            setId(e.target.value);
            setSaved(false);
          }}
        >
          <option value="">{bp('own')}</option>
          <ProfileOptions profiles={profiles} />
        </select>
      </label>
      <p>{bp('grants')}</p>
      <div className="hx-browser-profiles__grants">
        {profiles
          .filter((p) => p.id !== id)
          .map((p) => (
            <label className="hx-browser-profiles__check" key={p.id}>
              <input
                type="checkbox"
                checked={granted.includes(p.id)}
                onChange={(e) => {
                  setGranted((now) => (e.target.checked ? [...now, p.id] : now.filter((g) => g !== p.id)));
                  setSaved(false);
                }}
              />
              <span className="hx-browser-profiles__dot" style={{ background: p.color }} />
              {profileName(p)}
            </label>
          ))}
      </div>
      <button type="button" className="hx-btn" disabled={busy} onClick={() => void save()}>
        {bp('saveAccess')}
      </button>
      {saved && <p role="status">{bp('saved')}</p>}
      {error && (
        <p role="alert" className="hx-create__err">
          {error}
        </p>
      )}
    </fieldset>
  );
}
