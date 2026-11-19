// The «Настройки организации» screen of the new hub (admin): the organization's own settings. The rule for apps on
// its machines, members' own machines and the links for the chat. The switch changes after the hub answered, and the machines get the rule with their list.
// Its own chunk: it loads when the person opens the section.
import { useEffect, useState } from 'react';

import type { AppsConsent } from '../../../hub-api/orgs';
import { orgErrorText } from '../../org/api/errors';
import { useOrg } from '../../org/context';
import { Titles } from '../shell';
import { LinksSection } from './links-section';

type Loaded = { value: AppsConsent } | { error: string };

// MemberMachines is «Участники подключают свои машины»: on, a member connects a machine of their own and it waits for
// an administrator's trust; off, only those who manage a tenant connect machines.
const MemberMachines = () => {
  const { api, shell, refreshShell } = useOrg();
  const [on, setOn] = useState(shell.org.memberMachines !== false);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);
  const toggle = () => {
    if (busy) return;
    setBusy(true);
    setFailed(null);
    api.setMemberMachines(shell.org.slug, !on).then(
      (next) => {
        setOn(next);
        setBusy(false);
        refreshShell();
      },
      (e: unknown) => {
        setFailed(orgErrorText(e));
        setBusy(false);
      },
    );
  };
  return (
    <>
      <div className="set" data-busy={busy ? 'true' : undefined}>
        <div>
          <div className="hd-t" id="member-machines-label">
            Участники подключают свои машины
          </div>
          <div className="hd-d">
            Участник сам подключает свою машину, а работать на ней можно после подтверждения администратора.
            Выключите, чтобы машины подключали только администраторы. Уже подключённые машины остаются.
          </div>
        </div>
        <div>
          <button
            type="button"
            className="hx-switch"
            role="switch"
            aria-checked={on ? 'true' : 'false'}
            aria-labelledby="member-machines-label"
            disabled={busy}
            onClick={toggle}
          >
            <i />
          </button>
        </div>
      </div>
      {failed && <p className="mut sm">{failed}</p>}
    </>
  );
};

const Screen = () => {
  const { api, shell } = useOrg();
  const org = shell.org.slug;
  // null while it loads: a switch drawn before the answer would show a rule the hub may not have.
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    api.appsConsent(org).then(
      (value) => live && setLoaded({ value }),
      (e: unknown) => live && setLoaded({ error: orgErrorText(e) }),
    );
    return () => {
      live = false;
    };
  }, [api, org]);

  const value = loaded && 'value' in loaded ? loaded.value : null;
  const toggle = () => {
    if (value === null || busy) return;
    setBusy(true);
    setFailed(null);
    api.setAppsConsent(org, value === 'auto' ? 'ask' : 'auto').then(
      (next) => {
        setLoaded({ value: next });
        setBusy(false);
      },
      (e: unknown) => {
        setFailed(orgErrorText(e));
        setBusy(false);
      },
    );
  };

  return (
    <>
      <div className="hb-head hd-head">
        <Titles
          title="Настройки организации"
          lead={`${shell.org.name} · машины и чаты, ссылки в чатах`}
          scope={false}
        />
      </div>
      <div style={{ maxWidth: 1100 }}>
        <h2 className="hd-sech is-first" data-sec="chats">
          Машины и чаты
          <span className="mut2 sm">Кто подключает машины и что можно приложениям на них.</span>
        </h2>
        <div className="t-card viz-card dash-tile hd-card flat">
          <div className="set" data-busy={busy ? 'true' : undefined}>
            <div>
              <div className="hd-t" id="apps-consent-label">
                Не спрашивать разрешений у приложений
              </div>
              <div className="hd-d">
                Приложения на машинах организации получат все права, которые просят, без вопроса. Уже выданное
                при выключении остаётся.
              </div>
            </div>
            <div>
              {value === null ? (
                <span className="hx-sk" style={{ display: 'inline-block', width: 40, height: 24 }} />
              ) : (
                <button
                  type="button"
                  className="hx-switch"
                  role="switch"
                  aria-checked={value === 'auto' ? 'true' : 'false'}
                  aria-labelledby="apps-consent-label"
                  disabled={busy}
                  onClick={toggle}
                >
                  <i />
                </button>
              )}
            </div>
          </div>
          <MemberMachines />
        </div>
        {loaded && 'error' in loaded && (
          <p className="mut sm" style={{ marginTop: 8 }}>
            {loaded.error}
          </p>
        )}
        {failed && (
          <p className="mut sm" style={{ marginTop: 8 }}>
            {failed}
          </p>
        )}
        <LinksSection />
      </div>
    </>
  );
};

export default Screen;
