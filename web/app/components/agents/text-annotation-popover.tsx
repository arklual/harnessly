import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { MessageCirclePlus, TextQuote } from 'lucide-react';
import { POPOVER_ATTRIBUTE } from '@/hooks/use-text-selection-annotation';
import { placePopover, type IPopoverPlacement } from '@/lib/hl-popover-placement';
import type { ISelectionTarget } from '@/lib/hl-selection-target';
import { ANNOTATE, INTENT_LABELS, type TTextIntent } from '@/lib/hl-text-annotation';
import { t } from '@/lib/i18n';

interface ITextAnnotationPopoverProps {
  target: ISelectionTarget;
  /** A touch screen: bigger tap targets, the panel opens below the fragment. */
  compact: boolean;
  onPick: (intent: TTextIntent) => void;
  /** The fragment goes to the side chat of the tab instead of the composer. */
  onSideChat: () => void;
  onClose: () => void;
}

/** The pill next to a selected fragment: annotate it (its comment is typed in its card) or send it to the side chat. */
const TextAnnotationPopover = ({
  target,
  compact,
  onPick,
  onSideChat,
  onClose,
}: ITextAnnotationPopoverProps) => {
  const panelRef = useRef<HTMLDivElement>(null);
  const [placement, setPlacement] = useState<IPopoverPlacement | null>(null);
  const { rect, fromButton } = target;

  useLayoutEffect(() => {
    const panel = panelRef.current;
    if (!panel) return;
    setPlacement(
      placePopover({
        selection: rect,
        size: { width: panel.offsetWidth, height: panel.offsetHeight },
        viewport: { width: window.innerWidth, height: window.innerHeight },
        preferBelow: compact || fromButton,
      }),
    );
  }, [rect, compact, fromButton]);

  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.altKey && event.shiftKey && event.code === 'KeyA') {
        event.preventDefault();
        panelRef.current?.querySelector('button')?.focus();
      }
      if (event.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [onClose]);

  return createPortal(
    <div
      ref={panelRef}
      {...{ [POPOVER_ATTRIBUTE]: '' }}
      className="tw-textsel"
      role="dialog"
      aria-label={t('agents.textAnnotationPopover.label')}
      data-side={placement?.side}
      data-compact={compact || undefined}
      style={{
        left: placement?.left ?? 0,
        top: placement?.top ?? 0,
        visibility: placement ? 'visible' : 'hidden',
      }}
    >
      <div className="tw-textsel-actions" role="group" aria-label={t('agents.textAnnotationPopover.actions')}>
        <button
          type="button"
          className="tw-textsel-action"
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => onPick(ANNOTATE)}
        >
          <TextQuote aria-hidden="true" />
          {INTENT_LABELS[ANNOTATE]}
        </button>
        <span className="tw-textsel-sep" aria-hidden="true" />
        <button
          type="button"
          className="tw-textsel-action"
          data-icon-only
          title={t('agents.textAnnotationPopover.sideChat')}
          aria-label={t('agents.textAnnotationPopover.sideChat')}
          onMouseDown={(event) => event.preventDefault()}
          onClick={onSideChat}
        >
          <MessageCirclePlus aria-hidden="true" />
        </button>
      </div>
    </div>,
    document.body,
  );
};

export default TextAnnotationPopover;
