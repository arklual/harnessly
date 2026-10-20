import { useEffect, useEffectEvent, useId, useState } from 'react';
import { BUTTON_VARIANT, TwButton, TwDialog } from '@/components/agents/ui';
import { appFetch, currentMachineId } from '@/lib/app-path';
import { readPreview } from '@/lib/hl-preview-store';
import { runAgentLogin } from '@/lib/hl-vault-agent';
import {
  approveVaultEntry,
  denyVaultEntry,
  fetchVaultEntries,
  fetchVaultStatus,
  mintFillToken,
} from '@/lib/hl-vault-client';
import { loginInPreview } from '@/lib/hl-vault-fill';
import {
  VAULT_REQUEST_EVENT,
  vaultLoginOrigin,
  type IVaultLoginResult,
  type IVaultRequestEvent,
} from '@/lib/hl-vault-request';
import { sameVaultOrigin } from '@/lib/vault/vault-origin';
import { VAULT_GRANT_HOURS, type IVaultEntry } from '@/types/vault';
import { plural, t } from '@/lib/i18n';

const ID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
/** The runtime stops waiting at 120 s; the question goes away a little earlier, unanswered. */
const ASK_FOR_MS = 110_000;

const isRequest = (value: unknown): value is IVaultRequestEvent =>
  typeof value === 'object' &&
  value !== null &&
  typeof (value as IVaultRequestEvent).id === 'string' &&
  typeof (value as IVaultRequestEvent).tabId === 'string';

const report = async (id: string, result: IVaultLoginResult): Promise<void> => {
  try {
    await appFetch('/api/platform/vault-result', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ id, ...result }),
    });
  } catch {
    // The agent hears about a lost answer as needs-approval.
  }
};

interface IQuestion {
  origin: string;
  entries: IVaultEntry[];
  answer: (entry: IVaultEntry | null) => void;
}

const Question = ({ question }: { question: IQuestion }) => {
  const [chosen, setChosen] = useState(question.entries[0].id);
  const name = useId();
  const entry = question.entries.find((item) => item.id === chosen) ?? question.entries[0];
  const single = question.entries.length === 1;

  useEffect(() => {
    const timer = window.setTimeout(() => question.answer(null), ASK_FOR_MS);
    return () => window.clearTimeout(timer);
  }, [question]);

  return (
    <TwDialog
      open
      role="alertdialog"
      onClose={() => question.answer(null)}
      title={t('vault.vaultAgentRequests.title')}
      description={
        <>
          {t('vault.vaultAgentRequests.on')}{' '}
          <strong className="tw-vault-ask-origin">{question.origin}</strong>
          {single && (
            <>
              {' '}
              {t('vault.vaultAgentRequests.as')} <strong>{entry.login}</strong>
            </>
          )}
          {t('vault.vaultAgentRequests.explain', {
            hours: plural(VAULT_GRANT_HOURS, 'vault.vaultAgentRequests.hours'),
          })}
        </>
      }
      className="tw-vault-ask"
      footer={
        <>
          <TwButton variant={BUTTON_VARIANT.ghost} onClick={() => question.answer(null)}>
            {t('vault.vaultAgentRequests.deny')}
          </TwButton>
          <TwButton variant={BUTTON_VARIANT.primary} onClick={() => question.answer(entry)}>
            {single
              ? t('vault.vaultAgentRequests.allow')
              : t('vault.vaultAgentRequests.logInAs', { login: entry.login })}
          </TwButton>
        </>
      }
    >
      {!single && (
        <fieldset className="tw-vault-ask-logins">
          <legend className="tw-sform-label">{t('vault.vaultAgentRequests.whichLogin')}</legend>
          {question.entries.map((item) => (
            <label key={item.id} className="tw-vault-ask-login">
              <input
                type="radio"
                name={name}
                value={item.id}
                checked={item.id === chosen}
                onChange={() => setChosen(item.id)}
              />
              <span>{item.login}</span>
              {item.label && <span className="tw-vault-muted">{item.label}</span>}
            </label>
          ))}
        </fieldset>
      )}
    </TwDialog>
  );
};

/**
 * Answers `vault login` of the agent CLI of the agent of the tab on screen: asks the person unless they already allowed
 * this agent tab, logs in through the tab's browser and tells the runtime only how it went.
 */
const VaultAgentRequests = ({ tabId }: { tabId: string | null }) => {
  const [question, setQuestion] = useState<IQuestion | null>(null);

  const handle = useEffectEvent(async (event: IVaultRequestEvent) => {
    if (!tabId || event.tabId !== tabId || !ID_RE.test(event.id)) return;
    const origin = vaultLoginOrigin(event.origin);
    if (!origin)
      return report(event.id, { status: 'failed', message: t('vault.vaultAgentRequests.badOrigin') });
    const agent = `${currentMachineId() ?? 'local'}/${tabId}`;
    const result = await runAgentLogin(origin, {
      agent,
      status: fetchVaultStatus,
      entries: fetchVaultEntries,
      previewShows: (site) => {
        const { state } = readPreview(tabId);
        return (
          state.kind === 'open' && state.target.kind === 'url' && sameVaultOrigin(state.target.origin, site)
        );
      },
      login: (site, mint) => loginInPreview(tabId, site, mint),
      mint: (id, purpose, fill) => mintFillToken(id, origin, purpose, fill),
      approve: (id, key) => approveVaultEntry(id, origin, key),
      deny: denyVaultEntry,
      ask: (site, entries) =>
        new Promise((resolve) => {
          setQuestion({
            origin: site,
            entries,
            answer: (entry) => {
              setQuestion(null);
              resolve(entry);
            },
          });
        }),
    });
    await report(event.id, result);
  });

  useEffect(() => {
    if (!tabId) return;
    const onRequest = (event: Event) => {
      const detail = (event as CustomEvent<unknown>).detail;
      if (isRequest(detail)) void handle(detail);
    };
    window.addEventListener(VAULT_REQUEST_EVENT, onRequest);
    return () => window.removeEventListener(VAULT_REQUEST_EVENT, onRequest);
  }, [tabId]);

  return question ? <Question question={question} /> : null;
};

export default VaultAgentRequests;
