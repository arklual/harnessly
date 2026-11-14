// Settings → «Умные функции»: the background features of the machine and whose model does each. «Как сейчас»
// leaves a feature to the chat's own agent; a provider's model does it always. A feature that does not go through a
// picked model yet says so and keeps the choice; one whose last call failed says why in the provider's words.
import { markOf, type TChangedMarks } from '@/next/features/settings/changed-mark';
import { effortLabel } from '@/next/features/settings/ext-models-editor';
import { Group, MenuRow } from '@/next/features/settings/rows';
import type { IChoice } from '@/next/features/settings/options';
import {
  SMART_AGENTS,
  SMART_OFF,
  type ISmartChoice,
  type ISmartFeature,
  type ISmartProvider,
  type ISmartView,
  type TSmartAgent,
} from '@/next/features/settings/smart-api';
import { t } from '@/next/i18n';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { Skeleton } from '@/next/ui/skeleton';
import '@/next/styles/settings-changes.css';

/** The catalog's key of a feature's setting (cmd/runtime/routes_settingsctl.go). */
export const smartKey = (feature: string) => `background.${feature}`;

export type TSmartState =
  { k: 'loading' } | { k: 'failed'; text: string; onRetry: () => void } | { k: 'ready'; view: ISmartView };

interface ISmartSectionProps {
  state: TSmartState;
  /** The features whose choice waits for the machine. */
  busy: ReadonlySet<string>;
  marks: TChangedMarks;
  /** The models of the machine's agents, for a feature an agent's model may do. */
  agentModels: TAgentModels;
  onPick: (feature: string, choice: ISmartChoice | null) => void;
  onOpenProviders: () => void;
}

export type TAgentModels = Readonly<Record<TSmartAgent, readonly { id: string; label: string }[]>>;

/** A choice as the menu's value: a provider's id has no «/» (it is a slug), a model's may; a level of reasoning
 * follows a «#», which no model id has. */
const valueOf = (choice: ISmartChoice): string =>
  `${choice.provider}/${choice.model}${choice.effort ? `#${choice.effort}` : ''}`;

/** What a feature may be given: «as now», every model of every provider, and its own choice if no list has it. */
export const smartChoices = (
  feature: ISmartFeature,
  providers: readonly ISmartProvider[],
  agentModels: TAgentModels,
): { choices: IChoice[]; byValue: ReadonlyMap<string, ISmartChoice> } => {
  const byValue = new Map<string, ISmartChoice>();
  const choices: IChoice[] = [{ value: null, label: t('next.smart.asNow'), sub: null }];
  const add = (choice: ISmartChoice, label: string) => {
    byValue.set(valueOf(choice), choice);
    choices.push({ value: valueOf(choice), label, sub: null });
  };
  if (feature.agents) {
    add({ provider: SMART_OFF, model: '' }, t('next.smart.off'));
    for (const agent of SMART_AGENTS)
      for (const model of agentModels[agent])
        add({ provider: agent, model: model.id }, `${t(`next.settings.section.${agent}`)} · ${model.label}`);
  }
  for (const provider of providers)
    for (const model of provider.models) {
      add({ provider: provider.id, model: model.id }, `${provider.name} · ${model.label}`);
      // A model with levels of reasoning is offered at each of them too.
      for (const effort of model.efforts ?? [])
        add(
          { provider: provider.id, model: model.id, effort },
          `${provider.name} · ${model.label} · ${effortLabel(effort)}`,
        );
    }
  const own = feature.choice;
  if (own && !byValue.has(valueOf(own))) {
    const name = providers.find((p) => p.id === own.provider)?.name ?? own.provider;
    add(own, `${name} · ${own.model}`);
  }
  return { choices, byValue };
};

/** The line under a feature's name: what it is, that it waits for its day, or why its last call failed. */
const hintOf = (feature: ISmartFeature): string => {
  if (feature.choice && feature.failure !== null) return t('next.smart.failed', { reason: feature.failure });
  if (!feature.live) return `${feature.description} ${t('next.smart.notLive')}`;
  return feature.description;
};

const SmartSection = ({ state, busy, marks, agentModels, onPick, onOpenProviders }: ISmartSectionProps) => {
  if (state.k === 'loading')
    return (
      <div className="hx-set__group">
        <Skeleton className="hx-set__sk" />
      </div>
    );
  if (state.k === 'failed')
    return (
      <Group>
        <div className="hx-setlog__failed">
          <LoadFailed
            size={LOAD_FAILED_SIZE.inline}
            title={t('next.smart.loadFailed')}
            reason={state.text}
            onRetry={state.onRetry}
          />
        </div>
      </Group>
    );
  const { features, providers } = state.view;
  const none = providers.every((p) => p.models.length === 0);
  return (
    <Group
      foot={
        <>
          {t('next.smart.foot')}
          {none && (
            <>
              {' '}
              <button type="button" className="hx-set__chg-undo" onClick={onOpenProviders}>
                {t('next.smart.connect')}
              </button>
            </>
          )}
        </>
      }
    >
      {features.length === 0 && <div className="hx-set__empty">{t('next.smart.empty')}</div>}
      {features.map((feature) => {
        const { choices, byValue } = smartChoices(feature, providers, agentModels);
        return (
          <div
            key={feature.id}
            className="hx-smart__row"
            data-failed={(feature.choice !== null && feature.failure !== null) || undefined}
          >
            <MenuRow
              label={feature.title}
              hint={hintOf(feature)}
              value={feature.choice ? valueOf(feature.choice) : null}
              choices={choices}
              busy={busy.has(feature.id)}
              mark={markOf(marks, smartKey(feature.id))}
              onPick={(value) =>
                onPick(feature.id, value === null ? null : (byValue.get(String(value)) ?? null))
              }
            />
          </div>
        );
      })}
    </Group>
  );
};

export default SmartSection;
