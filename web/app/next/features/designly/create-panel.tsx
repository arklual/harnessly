// The settings of a new presentation or video, behind the field's one «Настройки» button: a popover anchored under the
// field (over it when there is no room below, never on top of the field) on a wide window, a bottom sheet with a scrim
// on a phone (560 px, the board's breakpoint). It opens in 240 ms (380 on the phone) and leaves in 160 ms (240); it
// closes on the scrim, the cross, «Готово», Esc and, on the phone, a swipe down. The design system is its first
// block: a short list, the one picked ticked.
import { useEffect, useEffectEvent, useLayoutEffect, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import type { IDesignSystem } from '@/next/features/designly/api';
import KitImport from '@/next/features/designly/kit-import';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { SheetGrip, useFocusTrap, useSheetDrag } from '@/next/ui/sheet';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresentFlag } from '@/next/ui/presence';
import { useMedia } from '@/next/ui/use-media';
import { t } from '@/next/i18n';

/** The board's phone breakpoint, where the popover becomes a sheet. */
export const DECK_PHONE_QUERY = '(max-width: 560px)';
const EXIT_WIDE_MS = 160;
const EXIT_PHONE_MS = 240;

/** The gap between the field and the popover, and the margin it keeps from the window's edge. */
const GAP = 8;
const EDGE = 12;

/** Puts the popover under the anchor (over it when the room below is smaller), through properties set on the panel. */
const place = (panel: HTMLElement, anchor: HTMLElement) => {
  const a = anchor.getBoundingClientRect();
  const below = window.innerHeight - a.bottom - GAP - EDGE;
  const above = a.top - GAP - EDGE;
  const need = panel.scrollHeight;
  const up = need > below && above > below;
  const room = Math.max(160, up ? above : below);
  const width = Math.min(380, a.width);
  const left = Math.min(Math.max(EDGE, a.left), Math.max(EDGE, window.innerWidth - width - EDGE));
  panel.dataset.side = up ? 'top' : 'bottom';
  panel.style.setProperty('--set-left', `${left}px`);
  panel.style.setProperty('--set-w', `${width}px`);
  panel.style.setProperty('--set-max', `${room}px`);
  panel.style.setProperty('--set-y', `${up ? window.innerHeight - a.top + GAP : a.bottom + GAP}px`);
};

interface ICreatePanelProps {
  open: boolean;
  onClose: () => void;
  /** The field the popover hangs from. */
  anchor: HTMLElement | null;
  /** The dialog's name for a screen reader. */
  label: string;
  children: ReactNode;
}

const CreatePanel = ({ open, onClose, anchor, label, children }: ICreatePanelProps) => {
  const phone = useMedia(DECK_PHONE_QUERY);
  const { mounted, state } = usePresentFlag(open, phone ? EXIT_PHONE_MS : EXIT_WIDE_MS);
  const [box, setBox] = useState<HTMLElement | null>(null);
  const close = useEffectEvent(onClose);
  // The start's hero moves with the scroll (a transform), which would take a fixed panel along: it opens outside it.
  const container = usePortalContainer();

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open]);
  useSheetDrag(box, onClose, open && phone);
  useFocusTrap(box, open);

  useLayoutEffect(() => {
    if (!box || !anchor || phone) return;
    const put = () => place(box, anchor);
    put();
    window.addEventListener('resize', put);
    return () => window.removeEventListener('resize', put);
  }, [box, anchor, phone, mounted]);

  if (!mounted) return null;
  return createPortal(
    <>
      <div className="hx-deck-scrim" data-state={state} onClick={onClose} aria-hidden="true" />
      <section
        ref={setBox}
        className="hx-deck-set"
        data-state={state}
        role="dialog"
        aria-label={label}
        tabIndex={-1}
      >
        <SheetGrip />
        <div className="hx-deck-set__head">
          <b>{t('next.deck.settings.title')}</b>
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            data-shape="icon"
            aria-label={t('next.deck.settings.close')}
            onClick={onClose}
          >
            <Icon name="x" size="sm" />
          </button>
        </div>
        {children}
        <button type="button" className="hx-btn hx-deck-set__go" data-variant="primary" onClick={onClose}>
          {t('next.deck.settings.done')}
        </button>
      </section>
    </>,
    container?.current ?? document.body,
  );
};

export default CreatePanel;

/** What «Добавить дизайн-систему» needs: the machine, and what to do once a kit is installed. */
export interface IKitAdd {
  machine: MachineId;
  onAdded: () => void;
}

/** A look from elsewhere than the machine's systems (a deck's company template): the last row of the list. */
export interface IDsOther {
  label: string;
  /** The file it comes from, or what it asks for while there is none. */
  desc: string;
  active: boolean;
  onPick: () => void;
}

interface IDsChoiceProps {
  systems: readonly IDesignSystem[];
  /** null: none, or the other look when it is active. */
  value: string | null;
  onChange: (id: string | null) => void;
  other?: IDsOther;
  /** Adds a kit file to the machine's systems; without it the list has no such button. */
  kit?: IKitAdd;
}

/** The design system as the panel's first block: none, each of the machine's systems, and the other look. */
export const DsChoice = ({ systems, value, onChange, other, kit }: IDsChoiceProps) => {
  const viaOther = other?.active === true && value === null;
  return (
    <>
      <div className="hx-deck-set__label">{t('next.designly.ds')}</div>
      <div className="hx-deck-set__ds" role="radiogroup" aria-label={t('next.designly.ds')}>
        <button
          type="button"
          role="radio"
          className="hx-option"
          aria-checked={value === null && !viaOther}
          aria-selected={value === null && !viaOther}
          onClick={() => onChange(null)}
        >
          <span className="hx-option__text">
            <span className="hx-option__name">{t('next.designly.noDs')}</span>
          </span>
          <Icon name="check" size="sm" className="hx-option__check" />
        </button>
        {systems.map((s) => (
          <button
            key={s.id}
            type="button"
            role="radio"
            className="hx-option"
            aria-checked={s.id === value}
            aria-selected={s.id === value}
            onClick={() => onChange(s.id)}
          >
            <span className="hx-option__text">
              <span className="hx-option__name">{s.label}</span>
              {s.desc && <span className="hx-option__desc">{s.desc}</span>}
            </span>
            <Icon name="check" size="sm" className="hx-option__check" />
          </button>
        ))}
        {other && (
          <button
            type="button"
            role="radio"
            className="hx-option"
            aria-checked={viaOther}
            aria-selected={viaOther}
            onClick={other.onPick}
          >
            <Icon name="presentation" size="sm" />
            <span className="hx-option__text">
              <span className="hx-option__name">{other.label}</span>
              <span className="hx-option__desc">{other.desc}</span>
            </span>
            <Icon name="check" size="sm" className="hx-option__check" />
          </button>
        )}
      </div>
      {kit && <KitImport machine={kit.machine} onAdded={kit.onAdded} />}
    </>
  );
};
