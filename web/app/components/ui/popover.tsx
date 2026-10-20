// Popover on Base UI: a trigger (its own button, or `render` for a custom element) and a positioned panel.
import { Popover as Base } from '@base-ui/react/popover';
import type { ComponentProps, ReactNode } from 'react';
import { cn } from '@/lib/utils';

export function Popover({
  open,
  onOpenChange,
  children,
}: {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  children?: ReactNode;
}) {
  return (
    <Base.Root open={open} onOpenChange={onOpenChange ? (next) => onOpenChange(next) : undefined}>
      {children}
    </Base.Root>
  );
}

export const PopoverTrigger = (props: ComponentProps<typeof Base.Trigger>) => <Base.Trigger {...props} />;

type TContentProps = Omit<ComponentProps<typeof Base.Popup>, 'className'> & {
  className?: string;
  side?: 'top' | 'bottom' | 'left' | 'right';
  align?: 'start' | 'center' | 'end';
  sideOffset?: number;
  alignOffset?: number;
};

export function PopoverContent({
  className,
  side = 'bottom',
  align = 'center',
  sideOffset = 4,
  alignOffset = 0,
  children,
  ...rest
}: TContentProps) {
  return (
    <Base.Portal>
      <Base.Positioner
        side={side}
        align={align}
        sideOffset={sideOffset}
        alignOffset={alignOffset}
        className="hl-popover-positioner"
      >
        <Base.Popup className={cn('hl-popover', className)} {...rest}>
          {children}
        </Base.Popup>
      </Base.Positioner>
    </Base.Portal>
  );
}
