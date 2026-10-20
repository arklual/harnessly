import { useState } from 'react';
import { Check, ChevronDown } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import AgentAvatar from '@/components/timeline/agent-avatar';
import { isSubscriptionModel, shortModelName, type IOwuiModel } from '@/lib/hl-openwebui';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';

interface IModelPickerProps {
  models: IOwuiModel[];
  value: IOwuiModel | undefined;
  disabled?: boolean;
  onChange: (id: string) => void;
}

const ModelPicker = ({ models, value, disabled, onChange }: IModelPickerProps) => {
  const [open, setOpen] = useState(false);
  const label = value ? shortModelName(value) : t('chat.modelPicker.model');

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={disabled || models.length === 0}
        aria-label={t('chat.modelPicker.triggerLabel', { label })}
        title={t('chat.modelPicker.model')}
        className="flex h-7 min-w-0 items-center gap-1.5 rounded-lg px-2 text-xs font-medium text-foreground/80 transition-colors hover:bg-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-45 data-[popup-open]:bg-muted"
      >
        <span className="truncate">{label}</span>
        <ChevronDown size={11} className="shrink-0 text-muted-foreground" />
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="start"
        sideOffset={10}
        className="w-[290px] max-w-[calc(100vw-24px)] gap-0 rounded-2xl p-2"
      >
        <div role="group" aria-label={t('chat.modelPicker.model')}>
          <div className="px-2.5 pb-2 pt-2 text-xs font-semibold">{t('chat.modelPicker.model')}</div>
          {models.map((model) => {
            const selected = model.id === value?.id;
            return (
              <button
                key={model.id}
                type="button"
                role="menuitemradio"
                aria-checked={selected}
                onClick={() => {
                  onChange(model.id);
                  setOpen(false);
                }}
                className={cn(
                  'flex w-full items-center gap-3 rounded-xl px-2.5 py-2 text-left transition-colors hover:bg-muted focus-visible:bg-muted focus-visible:outline-none',
                  selected && 'bg-brand/8',
                )}
              >
                <AgentAvatar provider="chat" className="h-7 w-7 rounded-lg [&_svg]:h-3.5 [&_svg]:w-3.5" />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <strong className="truncate text-xs font-medium">{shortModelName(model)}</strong>
                  <small className="text-[10px] text-muted-foreground">
                    {isSubscriptionModel(model) ? t('chat.modelPicker.viaSubscription') : model.id}
                  </small>
                </span>
                <span className="w-4 text-brand" aria-hidden="true">
                  {selected && <Check size={14} />}
                </span>
              </button>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
};

export default ModelPicker;
