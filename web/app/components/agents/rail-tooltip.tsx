import type { ReactElement } from 'react';
import { TOOLTIP_SIDE, TwKbd, TwTooltip } from '@/components/agents/ui';
import StatusDot from '@/components/agents/status-dot';
import type { IRailTooltip } from '@/lib/hl-rail-tooltip';

const SHOW_DELAY_MS = 300;

const RailTooltipBody = ({ tip }: { tip: IRailTooltip }) => (
  <div className="tw-rail-tip">
    <div className="tw-rail-tip-head">
      <strong className="tw-rail-tip-title">{tip.title}</strong>
      {tip.shortcut && <TwKbd>{tip.shortcut}</TwKbd>}
    </div>
    {tip.status && (
      <div className="tw-rail-tip-status">
        <StatusDot tone={tip.status.tone} inline />
        {tip.status.label}
      </div>
    )}
    {tip.description && <p className="tw-rail-tip-text">{tip.description}</p>}
    {tip.facts.length > 0 && (
      <dl className="tw-rail-tip-facts">
        {tip.facts.map((fact) => (
          <div key={fact.label} className="tw-rail-tip-fact">
            <dt>{fact.label}</dt>
            <dd>{fact.value}</dd>
          </div>
        ))}
      </dl>
    )}
  </div>
);

/** What a button of the collapsed rail is: name, explanation, hotkey, and for a conversation its state and whereabouts. */
const RailTooltip = ({
  tip,
  children,
}: {
  tip: IRailTooltip;
  children: ReactElement<{ 'aria-describedby'?: string }>;
}) => (
  <TwTooltip
    content={tip.text}
    bubble={<RailTooltipBody tip={tip} />}
    side={TOOLTIP_SIDE.right}
    delay={SHOW_DELAY_MS}
  >
    {children}
  </TwTooltip>
);

export default RailTooltip;
