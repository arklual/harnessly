// A change of model in the feed (the design's quiet event line): «Модель: Opus 5.5 → Sonnet 5.5 · effort high». It is
// the answer to the line over the field («Модель сменится на … после хода»): the runtime applied it, and here it stands
// at the place in the conversation where the agent took it. A change the agent did not take says so in its place:
// «Модель не сменилась на Sonnet 5.5: агент оставил прежнюю».
import { effortLabel } from '@/next/features/composer/agents';
import type { ModelSwitchItem } from '@/next/features/chat/types';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/chat.css';

export interface IModelSwitchProps {
  item: ModelSwitchItem;
  /** A model's id as people say it («Sonnet 5.5»). */
  label: (id: string) => string;
}

/** «Opus 5.5 → Sonnet 5.5», or only the new one when none was known before. */
export const switchModels = (item: ModelSwitchItem, label: (id: string) => string): string => {
  const to = label(item.to.id);
  if (!item.from) return to;
  if (item.from.id === item.to.id) return to;
  return t('next.modelSwitch.fromTo', { from: label(item.from.id), to });
};

/** Ultracode turned on or off by the change; null when the mode stayed. */
export const ultracodeChange = (item: ModelSwitchItem): boolean | null => {
  const to = item.to.ultracode === true;
  return to === (item.from?.ultracode === true) ? null : to;
};

/** Why the agent did not take a change, in words of the switch: it kept its model, or the model has no such mode. */
const WHY: Readonly<Record<string, string>> = {
  conflict: 'next.modelSwitch.why.kept',
  'option-unknown': 'next.modelSwitch.why.unsupported',
} as const;

export const refusalReason = (code: string): string => t(WHY[code] ?? `next.refuse.${code}`);

const ModelSwitchRow = ({ item, label }: IModelSwitchProps) => {
  const effort = item.to.effort;
  const ultracode = ultracodeChange(item);
  if (item.refused)
    return (
      <div className="hx-modelswitch" data-refused="true" role="status">
        <Icon name="circle-alert" size="sm" className="hx-modelswitch__ic" />
        <span className="hx-modelswitch__text">
          {t('next.modelSwitch.refused', { to: label(item.to.id), reason: refusalReason(item.refused) })}
        </span>
      </div>
    );
  return (
    <div className="hx-modelswitch" role="status">
      <Icon name="sliders-horizontal" size="sm" className="hx-modelswitch__ic" />
      <span className="hx-modelswitch__text">
        <span className="hx-modelswitch__label">{t('next.modelSwitch.label')}</span>
        <span className="hx-modelswitch__models">{switchModels(item, label)}</span>
        {effort && (
          <span className="hx-modelswitch__effort">
            {t('next.modelSwitch.effort', { effort: effortLabel(effort) })}
          </span>
        )}
        {ultracode !== null && (
          <span className="hx-modelswitch__effort" data-ultracode={ultracode || undefined}>
            {t(ultracode ? 'next.modelSwitch.ultracodeOn' : 'next.modelSwitch.ultracodeOff')}
          </span>
        )}
      </span>
    </div>
  );
};

export default ModelSwitchRow;
