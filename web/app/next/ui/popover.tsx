// A panel that opens from a control on Base UI's Popover (headless): positioning, focus and Esc come from it, the look
// from the caller's class. Base UI keeps the popup mounted while its exit runs ([data-ending-style]). On the phone the
// same panel is a bottom sheet (ui/sheet.tsx): the props do not change, the trigger opens and closes it.
import { Popover as BasePopover } from '@base-ui/react/popover';
import { cloneElement, type MouseEvent, type ReactElement, type ReactNode } from 'react';
import { usePortalContainer } from '@/next/ui/portal';
import Sheet from '@/next/ui/sheet';
import Tooltip from '@/next/ui/tooltip';
import { usePhone } from '@/next/ui/use-media';

interface IPopoverProps {
  trigger: ReactElement;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  label: string;
  /** A tooltip over the trigger while the panel is closed. */
  hint?: string;
  className: string;
  side?: 'top' | 'bottom';
  align?: 'start' | 'center' | 'end';
  children: ReactNode;
}

const Popover = ({
  trigger,
  open,
  onOpenChange,
  label,
  hint,
  className,
  side = 'top',
  align = 'start',
  children,
}: IPopoverProps) => {
  const container = usePortalContainer();
  const phone = usePhone();
  if (phone) {
    const own = (trigger.props as { onClick?: (event: MouseEvent<HTMLElement>) => void }).onClick;
    return (
      <>
        {cloneElement(trigger as ReactElement<Record<string, unknown>>, {
          'aria-expanded': open,
          'aria-haspopup': 'dialog',
          onClick: (event: MouseEvent<HTMLElement>) => {
            own?.(event);
            onOpenChange(!open);
          },
        })}
        <Sheet
          open={open}
          onClose={() => onOpenChange(false)}
          label={label}
          className={`${className} hx-sheet--pop`}
        >
          {children}
        </Sheet>
      </>
    );
  }
  return (
    <BasePopover.Root open={open} onOpenChange={(next) => onOpenChange(next)}>
      {hint ? (
        <Tooltip label={hint}>
          <BasePopover.Trigger render={trigger} />
        </Tooltip>
      ) : (
        <BasePopover.Trigger render={trigger} />
      )}
      <BasePopover.Portal container={container}>
        <BasePopover.Positioner
          className="hx-pop-pos"
          side={side}
          align={align}
          sideOffset={10}
          collisionPadding={8}
        >
          <BasePopover.Popup className={className} aria-label={label}>
            {children}
          </BasePopover.Popup>
        </BasePopover.Positioner>
      </BasePopover.Portal>
    </BasePopover.Root>
  );
};

export default Popover;
