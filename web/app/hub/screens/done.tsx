// «Всё готово»: what was set up, a way back to each part, and the first chat (the runtime page of a machine that has
// an agent ready, opened through the hub).
import { useEffect, useRef, useState } from 'react';

import { Button } from '@kit';

import { PROVIDER_INFO, PROVIDERS, isReady } from '../lib/agent-view';
import { useApi, useEnv } from '../lib/env';
import { errorText } from '../lib/errors';
import { ownEnvNames } from '../lib/format';
import { t } from '../lib/i18n';
import { navigate } from '../lib/router';
import { useMe } from '../lib/session';
import { StepFrame } from '../ui/frame';
import { Check, MessageSquarePlus, Plus } from '../ui/icons';
import { LinkButton, ListSkeleton, Msg, Note } from '../ui/parts';
import { useGoStep, useSpace } from './space';

export const DoneScreen = () => {
  const api = useApi();
  const env = useEnv();
  const me = useMe();
  const { space, live, setSpace, owner } = useSpace();
  const goStep = useGoStep(`/s/${space.slug}/setup/done`);
  const [opening, setOpening] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const marked = useRef(false);
  const machines = live.state.machines;

  // Reaching this screen is the end of the onboarding; from now on the space opens on its home page.
  useEffect(() => {
    if (marked.current || !owner || space.onboardingStep === 'done') return;
    marked.current = true;
    void api.spaces
      .update(space.id, { onboardingStep: 'done' })
      .then((r) => setSpace(r.space))
      .catch(() => {
        marked.current = false;
      });
  }, [api, owner, space.id, space.onboardingStep, setSpace]);

  const online = (machines ?? []).filter((m) => m.status === 'online');
  const first = online.find((m) => m.agents.some(isReady)) ?? online[0];
  const agents = PROVIDERS.filter((p) =>
    online.some((m) => m.agents.some((a) => a.provider === p && isReady(a))),
  );
  const access = [
    online.some((m) => m.env.includes('GITHUB_TOKEN')) ? 'GitHub' : null,
    online.some((m) => m.env.includes('GITLAB_TOKEN')) ? 'GitLab' : null,
    online.some((m) => m.env.some((n) => !ownEnvNames.includes(n))) ? t('done.secrets') : null,
  ].filter((x): x is string => x !== null);

  const openChat = async () => {
    if (!first) return;
    setOpening(true);
    setError(null);
    try {
      const { url } = await api.machines.open(first.id);
      env.assign(url);
    } catch (e) {
      setError(errorText(e));
      setOpening(false);
    }
  };

  const rows = [
    { k: t('done.account'), v: me.user.email, step: 0 },
    { k: t('done.space'), v: space.name || t('done.noName'), step: 1 },
    { k: t('done.machines'), v: online.map((m) => m.name).join(', ') || t('done.noMachines'), step: 2 },
    {
      k: t('done.agents'),
      v: agents.map((p) => PROVIDER_INFO[p].name).join(', ') || t('done.noAgents'),
      step: 3,
    },
    { k: t('done.access'), v: access.join(', ') || t('done.skipped'), step: 4 },
  ];

  return (
    <StepFrame
      step={5}
      icon={<Check size={30} />}
      title={t('done.title')}
      lead={t('done.lead')}
      goStep={goStep}
      foot={
        <>
          <span className="hub-spacer" />
          <Button
            icon={<Plus size={16} />}
            size="l"
            onClick={() => navigate(`/s/${space.slug}/setup/machines`)}
          >
            {t('done.moreMachine')}
          </Button>
          <Button
            appearance="primary"
            size="l"
            icon={<MessageSquarePlus size={16} />}
            loading={opening}
            aria-disabled={!first || undefined}
            onClick={() => void openChat()}
          >
            {t('done.openChat')}
          </Button>
        </>
      }
    >
      {machines === null ? (
        <ListSkeleton rows={5} />
      ) : (
        <ul className="hub-sum">
          {rows.map((r) => (
            <li key={r.step}>
              <span className="hub-sum__k">{r.k}</span>
              <span className="hub-sum__v">{r.v}</span>
              <LinkButton onClick={() => goStep(r.step)}>{t('done.change')}</LinkButton>
            </li>
          ))}
        </ul>
      )}
      {machines !== null && !first && <Note>{t('done.noChat')}</Note>}
      {error && <Msg tone="danger">{error}</Msg>}
    </StepFrame>
  );
};
