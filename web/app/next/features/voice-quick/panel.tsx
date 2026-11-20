// The panel of the quick voice settings: a small popover over the button it belongs to (Base UI, anchored on the button
// so it never covers the field), a bottom sheet on the phone. Esc, a click outside or the same gesture again close it;
// the focus goes in and comes back to the button.
import { Popover } from '@base-ui/react/popover';
import { useMemo, useRef, type ReactNode } from 'react';
import type { IQuick } from '@/next/features/voice-quick/use-quick';
import { usePortalContainer } from '@/next/ui/portal';
import Sheet from '@/next/ui/sheet';
import { usePhone } from '@/next/ui/use-media';
import '@/next/styles/voice-quick.css';

/**
 * Where the panel hangs: under the button's right edge, but over the top of the field the button stands in, so it
 * never covers the text being typed. A button outside a field gives its own box.
 */
const placeOver = (button: HTMLElement | null) =>
  button
    ? {
        getBoundingClientRect: () => {
          const at = button.getBoundingClientRect();
          const field = button.closest('.hx-composer')?.getBoundingClientRect();
          return new DOMRect(at.left, field ? field.top : at.top, at.width, 0);
        },
      }
    : undefined;

interface IQuickPanelProps {
  quick: IQuick;
  label: string;
  children: ReactNode;
}

const QuickPanel = ({ quick, label, children }: IQuickPanelProps) => {
  const container = usePortalContainer();
  const phone = usePhone();
  const popup = useRef<HTMLDivElement>(null);
  const anchor = useMemo(() => placeOver(quick.anchor), [quick.anchor]);
  if (phone) {
    return (
      <Sheet
        open={quick.open}
        onClose={() => quick.setOpen(false)}
        label={label}
        className="hx-vq hx-sheet--pop"
      >
        {children}
      </Sheet>
    );
  }
  return (
    <Popover.Root
      open={quick.open}
      onOpenChange={(next, details) => {
        // Esc in a list goes back to the settings; the next one closes.
        if (!next && details.reason === 'escape-key' && quick.sub !== null) {
          details.cancel();
          quick.setSub(null);
          return;
        }
        // A press on the button itself is its own gesture (a second right click closes): the button toggles.
        if (
          !next &&
          details.reason === 'outside-press' &&
          details.event.target instanceof Node &&
          quick.anchor?.contains(details.event.target)
        ) {
          details.cancel();
          return;
        }
        quick.setOpen(next);
      }}
    >
      <Popover.Portal container={container ?? undefined}>
        <Popover.Positioner
          className="hx-pop-pos"
          anchor={anchor}
          side="top"
          align="end"
          sideOffset={10}
          collisionPadding={8}
        >
          <Popover.Popup
            ref={popup}
            className="hx-vq"
            aria-label={label}
            // The panel itself (its controls come with Tab): the first one may still be a placeholder while the
            // microphones are read. Back on the button when it closes.
            initialFocus={popup}
            finalFocus={() => quick.anchor ?? true}
          >
            {children}
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
};

export default QuickPanel;
