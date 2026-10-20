import { useId, useRef, useState, type FormEvent, type RefObject } from 'react';
import { DatabaseZap, Gauge, ShieldOff, X } from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  CHIP_TONE,
  LAYER_ALIGN,
  TW_MENU_KIND,
  TwButton,
  TwChip,
  TwMenuButton,
  TwPopover,
  TwSpinner,
  twPopoverTriggerProps,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import { MAX_BLOCKED, MAX_PATTERN, THROTTLE_PRESETS, type IThrottle } from '@/lib/hl-preview/devtools/types';
import {
  THROTTLE_LABELS,
  describeThrottle,
  throttlePresetOptions,
  withPattern,
  withoutPattern,
} from '@/lib/hl-preview-devtools-view';
import type { TPreviewDevtools } from '@/hooks/use-preview-devtools';
import { plural, t } from '@/lib/i18n';

interface ICustomThrottleProps {
  throttle: IThrottle;
  saving: boolean;
  anchorRef: RefObject<HTMLElement | null>;
  onApply: (throttle: IThrottle) => Promise<boolean>;
  onClose: () => void;
}

const asNumber = (value: string): number | null => {
  const number = Number(value);
  return value.trim() !== '' && Number.isFinite(number) && number >= 0 ? Math.round(number) : null;
};

/** "Custom…": latency and speeds by hand, starting from what is on now (or Fast 4G when nothing is). */
const CustomThrottle = ({ throttle, saving, anchorRef, onApply, onClose }: ICustomThrottleProps) => {
  const start =
    throttle.preset === 'none' || throttle.preset === 'offline' ? THROTTLE_PRESETS['fast-4g'] : throttle;
  const [latency, setLatency] = useState(String(start.latencyMs));
  const [down, setDown] = useState(String(start.downKbps));
  const [up, setUp] = useState(String(start.upKbps));
  const id = useId();
  const values = { latencyMs: asNumber(latency), downKbps: asNumber(down), upKbps: asNumber(up) };
  const valid = values.latencyMs !== null && values.downKbps !== null && values.upKbps !== null;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!valid || values.latencyMs === null || values.downKbps === null || values.upKbps === null) return;
    if (
      await onApply({
        preset: 'custom',
        latencyMs: values.latencyMs,
        downKbps: values.downKbps,
        upKbps: values.upKbps,
      })
    )
      onClose();
  };

  const field = (key: string, label: string, unit: string, value: string, set: (next: string) => void) => (
    <label className="tw-devtools-field" htmlFor={`${id}-${key}`}>
      <span className="tw-devtools-field-label">{label}</span>
      <span className="tw-devtools-field-input">
        <input
          id={`${id}-${key}`}
          className="tw-devtools-input"
          type="number"
          inputMode="numeric"
          min={0}
          value={value}
          onChange={(event) => set(event.target.value)}
        />
        <span className="tw-devtools-field-unit">{unit}</span>
      </span>
    </label>
  );

  return (
    <TwPopover
      open
      onClose={onClose}
      anchorRef={anchorRef}
      title={t('devtools.networkConditions.customTitle')}
      align={LAYER_ALIGN.start}
      className="tw-devtools-popover"
    >
      <form className="tw-devtools-form" onSubmit={(event) => void submit(event)}>
        {field(
          'latency',
          t('devtools.networkConditions.latency'),
          t('devtools.networkConditions.ms'),
          latency,
          setLatency,
        )}
        {field(
          'down',
          t('devtools.networkConditions.download'),
          t('devtools.networkConditions.kbps'),
          down,
          setDown,
        )}
        {field('up', t('devtools.networkConditions.upload'), t('devtools.networkConditions.kbps'), up, setUp)}
        <p className="tw-devtools-hint">{t('devtools.networkConditions.noLimit')}</p>
        <div className="tw-devtools-form-actions">
          <TwButton type="button" size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.ghost} onClick={onClose}>
            {t('devtools.networkConditions.cancel')}
          </TwButton>
          <TwButton
            type="submit"
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.primary}
            disabled={!valid}
            pending={saving}
          >
            {t('devtools.networkConditions.apply')}
          </TwButton>
        </div>
      </form>
    </TwPopover>
  );
};

interface IBlockListProps {
  blocked: readonly string[];
  saving: boolean;
  anchorRef: RefObject<HTMLElement | null>;
  id: string;
  onChange: (blocked: string[]) => Promise<boolean>;
  onClose: () => void;
}

const BlockList = ({ blocked, saving, anchorRef, id, onChange, onClose }: IBlockListProps) => {
  const [draft, setDraft] = useState('');
  const full = blocked.length >= MAX_BLOCKED;
  const add = async (event: FormEvent) => {
    event.preventDefault();
    const pattern = draft.trim();
    if (!pattern || full) return;
    if (await onChange(withPattern(blocked, pattern))) setDraft('');
  };
  return (
    <TwPopover
      open
      onClose={onClose}
      anchorRef={anchorRef}
      id={id}
      title={t('devtools.networkConditions.blocks')}
      align={LAYER_ALIGN.start}
      className="tw-devtools-popover tw-devtools-blocks"
    >
      {blocked.length === 0 ? (
        <p className="tw-devtools-hint">
          {t('devtools.networkConditions.blockHint')} <code>posthog</code>, <code>*/collect*</code>.
        </p>
      ) : (
        <ul className="tw-devtools-block-list">
          {blocked.map((pattern) => (
            <li key={pattern} className="tw-devtools-block">
              <span className="tw-devtools-block-pattern" title={pattern}>
                {pattern}
              </span>
              <TwButton
                type="button"
                size={BUTTON_SIZE.sm}
                variant={BUTTON_VARIANT.ghost}
                className="tw-devtools-icon"
                aria-label={t('devtools.networkConditions.unblockPattern', { pattern })}
                title={t('devtools.networkConditions.unblock')}
                disabled={saving}
                onClick={() => void onChange(withoutPattern(blocked, pattern))}
              >
                <X />
              </TwButton>
            </li>
          ))}
        </ul>
      )}
      <form className="tw-devtools-block-add" onSubmit={(event) => void add(event)}>
        <input
          className="tw-devtools-input"
          value={draft}
          maxLength={MAX_PATTERN}
          onChange={(event) => setDraft(event.target.value)}
          placeholder={
            full
              ? plural(MAX_BLOCKED, 'devtools.networkConditions.maxRules')
              : t('devtools.networkConditions.patternPlaceholder')
          }
          aria-label={t('devtools.networkConditions.newBlock')}
          disabled={full}
        />
        <TwButton
          type="submit"
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.secondary}
          disabled={!draft.trim() || full}
          pending={saving}
        >
          {t('devtools.networkConditions.add')}
        </TwButton>
      </form>
    </TwPopover>
  );
};

/** Chrome's throttling menu, "Disable cache" and the request blocking list, all saved on the runtime. */
export const NetworkConditions = ({ devtools }: { devtools: TPreviewDevtools }) => {
  const { log, saving, update } = devtools;
  const { throttle, blocked, disableCache } = log.settings;
  const throttleRef = useRef<HTMLSpanElement>(null);
  const blocksRef = useRef<HTMLButtonElement>(null);
  const [custom, setCustom] = useState(false);
  const [blocksOpen, setBlocksOpen] = useState(false);
  const blocksId = useId();

  const menu: TTwMenuEntry[] = [
    ...throttlePresetOptions().map((option) => ({
      kind: TW_MENU_KIND.radio,
      id: option.preset,
      label: THROTTLE_LABELS[option.preset],
      hint: describeThrottle(option),
      checked: throttle.preset === option.preset,
      onSelect: () => void update('throttle', { throttle: option }),
    })),
    { kind: TW_MENU_KIND.separator, id: 'custom-separator' },
    {
      kind: TW_MENU_KIND.radio,
      id: 'custom',
      label: t('devtools.networkConditions.custom'),
      hint: throttle.preset === 'custom' ? describeThrottle(throttle) : undefined,
      checked: throttle.preset === 'custom',
      onSelect: () => setCustom(true),
    },
  ];
  const slowed = throttle.preset !== 'none';
  const savingThrottle = saving === 'throttle';

  return (
    <>
      <span ref={throttleRef} className="tw-devtools-anchor">
        <TwMenuButton
          items={menu}
          menuLabel={t('devtools.networkConditions.conditions')}
          align={LAYER_ALIGN.start}
          className="tw-ui-chip tw-devtools-chip"
          data-tone={slowed ? CHIP_TONE.warning : CHIP_TONE.neutral}
          aria-busy={savingThrottle || undefined}
          aria-label={t('devtools.networkConditions.conditionsValue', {
            preset: THROTTLE_LABELS[throttle.preset],
          })}
          title={describeThrottle(throttle)}
        >
          {savingThrottle ? <TwSpinner /> : <Gauge />}
          <span className="tw-ui-chip-label">{THROTTLE_LABELS[throttle.preset]}</span>
        </TwMenuButton>
      </span>
      {custom && (
        <CustomThrottle
          throttle={throttle}
          saving={savingThrottle}
          anchorRef={throttleRef}
          onApply={(next) => update('throttle', { throttle: next })}
          onClose={() => setCustom(false)}
        />
      )}
      <TwChip
        className="tw-devtools-chip"
        icon={<DatabaseZap />}
        aria-label={t('devtools.networkConditions.disableCache')}
        aria-pressed={disableCache}
        pending={saving === 'disableCache'}
        onClick={() => void update('disableCache', { disableCache: !disableCache })}
      >
        <span className="tw-devtools-chip-word">{t('devtools.networkConditions.disableCache')}</span>
      </TwChip>
      <TwChip
        ref={blocksRef}
        className="tw-devtools-chip"
        tone={blocked.length > 0 ? CHIP_TONE.warning : CHIP_TONE.neutral}
        icon={<ShieldOff />}
        pending={saving === 'blocked' && !blocksOpen}
        aria-label={
          blocked.length > 0
            ? t('devtools.networkConditions.blocksCount', { count: blocked.length })
            : t('devtools.networkConditions.blocks')
        }
        {...twPopoverTriggerProps(blocksOpen, blocksId)}
        onClick={() => setBlocksOpen(!blocksOpen)}
      >
        <span className="tw-devtools-chip-word">{t('devtools.networkConditions.blocks')}</span>
        {blocked.length > 0 && <span className="tw-devtools-chip-count">{blocked.length}</span>}
      </TwChip>
      {blocksOpen && (
        <BlockList
          blocked={blocked}
          saving={saving === 'blocked'}
          anchorRef={blocksRef}
          id={blocksId}
          onChange={(next) => update('blocked', { blocked: next })}
          onClose={() => setBlocksOpen(false)}
        />
      )}
    </>
  );
};
