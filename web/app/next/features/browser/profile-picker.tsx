import { Menu } from '@base-ui/react/menu';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import Icon from '@/next/ui/icon';
import { useEffect, useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import Modal from '@/next/features/shell/modal';
import { bp, profileName } from './profile-fields';
import { changeProfile, loadConversationProfiles, type IConversationProfiles } from './profiles-api';
import '@/next/styles/browser-profiles.css';

export function ProfilePicker({
  machine,
  conv,
  currentId,
  onPause,
  onResume,
}: {
  machine: MachineId;
  conv: string;
  currentId?: string;
  onPause: () => void;
  onResume: () => void;
}) {
  const menuActions = useMenuActions();
  const [data, setData] = useState<IConversationProfiles | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [pending, setPending] = useState<string | null>(null);
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let gone = false;
    void loadConversationProfiles(machine, conv)
      .then((value) => {
        if (!gone) {
          setData(value);
          setError(null);
        }
      })
      .catch((e) => {
        if (!gone) setError(e instanceof Error ? e.message : String(e));
      });
    const changed = (event: Event) => {
      if ((event as CustomEvent).detail === machine) setAttempt((n) => n + 1);
    };
    window.addEventListener('browser-profiles-changed', changed);
    return () => {
      gone = true;
      window.removeEventListener('browser-profiles-changed', changed);
    };
  }, [machine, conv, attempt, currentId]);
  const use = async (id: string) => {
    setPending(null);
    setBusy(true);
    setError(null);
    onPause();
    try {
      await changeProfile(machine, { action: 'use', conv, id });
      setAttempt((n) => n + 1);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
      onResume();
    }
  };
  return (
    <div className="hx-browser-profile-picker">
      <Menu.Root
        actionsRef={menuActions}
        onOpenChange={(open) => {
          if (open) setAttempt((n) => n + 1);
        }}
      >
        <Menu.Trigger className="hx-agent-browser__profile" aria-label={bp('profile')} disabled={busy}>
          <span
            className="hx-browser-profiles__dot"
            style={{ background: data?.current.color || 'transparent' }}
          />
          <span>{data ? profileName(data.current) : bp('profile')}</span>
          <Icon name="chevron-down" size="sm" />
        </Menu.Trigger>
        <MenuPanel actions={menuActions} className="hx-cmenu hx-browser-profiles__menu" label={bp('profile')}>
          <Menu.RadioGroup
            value={data?.current.id ?? ''}
            onValueChange={(id) => {
              setBusy(true);
              void loadConversationProfiles(machine, conv)
                .then((next) => {
                  setData(next);
                  if (next.hasTabs) setPending(id);
                  else return use(id);
                })
                .catch((e) => setError(e instanceof Error ? e.message : String(e)))
                .finally(() => setBusy(false));
            }}
          >
            {data?.profiles.map((profile) => (
              <Menu.RadioItem
                closeOnClick
                key={profile.id}
                value={profile.id}
                className="hx-cmenu__item"
                disabled={busy}
              >
                <span className="hx-browser-profiles__dot" style={{ background: profile.color }} />
                <span>{profileName(profile)}</span>
                {profile.id === data.current.id && <Icon name="check" size="sm" />}
              </Menu.RadioItem>
            ))}
          </Menu.RadioGroup>
          <Menu.Separator className="hx-cmenu__sep" />
          <Menu.Item
            className="hx-cmenu__item"
            onClick={() => goTo({ kind: ROUTE.settings, section: 'browser' })}
          >
            <Icon name="settings" size="sm" />
            {bp('settings')}
          </Menu.Item>
        </MenuPanel>
      </Menu.Root>
      {error && (
        <button
          type="button"
          className="hx-head-btn"
          title={error}
          aria-label={error}
          onClick={() => setAttempt((n) => n + 1)}
        >
          !
        </button>
      )}
      <Modal
        boxClass="hx-browser-profiles"
        open={pending !== null}
        onClose={() => setPending(null)}
        title={bp('switchTitle')}
        lead={bp('switchWarning')}
      >
        <div className="hx-modal__foot">
          <button type="button" className="hx-btn" onClick={() => setPending(null)}>
            {bp('cancel')}
          </button>
          <button
            type="button"
            className="hx-btn"
            data-variant="primary"
            onClick={() => {
              if (pending) void use(pending);
            }}
          >
            {bp('switch')}
          </button>
        </div>
      </Modal>
    </div>
  );
}
