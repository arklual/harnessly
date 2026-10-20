// Command palette: cmdk's list and filter inside our dialog.
import { Command as Cmdk } from 'cmdk';
import { Search } from 'lucide-react';
import type { ComponentProps, HTMLAttributes, ReactNode } from 'react';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';

export const Command = ({ className, ...rest }: ComponentProps<typeof Cmdk>) => (
  <Cmdk className={cn('hl-command', className)} {...rest} />
);

export function CommandDialog({
  open,
  onOpenChange,
  title = t('primitives.command.title'),
  description = '',
  className,
  showCloseButton = false,
  children,
}: {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  title?: string;
  description?: string;
  className?: string;
  showCloseButton?: boolean;
  children?: ReactNode;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={cn('hl-command-dialog', className)} showCloseButton={showCloseButton}>
        <DialogTitle className="hl-sr-only">{title}</DialogTitle>
        {description ? <DialogDescription className="hl-sr-only">{description}</DialogDescription> : null}
        {children}
      </DialogContent>
    </Dialog>
  );
}

export const CommandInput = ({ className, ...rest }: ComponentProps<typeof Cmdk.Input>) => (
  <div className="hl-command-input">
    <Search size={16} aria-hidden />
    <Cmdk.Input className={className} {...rest} />
  </div>
);

export const CommandList = ({ className, ...rest }: ComponentProps<typeof Cmdk.List>) => (
  <Cmdk.List className={cn('hl-command-list', className)} {...rest} />
);
export const CommandEmpty = ({ className, ...rest }: ComponentProps<typeof Cmdk.Empty>) => (
  <Cmdk.Empty className={cn('hl-command-empty', className)} {...rest} />
);
export const CommandGroup = ({ className, ...rest }: ComponentProps<typeof Cmdk.Group>) => (
  <Cmdk.Group className={cn('hl-command-group', className)} {...rest} />
);
export const CommandItem = ({ className, ...rest }: ComponentProps<typeof Cmdk.Item>) => (
  <Cmdk.Item className={cn('hl-command-item', className)} {...rest} />
);
export const CommandShortcut = ({ className, ...rest }: HTMLAttributes<HTMLSpanElement>) => (
  <span className={cn('hl-command-shortcut', className)} {...rest} />
);
