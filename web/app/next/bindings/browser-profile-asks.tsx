import { useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import Modal from '@/next/features/shell/modal';
import { useProfiles } from '@/next/features/browser/use-profiles';
import { changeProfile, type IProfileAsk, type IProfileChoice } from '@/next/features/browser/profiles-api';
import { bp, ProfileFields, ProfileOptions } from '@/next/features/browser/profile-fields';
import '@/next/styles/browser-profiles.css';

export function BrowserProfileAskHost({ machine }: { machine: MachineId | null }) {
  const { data } = useProfiles(machine, true);
  const [later, setLater] = useState<string[]>([]);
  const ask = data?.asks.find((a) => !later.includes(`${machine}:${a.id}`));
  if (!machine || !ask || !data) return null;
  return (
    <ProfileAsk
      key={`${machine}:${ask.id}`}
      machine={machine}
      ask={ask}
      profiles={data.profiles}
      onLater={() => setLater((now) => [...now, `${machine}:${ask.id}`])}
    />
  );
}
function ProfileAsk({
  machine,
  ask,
  profiles,
  onLater,
}: {
  machine: MachineId;
  ask: IProfileAsk;
  profiles: IProfileChoice[];
  onLater: () => void;
}) {
  const [id, setId] = useState(profiles[0]?.id ?? '');
  const [name, setName] = useState(''),
    [color, setColor] = useState('#6688cc');
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<string | null>(null);
  const answer = async (scope: 'chat' | 'project' | 'decline') => {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await changeProfile(machine, {
        action: 'answer',
        askId: ask.id,
        id,
        scope,
        ...(!id ? { name: name.trim(), color } : {}),
      });
      onLater();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal
      open
      onClose={() => {
        if (!busy) onLater();
      }}
      title={bp('askTitle')}
      lead={ask.title || ask.conv}
      boxClass="hx-browser-profiles"
    >
      <div className="hx-modal__body">
        <p className="hx-browser-profiles__why">{ask.why}</p>
        <label className="hx-create__f">
          <span className="hx-create__l">{bp('profile')}</span>
          <select
            className="hx-create__in"
            value={id}
            onChange={(e) => setId(e.target.value)}
            disabled={busy}
          >
            <ProfileOptions profiles={profiles} />
            <option value="">{bp('create')}</option>
          </select>
        </label>
        {!id && (
          <ProfileFields name={name} color={color} onName={setName} onColor={setColor} disabled={busy} />
        )}
        <p>{bp('accessNote')}</p>
        {error && (
          <p role="alert" className="hx-create__err">
            {error}
          </p>
        )}
      </div>
      <div className="hx-modal__foot hx-browser-profiles__actions">
        <button type="button" className="hx-btn" disabled={busy} onClick={() => void answer('decline')}>
          {bp('decline')}
        </button>
        <button
          type="button"
          className="hx-btn"
          disabled={busy || (!id && !name.trim())}
          onClick={() => void answer('chat')}
        >
          {bp('chat')}
        </button>
        <button
          type="button"
          className="hx-btn"
          data-variant="primary"
          disabled={busy || (!id && !name.trim())}
          onClick={() => void answer('project')}
        >
          {bp('project')}
        </button>
      </div>
    </Modal>
  );
}
