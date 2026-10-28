// /s/<slug>: the space — its machines with a way into each one's runtime page, and the way to add another.
import { useState } from 'react';

import { Button, EmptyState } from '@kit';

import { useApi, useEnv } from '../lib/env';
import { errorText } from '../lib/errors';
import { t } from '../lib/i18n';
import { Link, navigate } from '../lib/router';
import { useMe, useSession } from '../lib/session';
import type { Machine, SpaceRef } from '../lib/types';
import { AccountMenu, PlainPage } from '../ui/frame';
import { Plus, Server, Settings } from '../ui/icons';
import { MachineRow } from '../ui/machine-row';
import { Msg, Pending } from '../ui/parts';
import { useSpace } from './space';

const STEP_OF = { machines: 'machines', agents: 'agents', access: 'access', done: 'done' } as const;

export const useSignOut = () => {
  const api = useApi();
  const { clear } = useSession();
  return () => {
    void api.auth.logout().finally(() => {
      clear();
      navigate('/signin', { replace: true });
    });
  };
};

export const SpaceMenu = ({ current }: { current?: string }) => {
  const me = useMe();
  const signOut = useSignOut();
  return (
    <AccountMenu
      name={me.user.name || me.user.email}
      spaces={me.spaces.map((s: SpaceRef) => ({ slug: s.slug, name: s.name, current: s.slug === current }))}
      onSignOut={signOut}
    />
  );
};

export const HomeScreen = () => {
  const api = useApi();
  const env = useEnv();
  const { space, live, owner } = useSpace();
  const machines = live.state.machines;
  const [opening, setOpening] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const open = async (m: Machine) => {
    setOpening(m.id);
    setError(null);
    try {
      const { url } = await api.machines.open(m.id);
      env.assign(url);
    } catch (e) {
      setError(errorText(e));
      setOpening(null);
    }
  };

  const renderMachines = () => {
    if (machines === null) {
      return <Pending error={live.error} onRetry={live.reload} rows={2} />;
    }
    if (machines.length === 0) {
      return (
        <EmptyState
          icon={<Server size={28} />}
          title={t('home.empty.title')}
          actions={
            owner ? (
              <Button appearance="primary" onClick={() => navigate(`/s/${space.slug}/setup/machines`)}>
                {t('home.addMachine')}
              </Button>
            ) : undefined
          }
        >
          {owner ? t('home.empty.owner') : t('home.empty.member')}
        </EmptyState>
      );
    }
    return (
      <div className="hub-list">
        {machines.map((m) => (
          <MachineRow
            key={m.id}
            machine={m}
            details
            actions={
              <Button
                size="xs"
                appearance={m.status === 'online' ? 'primary' : 'outline'}
                disabled={m.status !== 'online'}
                loading={opening === m.id}
                onClick={() => void open(m)}
              >
                {t('home.open')}
              </Button>
            }
          />
        ))}
      </div>
    );
  };

  return (
    <PlainPage
      title={space.name}
      side={
        <>
          {owner && (
            <Link to={`/s/${space.slug}/settings`} className="k-btn" data-appearance="ghost" data-size="s">
              <span className="k-btn__content">
                <Settings size={14} />
                <span className="k-btn__label">{t('home.settings')}</span>
              </span>
            </Link>
          )}
          <SpaceMenu current={space.slug} />
        </>
      }
    >
      {owner && space.onboardingStep !== 'done' && (
        <Msg
          tone="info"
          action={
            <Link to={`/s/${space.slug}/setup/${STEP_OF[space.onboardingStep]}`} className="hub-link">
              {t('home.continueSetup')}
            </Link>
          }
        >
          {t('home.setupUnfinished')}
        </Msg>
      )}
      <section className="hub-section">
        <div className="hub-section__h">
          <h2 className="hub-section__t">{t('home.machines')}</h2>
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
        {error && <Msg tone="danger">{error}</Msg>}
      </section>
    </PlainPage>
  );
};
