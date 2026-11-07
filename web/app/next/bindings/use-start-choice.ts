// The model a new chat starts on, with the pill to change it (features/composer/start-choice.ts says the order): the
// start page's field and a design's first message take it the same way.
import { useEffect, useState } from 'react';
import { pickerAgents, type IModelChoice, type IPickerAgent } from '@/next/features/composer/agents';
import { useTourStep } from '@/next/features/tour/use-tour-step';
import { tourFact } from '@/next/stores/tour';
import { readLastChoice, rememberChoice, startChoice } from '@/next/features/composer/start-choice';
import { useAgentSettings } from '@/next/bindings/settings-data';
import type { MachineId } from '@/next/protocol/v2';
import { useMachine } from '@/next/stores/machines';

/** The quickest and cheapest model the machine has (Haiku, a mini), for a lesson's request; null when none is ready. */
export const cheapestChoice = (ready: readonly IPickerAgent[]): IModelChoice | null => {
  const tiers = [/haiku/i, /mini|nano|flash|lite/i];
  for (const tier of tiers) {
    for (const agent of ready) {
      const model = agent.models.find((m) => tier.test(m.id) || tier.test(m.label));
      if (!model) continue;
      return {
        provider: agent.provider,
        profile: agent.profile ?? null,
        model: model.id,
        effort: model.efforts.includes('low') ? 'low' : null,
        ultracode: false,
      };
    }
  }
  return null;
};

/** `plainOnly`: a start that has no backend profile (a design's own request) is offered the CLIs' own groups only. */
export const useStartChoice = (machine: MachineId | null, plainOnly = false) => {
  const machineView = useMachine(machine);
  const agents = pickerAgents(machineView?.machine.agents ?? []).filter((a) => !plainOnly || !a.profile);
  const [picked, setPicked] = useState<IModelChoice | null>(null);
  const [last] = useState(readLastChoice);
  // A new chat starts on the person's pick, else the default of the agent's settings file, else the last pick, and
  // only else the agent's first model; a pick of an agent the machine no longer runs gives way the same.
  const ready = agents.filter((a) => a.available);
  const readsSettings = (provider: IModelChoice['provider']) =>
    machine !== null && ready.some((a) => a.provider === provider && !a.profile);
  const claudeSettings = useAgentSettings(machine ?? '', 'claude', readsSettings('claude'));
  const codexSettings = useAgentSettings(machine ?? '', 'codex', readsSettings('codex'));
  const choice = startChoice({
    ready,
    picked,
    last,
    settings: {
      claude: claudeSettings.state.k === 'ready' ? claudeSettings.state.value.options : null,
      codex: codexSettings.state.k === 'ready' ? codexSettings.state.value.options : null,
    },
  });
  // A lesson's request goes on the cheapest model unless the person picked one; with no agent at all the lesson passes
  // over the request (tourFact `agent:none`).
  const cheap = useTourStep()?.cheap === true;
  const known = machineView?.machine.agents !== undefined;
  const none = known && ready.length === 0;
  useEffect(() => {
    if (known) tourFact('agent:none', none);
  }, [known, none]);
  const lesson = cheap && picked === null ? cheapestChoice(ready) : null;
  const onPick = (next: IModelChoice) => {
    setPicked(next);
    rememberChoice(next);
  };
  return { choice: lesson ?? choice, agents, onPick };
};
