import { useEffect, useRef, type CSSProperties } from 'react';
import '@/next/styles/health.css';
import Icon from '@/next/ui/icon';
import { SheetGrip, useSheetDrag } from '@/next/ui/sheet';
import { usePhone } from '@/next/ui/use-media';
import { t } from '@/next/i18n';
import { PENDING, type TPending } from '@/next/ui/use-pending-list';
import { useFitInViewport, useHoverIntent } from '@/next/ui/use-hover-intent';
import { MACHINE_HEALTH, type IMachineCard, type IMachineChoice } from '@/next/features/sidebar/types';

const HEALTH_LABEL = {
  [MACHINE_HEALTH.ok]: t('next.machine.ok'),
  [MACHINE_HEALTH.warn]: t('next.machine.warn'),
  [MACHINE_HEALTH.critical]: t('next.machine.critical'),
  [MACHINE_HEALTH.bad]: t('next.machine.bad'),
  [MACHINE_HEALTH.off]: t('next.machine.off'),
} as const;

export interface IMachineCardProps {
  /** null while the machines load: the chip is there, its card says so. */
  machine: IMachineCard | null;
  others: readonly IMachineChoice[];
  /** A switch to another machine waits for the server. */
  switching: { id: string; pending: TPending } | null;
  onPick?: (id: string) => void;
  onManage?: () => void;
  /** Each new value opens the card pinned (the banner's «Подробнее», a notification); 0 opens nothing. */
  reveal?: number;
}

// A meter's bar length and its place in the cascade are geometry: the one thing the shell writes into `style`.
const meterStyle = (ratio: number, index: number) =>
  ({ '--v': Math.min(1, Math.max(0, ratio)), '--j': index }) as CSSProperties;

/**
 * The machine at the bottom of the sidebar: a server icon with a health dot. Hover (or focus) shows its card with the
 * load and what is wrong, and keeps it while the pointer travels to it (ui/use-hover-intent.ts); a click pins the card
 * and opens the other machines under it, a second click lets it go. On the phone a tap opens the card as a bottom sheet
 * (ui/sheet.tsx) at the foot of the drawer: a tap outside or a swipe down closes it.
 */
const MachineCard = ({ machine, others, switching, onPick, onManage, reveal = 0 }: IMachineCardProps) => {
  const {
    open: shown,
    pinned,
    setTrigger,
    setFloating,
    getFloating,
    triggerProps,
    floatingProps,
    close,
  } = useHoverIntent();
  useFitInViewport(getFloating, shown);
  const phone = usePhone();
  useSheetDrag(getFloating, close, phone && shown);
  const revealed = useRef(reveal);
  useEffect(() => {
    if (reveal === revealed.current) return;
    revealed.current = reveal;
    if (!pinned) triggerProps.onClick();
  }, [reveal, pinned, triggerProps]);
  const health = machine?.health ?? MACHINE_HEALTH.off;
  const label = machine
    ? t('next.machine.aria', { name: machine.name, health: HEALTH_LABEL[health] })
    : t('next.machine.loading');

  return (
    <div className="hx-machine" data-health={health} data-hover={shown} data-open={pinned}>
      <button
        ref={setTrigger}
        type="button"
        className="hx-machine__chip"
        aria-label={label}
        aria-expanded={shown}
        {...triggerProps}
      >
        <Icon name="server" size="sm" />
        <span className="hx-machine__dot" />
      </button>
      <div
        ref={setFloating}
        className="hx-machine__pop hx-hover-fit hx-sheet-phone"
        role="dialog"
        aria-label={label}
        inert={!shown}
        {...floatingProps}
      >
        <SheetGrip />
        <div className="hx-machine__head">
          <span className="hx-machine__title">{machine?.name ?? t('next.machine.loading')}</span>
          {others.length > 0 && <span className="hx-machine__more">{t('next.machine.clickForAll')}</span>}
        </div>
        {machine?.issue && (
          <div className="hx-machine__issue">
            <Icon name="triangle-alert" size="sm" />
            <span>
              <b>{machine.issue.title}</b> {machine.issue.text}
            </span>
          </div>
        )}
        {machine?.meters && (
          <div className="hx-machine__rows">
            {machine.meters.map((meter, index) => (
              <div
                key={meter.id}
                className="hx-machine__row"
                data-tone={meter.tone}
                style={meterStyle(meter.ratio, index)}
              >
                <Icon name={meter.icon} size="sm" />
                <span className="hx-machine__label">{meter.label}</span>
                <span className="hx-machine__bar">
                  <i />
                </span>
                <span className="hx-machine__val">{meter.value}</span>
                {meter.reason && <span className="hx-machine__why">{meter.reason}</span>}
              </div>
            ))}
          </div>
        )}
        {machine && machine.meters === null && health !== MACHINE_HEALTH.off && (
          <p className="hx-machine__wait">{t('next.machine.loadWaiting')}</p>
        )}
        {machine && machine.notes.length > 0 && (
          <div className="hx-machine__foot">
            {machine.notes.map((note) => (
              <span key={note}>{note}</span>
            ))}
          </div>
        )}
        <div className="hx-machine__others">
          <div className="hx-machine__others-in" inert={!pinned}>
            {others.length > 0 && <div className="hx-machine__others-label">{t('next.machine.others')}</div>}
            {others.map((other) => {
              const busy = switching?.id === other.id;
              return (
                <button
                  key={other.id}
                  type="button"
                  className="hx-machine__other"
                  data-health={other.health}
                  data-busy={busy}
                  data-slow={busy && switching?.pending === PENDING.slow}
                  aria-disabled={other.off || !onPick || busy}
                  onClick={() => {
                    if (!other.off && onPick && !busy) onPick(other.id);
                  }}
                >
                  <span className="hx-machine__odot" />
                  <span className="hx-machine__oname">{other.name}</span>
                  <span className="hx-machine__osub">{other.note}</span>
                  <Icon name="loader-circle" size="sm" className="hx-machine__ospin" />
                </button>
              );
            })}
            {onManage && (
              <button
                type="button"
                className="hx-machine__manage"
                onClick={() => {
                  close();
                  onManage();
                }}
              >
                <Icon name="sliders-horizontal" size="sm" />
                {t('next.machine.manage')}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default MachineCard;
