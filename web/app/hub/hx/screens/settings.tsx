// The «Организация» screen of the new hub (admin): the organization's own settings. For now one: the rule for apps on
// its machines (HRN-915). The switch changes after the hub answered, and the machines get the rule with their list.
// Its own chunk: it loads when the person opens the section.
import { useEffect, useState } from 'react';

import type { AppsConsent } from '../../../hub-api/orgs';
import { orgErrorText } from '../../org/api/errors';
import { useOrg } from '../../org/context';
import { Titles } from '../shell';

type Loaded = { value: AppsConsent } | { error: string };

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
      <Titles title="Организация" lead={shell.org.name} />
      <div className="hx-set__group">
        <div className="hx-set__group-title">Приложения</div>
        <div className="hx-set__card">
          <div className="hx-set__row" data-busy={busy ? 'true' : undefined}>
            <span className="hx-set__row-text">
              <span className="hx-set__row-label" id="apps-consent-label">
                Не спрашивать разрешений у приложений
              </span>
              <span className="hx-set__row-hint">
                Приложения на машинах организации получат все права, которые просят, без вопроса. Уже выданное
                при выключении остаётся.
              </span>
            </span>
            <span className="hx-set__spin">
              <i className="hx-icon" data-i="loader-circle" data-size="sm" aria-hidden="true" />
            </span>
            {value !== null && (
              <button
                type="button"
                className="hx-switch"
                role="switch"
                aria-checked={value === 'auto' ? 'true' : 'false'}
                aria-labelledby="apps-consent-label"
                onClick={toggle}
              >
                <i />
              </button>
            )}
          </div>
        </div>
        {loaded && 'error' in loaded && <div className="hx-set__foot">{loaded.error}</div>}
        {failed && <div className="hx-set__foot">{failed}</div>}
      </div>
    </>
  );
};

export default Screen;
