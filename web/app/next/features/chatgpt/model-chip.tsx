// The model and the depth of thinking of a ChatGPT chat, by the field: a chip that opens a panel with the plan's
// models and, for a model that says it takes them, its depths. Only what the plan lists is offered.
import { useState } from 'react';
import type { IGptModel } from '@/next/features/chatgpt/model';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';

interface IModelChipProps {
  models: readonly IGptModel[];
  model: IGptModel | null;
  effort: string;
  onModel: (model: IGptModel) => void;
  onEffort: (effort: string) => void;
}

/** The words of a depth the plan names; an unknown one shows as it is. */
export const effortWord = (effort: string): string => {
  const key = `next.chatgpt.effort.${effort}`;
  const word = t(key);
  return word === key ? effort : word;
};

const ModelChip = ({ models, model, effort, onModel, onEffort }: IModelChipProps) => {
  const [open, setOpen] = useState(false);
  if (!model) return null;
  const efforts = model.efforts ?? [];
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      label={t('next.chatgpt.modelTitle')}
      className="hx-gpt-pick"
      side="top"
      align="start"
      trigger={
        <button type="button" className="hx-gpt-chip" aria-label={t('next.chatgpt.modelTitle')}>
          <span className="hx-gpt-chip__name">{model.name}</span>
          {effort && <span className="hx-gpt-chip__effort">{effortWord(effort)}</span>}
          <Icon name="chevron-down" size="sm" />
        </button>
      }
    >
      <div className="hx-gpt-pick__head">{t('next.chatgpt.modelTitle')}</div>
      <div className="hx-gpt-pick__list" role="listbox" aria-label={t('next.chatgpt.modelTitle')}>
        {models.map((m) => (
          <button
            key={m.slug}
            type="button"
            role="option"
            className="hx-gpt-pick__opt"
            aria-selected={m.slug === model.slug}
            onClick={() => onModel(m)}
          >
            <span>{m.name}</span>
            <Icon name="check" size="sm" className="hx-gpt-pick__check" />
          </button>
        ))}
      </div>
      {efforts.length > 0 && (
        <>
          <div className="hx-gpt-pick__head">{t('next.chatgpt.effortTitle')}</div>
          <div className="hx-gpt-pick__efforts" role="radiogroup" aria-label={t('next.chatgpt.effortTitle')}>
            {efforts.map((e) => (
              <button
                key={e}
                type="button"
                role="radio"
                aria-checked={e === effort}
                className="hx-gpt-pick__effort"
                onClick={() => onEffort(e)}
              >
                {effortWord(e)}
              </button>
            ))}
          </div>
        </>
      )}
      <div className="hx-gpt-pick__foot">{t('next.chatgpt.planFoot')}</div>
    </Popover>
  );
};

export default ModelChip;
