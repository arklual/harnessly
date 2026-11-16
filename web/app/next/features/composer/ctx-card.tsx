// The card of a meter in the field (the design's .hx-ctx): the context ring's, and the limits' and the spend's beside it
// It opens on hover and stays while the pointer travels to it (ui/use-hover-intent.ts); a click pins it, a
// tap toggles it on touch. The card is always in the page and only shown: it leaves with the same animation it came.
import type { ReactNode } from 'react';
import { useFitInViewport, useHoverIntent } from '@/next/ui/use-hover-intent';

export interface ICtxCardProps {
  /** The chip's words for a reader. */
  label: string;
  /** The card's name for a reader. */
  title: string;
  /** «warn» paints the big figure (and the ring) in the warning colour. */
  tone?: 'warn' | 'mid';
  /** The chip's class: the round one of the ring by default. */
  chipClassName?: string;
  chipRef?: (el: HTMLSpanElement | null) => void;
  chip: ReactNode;
  /** A chip that stays on a narrow field: its card then spans the field. */
  wide?: boolean;
  children: ReactNode;
}

const CtxCard = ({
  label,
  title,
  tone,
  chipClassName = 'hx-ctx__chip',
  chipRef,
  chip,
  wide,
  children,
}: ICtxCardProps) => {
  const { open, setTrigger, setFloating, getFloating, triggerProps, floatingProps } = useHoverIntent();
  useFitInViewport(getFloating, open);
  return (
    <div className="hx-ctx" data-tone={tone} data-open={open} data-wide={wide || undefined}>
      <span
        className={chipClassName}
        ref={(el) => {
          chipRef?.(el);
          setTrigger(el);
        }}
        role="button"
        tabIndex={0}
        aria-label={label}
        aria-expanded={open}
        aria-haspopup="dialog"
        {...triggerProps}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            triggerProps.onClick();
          }
        }}
      >
        {chip}
      </span>
      <div
        className="hx-ctx__pop hx-hover-fit"
        ref={setFloating}
        role="dialog"
        aria-label={title}
        inert={!open}
        {...floatingProps}
      >
        {children}
      </div>
    </div>
  );
};

export default CtxCard;
