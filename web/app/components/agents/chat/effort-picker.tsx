import { ULTRACODE_EFFORT } from '@/types/hl-launch';
import { useState, type ReactNode } from 'react';
import { BarChart3, Check, ChevronDown, Sparkles } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { EffortBars } from '@/components/agents/launch-options-picker';
import { EFFORT_DESCRIPTIONS, effortLabel, isKnownEffort } from '@/lib/hl-launch-options';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';

interface IEffortPickerProps {
  efforts: string[];
  defaultEffort: string | null;
  value: string | null;
  disabled?: boolean;
  onChange: (effort: string | null) => void;
}

const EffortOption = ({
  selected,
  icon,
  title,
  hint,
  onSelect,
}: {
  selected: boolean;
  icon: ReactNode;
  title: string;
  hint: string;
  onSelect: () => void;
}) => (
  <button
    type="button"
    role="menuitemradio"
    aria-checked={selected}
    onClick={onSelect}
    className={cn(
      'flex w-full items-center gap-3 rounded-xl px-2.5 py-2 text-left transition-colors hover:bg-muted focus-visible:bg-muted focus-visible:outline-none',
      selected && 'bg-brand/8 text-foreground',
    )}
  >
    {icon}
    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
      <strong className="text-xs font-medium">{title}</strong>
      <small className="text-[10px] leading-snug text-muted-foreground">{hint}</small>
    </span>
    <span className="w-4 text-brand" aria-hidden="true">
      {selected && <Check size={14} />}
    </span>
  </button>
);

const EffortPicker = ({ efforts, defaultEffort, value, disabled, onChange }: IEffortPickerProps) => {
  const [open, setOpen] = useState(false);
  if (efforts.length === 0) return null;
  const label = value ? effortLabel(value) : t('chat.effortPicker.auto');
  const choose = (effort: string | null) => {
    onChange(effort);
    setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={disabled}
        aria-label={t('chat.effortPicker.triggerLabel', { label })}
        title={t('chat.effortPicker.title')}
        className="flex h-7 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-lg px-2 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-45 data-[popup-open]:bg-muted data-[popup-open]:text-foreground"
      >
        <BarChart3 size={14} />
        <span data-ultra={value === ULTRACODE_EFFORT || undefined}>{label}</span>
        <ChevronDown size={11} />
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="end"
        sideOffset={10}
        className="w-[310px] max-w-[calc(100vw-24px)] gap-0 rounded-2xl p-2"
      >
        <div role="group" aria-label={t('chat.effortPicker.title')}>
          <div className="flex items-center justify-between px-2.5 pb-2.5 pt-2 text-xs font-semibold">
            {t('chat.effortPicker.title')}
            <span className="text-[10px] font-normal text-muted-foreground">Effort</span>
          </div>
          <EffortOption
            selected={value === null}
            icon={
              <span className="flex h-7 w-6 shrink-0 items-center justify-center text-muted-foreground">
                <Sparkles size={16} />
              </span>
            }
            title={t('chat.effortPicker.automatic')}
            hint={
              defaultEffort
                ? t('chat.effortPicker.defaultWith', { effort: effortLabel(defaultEffort) })
                : t('chat.effortPicker.default')
            }
            onSelect={() => choose(null)}
          />
          <div className="mx-2.5 my-1.5 h-px bg-border" />
          {efforts.map((effort) => (
            <EffortOption
              key={effort}
              selected={value === effort}
              icon={<EffortBars effort={effort} />}
              title={effortLabel(effort)}
              hint={isKnownEffort(effort) ? EFFORT_DESCRIPTIONS[effort] : ''}
              onSelect={() => choose(effort)}
            />
          ))}
          <div className="px-2.5 pb-1 pt-2.5 text-[10px] text-muted-foreground">
            {t('chat.effortPicker.footer')}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
};

export default EffortPicker;
