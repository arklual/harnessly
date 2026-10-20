import { useMemo, useState } from 'react';
import { useEfficiency } from '@/hooks/use-efficiency';
import type { IEvalRuns, TEffScope, TEffSummaryTab } from '@/types/efficiency';
import SummaryTab from '@/components/agents/efficiency/summary-tab';
import SkillsTab from '@/components/agents/efficiency/skills-tab';
import ChangesTab from '@/components/agents/efficiency/changes-tab';
import ComponentsTab from '@/components/agents/efficiency/components-tab';
import HypothesesTab from '@/components/agents/efficiency/hypotheses-tab';
import SessionsTab from '@/components/agents/efficiency/sessions-tab';
import RunsTab from '@/components/agents/efficiency/runs-tab';
import { Segmented } from '@/components/agents/efficiency/primitives';
import { mineOpenAlerts } from '@/components/agents/efficiency/runs-math';
import { t } from '@/lib/i18n';

const TAB = {
  summary: 'summary',
  skills: 'skills',
  changes: 'changes',
  components: 'components',
  hypotheses: 'hypotheses',
  sessions: 'sessions',
  runs: 'runs',
} as const;
type TTab = (typeof TAB)[keyof typeof TAB];

const TAB_LABELS: Record<TTab, string> = {
  summary: t('efficiency.efficiencyView.tabSummary'),
  skills: t('efficiency.efficiencyView.tabSkills'),
  changes: t('efficiency.efficiencyView.tabChanges'),
  components: t('efficiency.efficiencyView.tabComponents'),
  hypotheses: t('efficiency.efficiencyView.tabHypotheses'),
  sessions: t('efficiency.efficiencyView.tabSessions'),
  runs: t('efficiency.efficiencyView.tabRuns'),
};

const SCOPES: { value: TEffScope; label: string }[] = [
  { value: 'me', label: t('efficiency.efficiencyView.scopeMe') },
  { value: 'team', label: t('efficiency.efficiencyView.scopeTeam') },
];

/** Tabs that read aggregate data and so can be switched between "mine" and the whole team. */
const SCOPED_TABS: TTab[] = [TAB.summary, TAB.skills, TAB.changes, TAB.components];

const EfficiencyView = () => {
  const [tab, setTab] = useState<TTab>(TAB.summary);
  const [scope, setScope] = useState<TEffScope>('me');
  const [hypothesisId, setHypothesisId] = useState<string | null>(null);
  const runs = useEfficiency<IEvalRuns>('/runs');
  const mine = useMemo(() => mineOpenAlerts(runs.data), [runs.data]);

  const openHypothesis = (id: string | null) => {
    setHypothesisId(id);
    if (id) setTab(TAB.hypotheses);
  };

  const openFromSummary = (target: TEffSummaryTab, id?: string) => {
    if (target === 'hypotheses') return openHypothesis(id ?? null);
    setTab(target);
  };

  return (
    <div className="ts-root">
      <div className="ts-head">
        <div>
          <h1 className="ts-title">{t('efficiency.efficiencyView.title')}</h1>
          <p className="ts-sub">{t('efficiency.efficiencyView.sub')}</p>
        </div>
      </div>
      <div className="ts-filters">
        <Segmented
          label={t('efficiency.efficiencyView.screen')}
          value={tab}
          options={(Object.keys(TAB_LABELS) as TTab[]).map((value) => ({
            value,
            label: TAB_LABELS[value],
            dot: value === TAB.runs && mine.length > 0,
          }))}
          onChange={setTab}
        />
        {SCOPED_TABS.includes(tab) && (
          <Segmented
            label={t('efficiency.efficiencyView.whoseData')}
            value={scope}
            options={SCOPES}
            onChange={setScope}
          />
        )}
      </div>
      {tab === TAB.summary && <SummaryTab scope={scope} onOpen={openFromSummary} />}
      {tab === TAB.skills && <SkillsTab scope={scope} />}
      {tab === TAB.changes && <ChangesTab scope={scope} alerts={mine} onOpenRuns={() => setTab(TAB.runs)} />}
      {tab === TAB.components && <ComponentsTab scope={scope} onOpenHyp={openHypothesis} />}
      {tab === TAB.hypotheses && <HypothesesTab openId={hypothesisId} onOpen={setHypothesisId} />}
      {tab === TAB.sessions && <SessionsTab />}
      {tab === TAB.runs && <RunsTab resource={runs} />}
    </div>
  );
};

export default EfficiencyView;
