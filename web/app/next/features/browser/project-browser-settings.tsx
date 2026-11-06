import { useEffect, useRef, useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import Modal from '@/next/features/shell/modal';
import { useProfiles } from './use-profiles';
import { changeProfile, previewProjectAccess, type IProfileAccess } from './profiles-api';
import { bp, ProfileOptions, profileName } from './profile-fields';
import '@/next/styles/browser-profiles.css';

/** Keeps browser access in the project dialog's single prepare/save flow. */
export function useProjectBrowserSettings(machine: MachineId | null, project: string | null) {
  const { data, error, retry } = useProfiles(project ? machine : null);
  const key = `${machine}:${project}`;
  const [draft, setDraft] = useState<{ key: string; access: IProfileAccess } | null>(null);
  const [affected, setAffected] = useState<number | null>(null);
  const [shownKey, setShownKey] = useState(key);
  if (shownKey !== key) {
    setShownKey(key);
    setDraft(null);
    setAffected(null);
  }
  const confirmation = useRef<((accepted: boolean) => void) | null>(null);
  const prepared = useRef<IProfileAccess | null>(null);
  const current = (project && data?.projects[project]) || { default: '', granted: [] };
  const access = draft?.key === key ? draft.access : current;
  const edit = (access: IProfileAccess) => setDraft({ key, access });
  const answer = (accepted: boolean) => {
    confirmation.current?.(accepted);
    confirmation.current = null;
    setAffected(null);
  };
  useEffect(
    () => () => {
      confirmation.current?.(false);
      confirmation.current = null;
      prepared.current = null;
    },
    [key],
  );
  const prepare = async () => {
    prepared.current = null;
    if (!machine || !project || draft?.key !== key) return true;
    const next = { ...access, granted: access.granted.filter((id) => id !== access.default) };
    if (
      next.default === current.default &&
      [...next.granted].sort().join(',') === [...current.granted].sort().join(',')
    )
      return true;
    try {
      const count = await previewProjectAccess(machine, project, next);
      if (count > 0) {
        const accepted = await new Promise<boolean>((resolve) => {
          confirmation.current = resolve;
          setAffected(count);
        });
        if (!accepted) return false;
      }
      prepared.current = next;
      return true;
    } catch (failure) {
      throw new Error(
        bp('accessFailed', { reason: failure instanceof Error ? failure.message : String(failure) }),
        { cause: failure },
      );
    }
  };
  const save = async () => {
    if (!machine || !project || !prepared.current) return;
    try {
      await changeProfile(machine, { action: 'project', project, ...prepared.current });
      prepared.current = null;
    } catch (failure) {
      throw new Error(
        bp('accessFailed', { reason: failure instanceof Error ? failure.message : String(failure) }),
        { cause: failure },
      );
    }
  };
  return {
    prepare,
    save,
    render: (disabled: boolean) => (
      <>
        {error ? (
          <p role="alert" className="hx-create__err">
            {error}{' '}
            <button type="button" className="hx-btn" onClick={retry}>
              {bp('retry')}
            </button>
          </p>
        ) : !data ? (
          <p role="status">{bp('loading')}</p>
        ) : (
          <fieldset className="hx-browser-profiles__project" disabled={disabled}>
            <legend>{bp('projectSettings')}</legend>
            <p>{bp('projectNote')}</p>
            <label className="hx-create__f">
              <span className="hx-create__l">{bp('default')}</span>
              <select
                className="hx-create__in"
                value={access.default}
                onChange={(e) => edit({ ...access, default: e.target.value })}
              >
                <option value="">{bp('own')}</option>
                <ProfileOptions profiles={data.profiles} />
              </select>
            </label>
            <p>{bp('grants')}</p>
            <div className="hx-browser-profiles__grants">
              {data.profiles
                .filter((p) => p.id !== access.default)
                .map((p) => (
                  <label className="hx-browser-profiles__check" key={p.id}>
                    <input
                      type="checkbox"
                      checked={access.granted.includes(p.id)}
                      onChange={(e) =>
                        edit({
                          ...access,
                          granted: e.target.checked
                            ? [...access.granted, p.id]
                            : access.granted.filter((id) => id !== p.id),
                        })
                      }
                    />
                    <span className="hx-browser-profiles__dot" style={{ background: p.color }} />
                    {profileName(p)}
                  </label>
                ))}
            </div>
          </fieldset>
        )}
        <Modal
          boxClass="hx-browser-profiles"
          open={affected !== null}
          onClose={() => answer(false)}
          title={bp('projectSwitchTitle')}
          lead={bp('projectSwitchWarning', { count: affected ?? 0 })}
        >
          <div className="hx-modal__foot">
            <button type="button" className="hx-btn" onClick={() => answer(false)}>
              {bp('cancel')}
            </button>
            <button type="button" className="hx-btn" data-variant="primary" onClick={() => answer(true)}>
              {bp('confirm')}
            </button>
          </div>
        </Modal>
      </>
    ),
  };
}
