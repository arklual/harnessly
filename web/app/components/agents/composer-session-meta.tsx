import { useId, useRef, useState } from 'react';
import {
  POPOVER_SIDE,
  TOOLTIP_SIDE,
  TwMeter,
  TwPopover,
  TwTooltip,
  METER_TONE,
  twPopoverTriggerProps,
} from '@/components/agents/ui';
import { SlidersHorizontal } from 'lucide-react';
import { SessionLaunchControls, SessionLaunchPicker } from '@/components/agents/launch-options-picker';
import BackgroundChip, { BackgroundTasksSection } from '@/components/agents/background-chip';
import ComposerLimits, {
  ComposerLimitsDetail,
  limitsTitle,
  useComposerLimits,
} from '@/components/agents/composer-limits';
import { SessionMetaDetail } from '@/components/workspace/session-meta-bar';
import { tabIdOfSession } from '@/hooks/use-background-tasks';
import useTabStore, { type ISessionMetaCache } from '@/hooks/use-tab-store';
import useHlEmbedded from '@/hooks/use-hl-embedded';
import { t } from '@/lib/i18n';

const RING_RADIUS = 6;

const CONTEXT_LEVEL = {
  unknown: 'unknown',
  low: 'low',
  mid: 'mid',
  high: 'high',
} as const;

const contextLevel = (pct: number | null) => {
  if (pct === null) return CONTEXT_LEVEL.unknown;
  if (pct >= 80) return CONTEXT_LEVEL.high;
  if (pct >= 55) return CONTEXT_LEVEL.mid;
  return CONTEXT_LEVEL.low;
};

const formatTokens = (n: number) => (n >= 1000 ? `${Math.round(n / 1000)}k` : String(n));

const contextText = (cache: ISessionMetaCache, pct: number | null) => {
  const { currentContextTokens, contextWindowSize } = cache.meta;
  const share = pct === null ? t('agents.composerSessionMeta.unknown') : `${pct}%`;
  if (currentContextTokens == null || !contextWindowSize)
    return t('agents.composerSessionMeta.context', { share });
  return t('agents.composerSessionMeta.contextTokens', {
    share,
    used: formatTokens(currentContextTokens),
    total: formatTokens(contextWindowSize),
  });
};

interface IContextRingProps {
  cache: ISessionMetaCache;
  sessionName: string;
}

/** Context fill as a ring with its percentage, like Claude Code's footer; opens the session details. */
const ContextRing = ({ cache, sessionName }: IContextRingProps) => {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();
  const { usedPercentage } = cache.meta;
  const pct = usedPercentage == null ? null : Math.max(0, Math.min(100, Math.round(usedPercentage)));
  const summary = contextText(cache, pct);

  return (
    <>
      <TwTooltip content={summary} side={TOOLTIP_SIDE.top}>
        <button
          ref={triggerRef}
          type="button"
          className="tw-context-ring"
          data-level={contextLevel(pct)}
          aria-label={t('agents.composerSessionMeta.ringLabel', { summary })}
          {...twPopoverTriggerProps(open, popoverId)}
          onClick={() => setOpen((value) => !value)}
        >
          <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">
            <circle className="tw-context-ring-track" cx="8" cy="8" r={RING_RADIUS} />
            <circle
              className="tw-context-ring-fill"
              cx="8"
              cy="8"
              r={RING_RADIUS}
              pathLength={100}
              strokeDasharray={`${pct ?? 0} 100`}
              transform="rotate(-90 8 8)"
            />
          </svg>
          <span>{pct === null ? '—' : `${pct}%`}</span>
        </button>
      </TwTooltip>
      <TwPopover
        id={popoverId}
        open={open}
        onClose={() => setOpen(false)}
        anchorRef={triggerRef}
        side={POPOVER_SIDE.top}
        align="end"
        title={t('agents.composerSessionMeta.session')}
        className="tw-context-pop"
      >
        <div className="tw-context-pop-meter">
          <TwMeter
            value={pct ?? 0}
            max={100}
            label={t('agents.composerSessionMeta.contextFill')}
            tone={METER_TONE.auto}
            warnAt={0.55}
            dangerAt={0.8}
          />
          <span>{summary}</span>
        </div>
        {open && (
          <SessionMetaDetail
            meta={cache.meta}
            sessionName={sessionName}
            sessionId={cache.sessionId}
            jsonlPath={cache.jsonlPath}
          />
        )}
      </TwPopover>
    </>
  );
};

interface ISessionSheetProps {
  cache: ISessionMetaCache;
  sessionName: string;
  provider: 'claude' | 'codex';
}

const LIMIT_ALERT = { low: 'low', out: 'out' } as const;

/**
 * Everything about the session behind one button for a narrow composer, like ChatGPT's
 * «Configure»: model and effort to change, context, limits and background work to glance at. A dot on the button
 * says a limit runs low.
 */
const SessionSheet = ({ cache, sessionName, provider }: ISessionSheetProps) => {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();
  const limits = useComposerLimits(provider);
  const { usedPercentage } = cache.meta;
  const pct = usedPercentage == null ? null : Math.max(0, Math.min(100, Math.round(usedPercentage)));
  const summary = contextText(cache, pct);
  const alert =
    limits.level === LIMIT_ALERT.low || limits.level === LIMIT_ALERT.out ? limits.level : undefined;
  const label = [summary, limits.level ? limits.summary : null].filter(Boolean).join('. ');

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className="tw-cbtn tw-csettings"
        data-level={contextLevel(pct)}
        data-alert={alert}
        aria-label={t('agents.composerSessionMeta.settingsLabel', { label })}
        title={label}
        {...twPopoverTriggerProps(open, popoverId)}
        onClick={() => setOpen((value) => !value)}
      >
        <SlidersHorizontal aria-hidden="true" />
      </button>
      <TwPopover
        id={popoverId}
        open={open}
        onClose={() => setOpen(false)}
        anchorRef={triggerRef}
        side={POPOVER_SIDE.top}
        align="start"
        title={t('agents.composerSessionMeta.session')}
        className="tw-cgauge-pop tw-csheet"
      >
        <SessionLaunchControls
          provider={provider}
          sessionName={sessionName}
          model={cache.meta.model}
          effort={cache.meta.effort}
          onLeave={() => setOpen(false)}
        />
        <section className="tw-csheet-section">
          <div className="tw-context-pop-meter">
            <TwMeter
              value={pct ?? 0}
              max={100}
              label={t('agents.composerSessionMeta.contextFill')}
              tone={METER_TONE.auto}
              warnAt={0.55}
              dangerAt={0.8}
            />
            <span>{summary}</span>
          </div>
        </section>
        <BackgroundTasksSection tabId={tabIdOfSession(sessionName)} />
        {limits.level && (
          <section className="tw-csheet-section">
            <h3 className="tw-csheet-heading">{limitsTitle(limits)}</h3>
            <ComposerLimitsDetail data={limits} />
          </section>
        )}
        {open && (
          <details className="tw-csheet-section tw-csheet-details">
            <summary>{t('agents.composerSessionMeta.details')}</summary>
            <SessionMetaDetail
              meta={cache.meta}
              sessionName={sessionName}
              sessionId={cache.sessionId}
              jsonlPath={cache.jsonlPath}
            />
          </details>
        )}
      </TwPopover>
    </>
  );
};

interface IComposerSessionMetaProps {
  tabId?: string;
  sessionName?: string;
  provider: 'claude' | 'codex';
}

const useSessionMeta = (tabId: string | undefined, sessionName: string | undefined) => {
  const embedded = useHlEmbedded();
  const cache = useTabStore((s) => (tabId ? (s.tabs[tabId]?.sessionMetaCache ?? null) : null));
  return embedded && sessionName && cache ? { cache, sessionName } : null;
};

/**
 * The session's state in the composer row of the Hl workspace: background tasks, context
 * and limits as chips where the composer is wide, one settings button where it is not.
 */
export const ComposerSessionStatus = ({ tabId, sessionName, provider }: IComposerSessionMetaProps) => {
  const session = useSessionMeta(tabId, sessionName);
  if (!session) return null;
  return (
    <div className="tw-cstatus">
      <BackgroundChip tabId={tabIdOfSession(session.sessionName)} />
      <span className="tw-cstatus-wide">
        <ContextRing cache={session.cache} sessionName={session.sessionName} />
        <ComposerLimits provider={provider} />
      </span>
      <span className="tw-cstatus-compact">
        <SessionSheet cache={session.cache} sessionName={session.sessionName} provider={provider} />
      </span>
    </div>
  );
};

/** Model and effort of the session, beside the microphone. */
export const ComposerSessionModel = ({ tabId, sessionName, provider }: IComposerSessionMetaProps) => {
  const session = useSessionMeta(tabId, sessionName);
  if (!session) return null;
  return (
    <SessionLaunchPicker
      provider={provider}
      sessionName={session.sessionName}
      model={session.cache.meta.model}
      effort={session.cache.meta.effort}
      side={POPOVER_SIDE.top}
    />
  );
};
