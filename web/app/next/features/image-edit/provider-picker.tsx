// The choice of the generator and of its model, and the price of a paid one: the same in the panel that draws a picture
// (designly/image-gen/gen-panel.tsx) and in the editor that changes one (edit-dock.tsx). Labels and selects only: the
// place they sit in styles them.
import { t } from '@/next/i18n';
import type { ImageProvider } from '@/next/features/image-edit/host';
import {
  creditsText,
  modelsFor,
  type IImageProviderInfo,
  type TPrice,
} from '@/next/features/image-edit/providers';

export interface IProviderChoice {
  provider: ImageProvider;
  /** The model picked; '' for the provider's first (its default). */
  model: string;
}

/** The choice as it is sent: a generator the machine has, and its model when it has models. */
export const settleChoice = (
  providers: readonly IImageProviderInfo[],
  choice: IProviderChoice,
  editing: boolean,
): { info: IImageProviderInfo | undefined; provider: ImageProvider; model?: string } => {
  const info = providers.find((p) => p.id === choice.provider && (!editing || p.edit));
  if (!info) return { info: providers[0], provider: providers[0]?.id ?? 'codex' };
  const models = modelsFor(info, editing);
  if (models.length === 0) return { info, provider: info.id };
  const model = models.some((m) => m.id === choice.model) ? choice.model : models[0].id;
  return { info, provider: info.id, model };
};

interface IProviderPickerProps {
  providers: readonly IImageProviderInfo[];
  choice: IProviderChoice;
  /** A change of a picture: only the generators and models that take one. */
  editing?: boolean;
  disabled?: boolean;
  onChoice: (choice: IProviderChoice) => void;
}

export const ProviderPicker = ({
  providers,
  choice,
  editing = false,
  disabled,
  onChoice,
}: IProviderPickerProps) => {
  const list = providers.filter((p) => !editing || p.edit);
  const settled = settleChoice(providers, choice, editing);
  const models = modelsFor(settled.info, editing);
  return (
    <>
      <label>
        {t('next.imagegen.provider')}
        <select
          aria-label={t('next.imagegen.provider')}
          value={settled.provider}
          disabled={disabled}
          onChange={(event) => {
            const next = list.find((p) => p.id === event.target.value);
            if (next) onChoice({ provider: next.id, model: '' });
          }}
        >
          {list.map((p) => (
            <option key={p.id} value={p.id}>
              {p.experimental ? `${p.name} · ${t('next.agents.experimental')}` : p.name}
            </option>
          ))}
        </select>
      </label>
      {models.length > 0 && (
        <label>
          {t('next.imagegen.model')}
          <select
            aria-label={t('next.imagegen.model')}
            value={settled.model}
            disabled={disabled}
            onChange={(event) => onChoice({ provider: settled.provider, model: event.target.value })}
          >
            {models.map((m) => (
              <option key={m.id} value={m.id}>
                {m.maker && !m.name.includes(m.maker) ? `${m.name} · ${m.maker}` : m.name}
              </option>
            ))}
          </select>
        </label>
      )}
    </>
  );
};

/** The line under a paid generator: what the job costs, or why it cannot be told. Nothing for a free one. */
export const PriceNote = ({ price, name }: { price: TPrice; name: string }) => {
  if (price.k === 'none') return null;
  const text =
    price.k === 'loading'
      ? t('next.imagegen.pricing')
      : price.k === 'ready'
        ? t('next.imagegen.price', { credits: creditsText(price.price.credits), name })
        : t('next.imagegen.priceFailed', { why: price.why });
  return (
    <p role="note" className="hx-imagegen__price" data-failed={price.k === 'failed' ? '1' : undefined}>
      {text}
    </p>
  );
};

/** A paid job may start only once its price is known (the person sees what it costs before it is spent). */
export const priceAllows = (price: TPrice): boolean => price.k === 'none' || price.k === 'ready';
