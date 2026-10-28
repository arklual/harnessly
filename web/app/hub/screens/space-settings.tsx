// /s/<slug>/settings: the space's name, its people and invitations, its machines.
import { useCallback, useEffect, useState } from 'react';

import { Button, Dialog, TextField } from '@kit';

import { isApiError } from '../lib/api';
import { useApi, useEnv } from '../lib/env';
import { errorText } from '../lib/errors';
import { hhmm, isEmailLike } from '../lib/format';
import { t } from '../lib/i18n';
import { Link, navigate } from '../lib/router';
import { useMe, useSession } from '../lib/session';
import type { Invite, Machine, Member, Role } from '../lib/types';
import { PlainPage } from '../ui/frame';
import { LinkIcon, Pencil, Plus, Trash2 } from '../ui/icons';
import { MachineRow } from '../ui/machine-row';
import { CopyBox, ListSkeleton, Msg, Note, Segmented } from '../ui/parts';
import { SpaceMenu } from './home';
import { useSpace } from './space';

export const SpaceSettingsScreen = () => {
  const { space, owner } = useSpace();
  return (
    <PlainPage
      title={t('settings.space.title', { name: space.name })}
      side={<SpaceMenu current={space.slug} />}
    >
      <p className="hub-note">
        <Link to={`/s/${space.slug}`} className="hub-link">
          {t('settings.back', { name: space.name })}
        </Link>
      </p>
      {owner && <NameSection />}
      <MembersSection />
      {owner && <InvitesSection />}
      <MachinesSection />
    </PlainPage>
  );
};

const NameSection = () => {
  const api = useApi();
  const { reload } = useSession();
  const { space, setSpace } = useSpace();
  const [name, setName] = useState(space.name);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const save = async () => {
    const clean = name.trim();
    if (!clean || clean.length > 60) {
      setError(t('space.nameEmpty'));
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const { space: next } = await api.spaces.update(space.id, { name: clean });
      setSpace(next);
      await reload();
      setSaved(true);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="hub-section">
      <h2 className="hub-section__t">{t('settings.space.name')}</h2>
      <form
        className="hub-inputrow"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <TextField
          size="m"
          aria-label={t('settings.space.name')}
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            setSaved(false);
          }}
          error={error ?? undefined}
        />
        <Button type="submit" loading={busy} disabled={name.trim() === space.name}>
          {saved ? t('common.saved') : t('common.save')}
        </Button>
      </form>
    </section>
  );
};

const MembersSection = () => {
  const api = useApi();
  const me = useMe();
  const { reload } = useSession();
  const { space, owner, live } = useSpace();
  const [members, setMembers] = useState<Member[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const tick = live.state.membersTick;

  const load = useCallback(async () => {
    try {
      setMembers((await api.spaces.members(space.id)).members);
    } catch (e) {
      setError(errorText(e));
    }
  }, [api, space.id]);

  useEffect(() => {
    let alive = true;
    api.spaces.members(space.id).then(
      (r) => {
        if (alive) setMembers(r.members);
      },
      (e: unknown) => {
        if (alive) setError(errorText(e));
      },
    );
    return () => {
      alive = false;
    };
  }, [api, space.id, tick]);

  const setRole = async (m: Member, role: Role) => {
    setBusy(m.userId);
    setError(null);
    try {
      await api.spaces.setRole(space.id, m.userId, role);
      await load();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(null);
    }
  };

  const remove = async (m: Member) => {
    setBusy(m.userId);
    setError(null);
    try {
      await api.spaces.removeMember(space.id, m.userId);
      if (m.userId === me.user.id) {
        await reload();
        navigate('/', { replace: true });
        return;
      }
      await load();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(null);
    }
  };

  return (
    <section className="hub-section">
      <h2 className="hub-section__t">{t('settings.members')}</h2>
      {members === null ? (
        <ListSkeleton rows={2} />
      ) : (
        <div className="hub-list">
          {members.map((m) => {
            const self = m.userId === me.user.id;
            return (
              <div key={m.userId} className="hub-item">
                <div className="hub-item__head">
                  <span className="hub-t">
                    <span className="hub-t__n">{m.name || m.email}</span>
                    <span className="hub-item__s">{m.name ? m.email : ''}</span>
                  </span>
                  {owner ? (
                    <Segmented<Role>
                      value={m.role}
                      label={t('settings.role')}
                      onChange={(role) => void setRole(m, role)}
                      options={[
                        { value: 'owner', label: t('settings.role.owner'), disabled: busy === m.userId },
                        { value: 'member', label: t('settings.role.member'), disabled: busy === m.userId },
                      ]}
                    />
                  ) : (
                    <span className="hub-item__st">
                      {t(m.role === 'owner' ? 'settings.role.owner' : 'settings.role.member')}
                    </span>
                  )}
                  {(owner || self) && (
                    <Button
                      size="xs"
                      appearance="ghost"
                      loading={busy === m.userId}
                      onClick={() => void remove(m)}
                    >
                      {self ? t('settings.leave') : t('settings.remove')}
                    </Button>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
      {error && <Msg tone="danger">{error}</Msg>}
    </section>
  );
};

const InvitesSection = () => {
  const api = useApi();
  const env = useEnv();
  const { space, live } = useSpace();
  const [invites, setInvites] = useState<Invite[] | null>(null);
  const [email, setEmail] = useState('');
  const [busy, setBusy] = useState<null | 'mail' | 'link' | string>(null);
  const [error, setError] = useState<string | null>(null);
  const [fresh, setFresh] = useState<Invite[]>([]);
  const tick = live.state.membersTick;

  const load = useCallback(async () => {
    try {
      setInvites((await api.spaces.invites(space.id)).invites);
    } catch (e) {
      setError(errorText(e));
    }
  }, [api, space.id]);

  useEffect(() => {
    let alive = true;
    api.spaces.invites(space.id).then(
      (r) => {
        if (alive) setInvites(r.invites);
      },
      (e: unknown) => {
        if (alive) setError(errorText(e));
      },
    );
    return () => {
      alive = false;
    };
  }, [api, space.id, tick]);

  const send = async () => {
    const address = email.trim();
    if (!isEmailLike(address)) {
      setError(t('space.badInvite'));
      return;
    }
    setBusy('mail');
    setError(null);
    try {
      const r = await api.spaces.invite(space.id, [address]);
      setEmail('');
      setFresh(r.invites.filter((i) => i.sent === false && i.url));
      await load();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(null);
    }
  };

  const makeLink = async () => {
    setBusy('link');
    setError(null);
    try {
      const { invite } = await api.spaces.inviteLink(space.id, { days: 7 });
      if (invite.url) {
        setFresh([invite]);
        void env.copyText(invite.url);
      }
      await load();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(null);
    }
  };

  const revoke = async (i: Invite) => {
    setBusy(i.id);
    setError(null);
    try {
      await api.spaces.revokeInvite(space.id, i.id);
      await load();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(null);
    }
  };

  const renderInvites = () => {
    if (invites === null) return <ListSkeleton rows={1} />;
    if (invites.length === 0) return <Note>{t('settings.invites.none')}</Note>;
    return (
      <div className="hub-list">
        {invites.map((i) => (
          <div key={i.id} className="hub-item">
            <div className="hub-item__head">
              <span className="hub-t">
                <span className="hub-t__n">{i.kind === 'email' ? i.email : t('settings.invite.link')}</span>
                <span className="hub-item__s">
                  {t('settings.invite.until', { time: hhmm(i.expiresAt) })}
                  {i.maxUses ? ` · ${i.uses}/${i.maxUses}` : ''}
                </span>
              </span>
              <Button
                appearance="ghost"
                size="xs"
                className="k-btn--icon"
                aria-label={t('settings.invite.revoke')}
                title={t('settings.invite.revoke')}
                loading={busy === i.id}
                onClick={() => void revoke(i)}
              >
                <Trash2 size={14} />
              </Button>
            </div>
          </div>
        ))}
      </div>
    );
  };

  return (
    <section className="hub-section">
      <div className="hub-section__h">
        <h2 className="hub-section__t">{t('settings.invites')}</h2>
        <Button
          size="s"
          icon={<LinkIcon size={14} />}
          loading={busy === 'link'}
          onClick={() => void makeLink()}
        >
          {t('settings.newLink')}
        </Button>
      </div>
      <form
        className="hub-inputrow"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        <TextField
          size="m"
          type="email"
          aria-label={t('space.colleagueMail')}
          placeholder="name@company.ru"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <Button type="submit" icon={<Plus size={14} />} loading={busy === 'mail'}>
          {t('settings.invite')}
        </Button>
      </form>
      {fresh.map((i) => (
        <div key={i.id} className="hub-stack">
          <Msg tone={i.sent === false ? 'warn' : 'success'}>
            {i.sent === false ? t('space.noMail') : t('settings.linkMade')}
          </Msg>
          <CopyBox text={i.url ?? ''} wrap />
        </div>
      ))}
      {renderInvites()}
      {error && <Msg tone="danger">{error}</Msg>}
    </section>
  );
};

const MachinesSection = () => {
  const { space, live, owner } = useSpace();
  const machines = live.state.machines;
  const [renaming, setRenaming] = useState<Machine | null>(null);
  const [removing, setRemoving] = useState<Machine | null>(null);
  const renderMachines = () => {
    if (machines === null) return <ListSkeleton rows={2} />;
    if (machines.length === 0) return <Note>{t('home.empty.title')}</Note>;
    return (
      <div className="hub-list">
        {machines.map((m) => (
          <MachineRow
            key={m.id}
            machine={m}
            details
            actions={
              owner && (
                <>
                  <Button
                    size="xs"
                    appearance="ghost"
                    className="k-btn--icon"
                    aria-label={t('settings.machine.rename')}
                    title={t('settings.machine.rename')}
                    onClick={() => setRenaming(m)}
                  >
                    <Pencil size={14} />
                  </Button>
                  <Button
                    size="xs"
                    appearance="ghost"
                    className="k-btn--icon"
                    aria-label={t('settings.machine.remove')}
                    title={t('settings.machine.remove')}
                    onClick={() => setRemoving(m)}
                  >
                    <Trash2 size={14} />
                  </Button>
                </>
              )
            }
          />
        ))}
      </div>
    );
  };

  return (
    <section className="hub-section">
      <div className="hub-section__h">
        <h2 className="hub-section__t">{t('settings.machines')}</h2>
        {owner && (
          <Button
            size="s"
            icon={<Plus size={14} />}
            onClick={() => navigate(`/s/${space.slug}/setup/machines`)}
          >
            {t('home.addMachine')}
          </Button>
        )}
      </div>
      {renderMachines()}
      {renaming && <RenameDialog key={renaming.id} machine={renaming} onClose={() => setRenaming(null)} />}
      {removing && <RemoveDialog key={removing.id} machine={removing} onClose={() => setRemoving(null)} />}
    </section>
  );
};

const RenameDialog = ({ machine, onClose }: { machine: Machine; onClose: () => void }) => {
  const api = useApi();
  const { live } = useSpace();
  const [name, setName] = useState(machine.name);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const save = async () => {
    const clean = name.trim();
    if (!clean) return;
    setBusy(true);
    setError(null);
    try {
      const { machine: next } = await api.machines.rename(machine.id, clean);
      live.push({ type: 'event', event: { type: 'machine', data: { machine: next } } });
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open
      onClose={onClose}
      size="s"
      title={t('settings.machine.rename')}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button appearance="primary" loading={busy} disabled={!name.trim()} onClick={() => void save()}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <TextField
        size="m"
        aria-label={t('settings.machine.name')}
        value={name}
        onChange={(e) => setName(e.target.value)}
        error={error ?? undefined}
      />
    </Dialog>
  );
};

const RemoveDialog = ({ machine, onClose }: { machine: Machine; onClose: () => void }) => {
  const api = useApi();
  const { live } = useSpace();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const remove = async () => {
    setBusy(true);
    setError(null);
    try {
      await api.machines.remove(machine.id);
      live.push({ type: 'event', event: { type: 'machineRemoved', data: { machineId: machine.id } } });
      onClose();
    } catch (e) {
      setError(isApiError(e) ? errorText(e) : t('err.internal'));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open
      onClose={onClose}
      size="s"
      title={t('settings.machine.removeTitle', { name: machine.name })}
      description={t('settings.machine.removeText')}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button appearance="danger" loading={busy} onClick={() => void remove()}>
            {t('settings.machine.removeDo')}
          </Button>
        </>
      }
    >
      {error && <Msg tone="danger">{error}</Msg>}
    </Dialog>
  );
};
