// «Агент в браузере» (Settings → General): whether the agent asks before its first action on a site in the pane's
// browser, and the sites the person answered for, each of which can be forgotten (the agent then asks there again).
// Kept in this browser (stores/browser-agent.ts).
import { Group, Row, SetButton, SwitchRow } from '@/next/features/settings/rows';
import type { TSiteDecision } from '@/next/stores/browser-agent';
import { t } from '@/next/i18n';

interface IBrowserAgentGroupProps {
  ask: boolean;
  sites: Readonly<Record<string, TSiteDecision>>;
  onAsk: (ask: boolean) => void;
  onForget: (host: string) => void;
}

export const BrowserAgentGroup = ({ ask, sites, onAsk, onForget }: IBrowserAgentGroupProps) => {
  const hosts = Object.keys(sites).sort();
  return (
    <Group title={t('next.settings.browserAgent')} foot={t('next.settings.browserAgent.foot')}>
      <SwitchRow
        label={t('next.settings.browserAgent.ask')}
        hint={t('next.settings.browserAgent.ask.hint')}
        on={ask}
        onChange={onAsk}
      />
      {hosts.map((host) => (
        <Row
          key={host}
          type="link"
          label={host}
          hint={
            sites[host] === 'allow'
              ? t('next.settings.browserAgent.allowed')
              : t('next.settings.browserAgent.denied')
          }
        >
          <SetButton tone="ghost" onClick={() => onForget(host)}>
            {t('next.settings.browserAgent.forget')}
          </SetButton>
        </Row>
      ))}
    </Group>
  );
};

export default BrowserAgentGroup;
