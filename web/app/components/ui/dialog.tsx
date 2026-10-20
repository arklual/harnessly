// Modal dialog on Base UI's headless Dialog: a backdrop, a centred panel, an optional close button.
import { Dialog as Base } from '@base-ui/react/dialog';
import { X } from 'lucide-react';
import type { ComponentProps, HTMLAttributes, ReactNode } from 'react';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';

export function Dialog({
  open,
  onOpenChange,
  children,
}: {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  children?: ReactNode;
}) {
  return (
    <Base.Root open={open} onOpenChange={(next) => onOpenChange?.(next)}>
      {children}
    </Base.Root>
  );
}

type TContentProps = Omit<ComponentProps<typeof Base.Popup>, 'className'> & {
  className?: string;
  showCloseButton?: boolean;
};

export function DialogContent({ className, children, showCloseButton = true, ...rest }: TContentProps) {
  return (
    <Base.Portal>
      <Base.Backdrop className="hl-dialog-backdrop" />
      <Base.Popup className={cn('hl-dialog', className)} {...rest}>
        {children}
        {showCloseButton && (
          <Base.Close className="hl-dialog-close" aria-label={t('primitives.dialog.close')}>
            <X size={16} />
          </Base.Close>
        )}
      </Base.Popup>
    </Base.Portal>
  );
}

export const DialogHeader = ({ className, ...rest }: HTMLAttributes<HTMLDivElement>) => (
  <div className={cn('hl-dialog-header', className)} {...rest} />
);
export const DialogFooter = ({ className, ...rest }: HTMLAttributes<HTMLDivElement>) => (
  <div className={cn('hl-dialog-footer', className)} {...rest} />
);
export const DialogTitle = ({ className, ...rest }: ComponentProps<typeof Base.Title>) => (
  <Base.Title className={cn('hl-dialog-title', className as string)} {...rest} />
);
export const DialogDescription = ({ className, ...rest }: ComponentProps<typeof Base.Description>) => (
  <Base.Description className={cn('hl-dialog-description', className as string)} {...rest} />
);
