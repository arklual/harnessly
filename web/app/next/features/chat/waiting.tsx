// The line under a finished answer while the agent waits for its background agents. It is drawn as the tail of the
// live turn (the same rail as «Now»): a stack of small faces, one for each agent still running — the name on hover,
// a click opens that agent in the right pane — and «Ждёт 5 фоновых агентов», a click on which shows them all. A dynamic
// workflow is a face of its own and its own words: «Ждёт workflow», or «Ждёт 2 фоновых агентов и workflow».
import { plural, t } from '@/next/i18n';
import { openSubagent } from '@/next/features/subagents/open';
import type { BackgroundTask } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import Tooltip from '@/next/ui/tooltip';
import '@/next/styles/background.css';

/** More agents than this are one more face, «+3». */
const FACES = 4;

export interface IWaitingLineProps {
  /** The background agents still running. */
  agents: readonly BackgroundTask[];
  /** Shows the right column with all of them. */
  onOpen: () => void;
}

/** What a face says on hover: the agent's name and its kind. */
const nameOf = (agent: BackgroundTask): string =>
  agent.role && agent.role !== agent.title ? `${agent.title} · ${agent.role}` : agent.title;

/** «Ждёт 2 фоновых агентов», «Ждёт workflow», «Ждёт 2 фоновых агентов и workflow». */
export const waitingLabel = (agents: readonly BackgroundTask[]): string => {
  const flows = agents.filter((x) => x.kind === 'workflow').length;
  const subs = agents.length - flows;
  if (flows === 0) return plural(subs, 'next.bg.waiting');
  if (subs === 0) return flows === 1 ? t('next.bg.waitingFlow') : plural(flows, 'next.bg.waitingFlows');
  return t('next.bg.waitingBoth', {
    agents: plural(subs, 'next.bg.waiting'),
    flows: plural(flows, 'next.bg.flowsCount'),
  });
};

const WaitingLine = ({ agents, onOpen }: IWaitingLineProps) => {
  const shown = agents.slice(0, FACES);
  const more = agents.length - shown.length;
  return (
    <div className="hx-now hx-wait" role="status">
      <div className="hx-now__rail">
        <div className="hx-now__step">
          <span className="hx-wait__faces">
            {shown.map((agent) => (
              <Tooltip key={agent.id} label={nameOf(agent)}>
                <button
                  type="button"
                  className="hx-wait__face"
                  aria-label={nameOf(agent)}
                  onClick={() => openSubagent(agent.id)}
                >
                  {agent.kind === 'workflow' ? (
                    <Icon name="layers" size="sm" />
                  ) : (
                    <i className="hx-agent" data-agent="claude" aria-hidden="true" />
                  )}
                </button>
              </Tooltip>
            ))}
            {more > 0 && (
              <span className="hx-wait__face" data-more="true" aria-hidden="true">
                +{more}
              </span>
            )}
          </span>
          <Tooltip label={t('next.bg.waitingTip')}>
            <button type="button" className="hx-wait__label" onClick={onOpen}>
              {waitingLabel(agents)}
            </button>
          </Tooltip>
        </div>
      </div>
    </div>
  );
};

export default WaitingLine;
