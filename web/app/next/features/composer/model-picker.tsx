// The pill over the field and its popover (the design's .hx-model and .hx-picker): the first screen is the depth of
// thinking on a slider of the stops the model takes, a tap on the agent and model goes to the second, the agents in
// segments and their models in a sideways carousel. In a created chat the agent is fixed and only its models show.
// Ultracode (Claude Code's mode of dynamic workflows with subagents) is no stop of the slider: a switch of its own under
// it, on the models that run it, and on the pill a glow and a mark of its own beside «model · depth». The pill and the
// row's glow are what the agent was last seen running; a turn of the switch the agent has not confirmed yet shows on the
// switch and is said under it, never on the pill. On the phone it stays the same compact popover over the pill (the
// design's mobile.css keeps it one), its rows a finger tall. A new chat of a person with several machines also says
// the machine it starts on, a row under the depth that opens a third screen of the machines: another one
// brings that machine's agents and models, the ones logged in there.
import { useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import {
  agentKey,
  choiceAgentKey,
  effortHint,
  effortLabel,
  unavailableNote,
  modelName,
  modelOf,
  withModel,
  type IModelChoice,
  type IPickerAgent,
} from '@/next/features/composer/agents';
import { choiceKey } from '@/next/features/composer/multi-model';
import { EFFORTS, type AgentModel, type Effort } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import Switch from '@/next/ui/switch';
import { usePresentFlag } from '@/next/ui/presence';
import type { IMachineChoice } from '@/next/features/sidebar/types';
import { t } from '@/next/i18n';
import '@/next/styles/picker.css';

const EXIT_MS = 240;

const VIEW = { effort: 'effort', models: 'models', machines: 'machines' } as const;
type TView = (typeof VIEW)[keyof typeof VIEW];

export interface IModelPickerProps {
  value: IModelChoice;
  /** The agents to offer: every ready one for a new chat, the chat's own one in a created chat. */
  agents: readonly IPickerAgent[];
  /** A created chat: the agent stays, only its models and the depth change. */
  locked: boolean;
  /** A change waits for the agent's answer: the pill dims, the choice stays as the agent last said. */
  busy?: boolean;
  /** Why a change cannot be made now (the agent is mid-turn, cannot switch); the picker only shows. */
  blocked?: string | null;
  /** The switch of ultracode shows (on a model that runs it); false where the start does not take the mode. */
  offersUltracode?: boolean;
  /** The mode an ask waits for the agent to turn to; null or absent when none waits. value.ultracode is the mode seen. */
  ultracodeAsked?: boolean | null;
  onChange: (choice: IModelChoice) => void;
  /**
   * Several models for one prompt (a beta function, a new chat only): `chosen` is every model of the set, the pill's own
   * first; Shift, Ctrl or ⌘ with a click, or the box before a model, adds it to the set or takes it out; a plain click
   * is the one model, as ever.
   */
  multi?: { chosen: readonly IModelChoice[]; onToggle: (choice: IModelChoice) => void };
  /**
   * A created chat offers the other agents too: a model of one of them is no switch but a handoff of the context to a
   * new chat (`onPick` opens its preview). Absent: only the chat's own agent shows.
   */
  handoff?: {
    agents: readonly IPickerAgent[];
    onPick: (target: IPickerAgent, model: AgentModel) => void;
  } | null;
  /**
   * The machine a new chat starts on, for a person with several: `choices` every machine, the default
   * first; one that cannot take a chat now is dimmed with the word why. Absent: the row does not show.
   */
  machine?: {
    current: string;
    choices: readonly IMachineChoice[];
    onPick: (id: string) => void;
  } | null;
}

/** The stops of a model's slider, in the order of the protocol's efforts. */
const stopsOf = (model: AgentModel | null): Effort[] =>
  model ? EFFORTS.filter((effort) => model.efforts.includes(effort)) : [];

/** What the row says under the name while a turn of the switch waits for the agent. */
const ULTRACODE_WAITS = {
  on: 'next.picker.ultracodeWaitsOn',
  off: 'next.picker.ultracodeWaitsOff',
} as const;

const ModelPicker = ({
  value,
  agents,
  locked,
  busy,
  blocked,
  offersUltracode = true,
  ultracodeAsked = null,
  onChange,
  multi,
  handoff = null,
  machine = null,
}: IModelPickerProps) => {
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<TView>(VIEW.effort);
  const [browse, setBrowse] = useState(choiceAgentKey(value));
  const { mounted, state } = usePresentFlag(open, EXIT_MS);
  const anchorRef = useRef<HTMLDivElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  const sliderRef = useRef<HTMLDivElement>(null);

  const agent = agents.find((a) => agentKey(a) === choiceAgentKey(value)) ?? agents[0];
  // The agents of the second screen: the chat's own, then those a handoff goes to.
  const listed = handoff
    ? [...agents, ...handoff.agents.filter((a) => !agents.some((own) => agentKey(own) === agentKey(a)))]
    : agents;
  const segmented = !locked || listed.length > 1;
  const model = modelOf(agent, value.model);
  // The depth is the one the chat is known to run at: a model's default is not made up for a chat that never said.
  const effort = value.effort;
  const stops = stopsOf(model);
  const step = effort ? stops.indexOf(effort) : -1;
  const browseIndex = Math.max(
    0,
    listed.findIndex((a) => agentKey(a) === browse),
  );
  const frozen = !!blocked || !!busy;
  const ultracodeOffered = offersUltracode && model?.ultracode === true;
  const ultracode = ultracodeOffered && value.ultracode;
  // An ask that would turn the mode the other way and is not confirmed yet; null when none waits.
  const asked =
    ultracodeOffered && ultracodeAsked !== null && ultracodeAsked !== ultracode ? ultracodeAsked : null;
  const switchOn = asked ?? ultracode;
  const ultraHint =
    asked === null ? t('next.picker.ultracodeHint') : t(ULTRACODE_WAITS[asked ? 'on' : 'off']);
  // Where the thumb and the stops stand, for any number of stops (each model has its own count).
  const along = (i: number) => String(stops.length > 1 ? i / (stops.length - 1) : 0);
  useLayoutEffect(() => {
    const slider = sliderRef.current;
    if (!slider || step < 0) return;
    slider.style.setProperty('--p', along(step));
  });

  const close = () => {
    setOpen(false);
    window.setTimeout(() => setView(VIEW.effort), EXIT_MS);
  };
  const toggle = () => {
    if (open) {
      close();
      return;
    }
    setView(VIEW.effort);
    setBrowse(choiceAgentKey(value));
    setOpen(true);
  };
  const setStep = (i: number) => {
    if (frozen || stops.length === 0) return;
    const next = stops[Math.max(0, Math.min(stops.length - 1, i))];
    if (next !== effort) onChange({ ...value, effort: next });
  };

  // Outside press and Esc close; arrows move the depth on its screen.
  useEffect(() => {
    if (!open) return;
    const onDown = (event: PointerEvent) => {
      if (!anchorRef.current?.contains(event.target as Node)) close();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close();
      if (view === VIEW.effort && (event.key === 'ArrowRight' || event.key === 'ArrowLeft')) {
        setStep(step + (event.key === 'ArrowRight' ? 1 : -1));
      }
    };
    window.addEventListener('pointerdown', onDown);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', onDown);
      window.removeEventListener('keydown', onKey);
    };
  });

  // The popover's height follows the active screen; in the carousel only the visible list counts.
  useLayoutEffect(() => {
    const pop = popRef.current;
    if (!pop) return;
    const pane = pop.querySelector<HTMLElement>('.hx-picker__pane[data-active="true"]');
    if (!pane) return;
    let height = pane.offsetHeight;
    const carousel = pane.querySelector<HTMLElement>('.hx-carousel');
    const list = carousel?.querySelector<HTMLElement>('.hx-options[data-active="true"]');
    if (carousel && list) height -= carousel.offsetHeight - list.offsetHeight;
    pop.style.height = `${height}px`;
  });

  const stepAt = (x: number) => {
    const slider = sliderRef.current;
    if (!slider || stops.length < 2) return;
    const box = slider.getBoundingClientRect();
    const p = (x - box.left - box.height / 2) / (box.width - box.height);
    setStep(Math.round(p * (stops.length - 1)));
  };
  const onSliderDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (frozen) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    stepAt(event.clientX);
  };
  const onSliderMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.currentTarget.hasPointerCapture(event.pointerId)) stepAt(event.clientX);
  };

  // Another agent of a created chat is a handoff: the preview opens, the chat stays as it is.
  const crosses = (target: IPickerAgent) => locked && agentKey(target) !== choiceAgentKey(value);
  const pick = (target: IPickerAgent, next: AgentModel, add = false) => {
    if (handoff && crosses(target)) {
      close();
      handoff.onPick(target, next);
      return;
    }
    if (frozen) return;
    if (add && multi) {
      multi.onToggle(withModel(value, target, next));
      return;
    }
    onChange(withModel(value, target, next));
    setView(VIEW.effort);
  };
  const chosenKeys = new Set((multi?.chosen ?? []).map(choiceKey));
  const isChosen = (target: IPickerAgent, m: AgentModel) =>
    multi
      ? chosenKeys.has(choiceKey({ provider: target.provider, profile: target.profile, model: m.id }))
      : agentKey(target) === choiceAgentKey(value) && m.id === value.model;
  const more = multi ? multi.chosen.length - 1 : 0;
  const startsOn = machine?.choices.find((c) => c.id === machine.current) ?? null;

  const hint = (): string => {
    if (blocked) return blocked;
    if (stops.length === 0) return t('next.picker.effortNone');
    return effort ? effortHint(effort) : t('next.picker.effortUnknownHint');
  };

  return (
    <div className="hx-picker-anchor" ref={anchorRef}>
      <button
        type="button"
        className="hx-model hx-tipped"
        data-ultracode={ultracode || undefined}
        aria-expanded={open}
        aria-haspopup="dialog"
        aria-busy={busy || undefined}
        onClick={toggle}
      >
        {!open && <span className="hx-tip">{t('next.composer.modelTip')}</span>}
        <i className="hx-agent" data-agent={agent.vendor ?? value.provider} aria-hidden="true" />
        {ultracode && (
          <i
            className="hx-icon hx-model__ultra"
            data-i="workflow"
            data-size="sm"
            role="img"
            aria-label={t('next.picker.ultracode')}
          />
        )}
        <span className="hx-model__name">{modelName(agent, value.model)}</span>
        {effort && <span className="hx-model__effort">{effortLabel(effort)}</span>}
        {more > 0 && <span className="hx-model__more">{`+${more}`}</span>}
        <Icon name="chevron-down" size="sm" className="hx-model__chev" />
      </button>
      {mounted && (
        <div
          ref={popRef}
          className="hx-picker"
          data-state={state}
          role="dialog"
          aria-label={t('next.composer.modelTip')}
        >
          <div className="hx-picker__panes">
            <div className="hx-picker__pane" data-pane="effort" data-active={view === VIEW.effort}>
              <div className="hx-picker__top">
                <button
                  type="button"
                  className="hx-picker__center"
                  onClick={() => {
                    setBrowse(choiceAgentKey(value));
                    setView(VIEW.models);
                  }}
                >
                  <span className="hx-picker__effort">
                    {effort ? effortLabel(effort) : t('next.picker.effortUnknown')}
                  </span>
                  <span className="hx-picker__model">
                    <i className="hx-agent" data-agent={agent.vendor ?? value.provider} aria-hidden="true" />
                    {agent.name} · {modelName(agent, value.model)}
                    <Icon name="chevron-right" size="sm" />
                  </span>
                </button>
              </div>
              {stops.length > 1 && (
                <>
                  <div
                    ref={sliderRef}
                    className="hx-effort"
                    data-n={stops.length}
                    data-p={step < 0 ? undefined : step}
                    data-frozen={frozen || undefined}
                    role="slider"
                    aria-label={t('next.picker.effortLabel')}
                    aria-valuemin={0}
                    aria-valuemax={stops.length - 1}
                    aria-valuenow={step < 0 ? undefined : step}
                    aria-valuetext={effort ? effortLabel(effort) : t('next.picker.effortUnknown')}
                    aria-disabled={frozen || undefined}
                    onPointerDown={onSliderDown}
                    onPointerMove={onSliderMove}
                  >
                    <div className="hx-effort__fill" />
                    {stops.map((stop, i) => (
                      <button
                        key={stop}
                        type="button"
                        tabIndex={-1}
                        className="hx-effort__stop"
                        data-k={i}
                        ref={(el) => el?.style.setProperty('--k', along(i))}
                        data-on={i <= step}
                        aria-label={effortLabel(stop)}
                      />
                    ))}
                    <div className="hx-effort__thumb" />
                  </div>
                  <div
                    className="hx-effort__labels"
                    ref={(el) => el?.style.setProperty('--n', String(stops.length))}
                    aria-hidden="true"
                  >
                    {stops.map((stop) => (
                      <span
                        key={stop}
                        className="hx-effort__label"
                        data-chosen={stop === effort || undefined}
                      >
                        {effortLabel(stop)}
                      </span>
                    ))}
                  </div>
                </>
              )}
              <div className="hx-picker__hint">{hint()}</div>
              {ultracodeOffered && (
                <div
                  className="hx-picker__ultra"
                  data-on={ultracode || undefined}
                  data-pending={asked !== null || undefined}
                  title={t('next.picker.ultracodeTitle')}
                >
                  <Icon name="workflow" size="sm" className="hx-picker__ultra-ic" />
                  <span className="hx-picker__ultra-text">
                    <span className="hx-picker__ultra-name">{t('next.picker.ultracode')}</span>
                    <span className="hx-picker__ultra-hint">{ultraHint}</span>
                  </span>
                  <Switch
                    label={t('next.picker.ultracode')}
                    checked={switchOn}
                    disabled={frozen}
                    onChange={(on) => onChange({ ...value, ultracode: on })}
                  />
                </div>
              )}
              {machine && startsOn && (
                <button type="button" className="hx-picker__vm" onClick={() => setView(VIEW.machines)}>
                  <Icon name="server" size="sm" className="hx-picker__vm-ic" />
                  <span className="hx-picker__vm-name">
                    {t('next.picker.machineOn', { machine: startsOn.name })}
                  </span>
                  <Icon name="chevron-right" size="sm" className="hx-picker__vm-chev" />
                </button>
              )}
            </div>

            <div className="hx-picker__pane" data-pane="models" data-active={view === VIEW.models}>
              <div className="hx-picker__head">
                <button
                  type="button"
                  className="hx-picker__back"
                  aria-label={t('next.picker.back')}
                  onClick={() => setView(VIEW.effort)}
                >
                  <Icon name="chevron-left" size="sm" />
                  <span>{segmented ? t('next.picker.agentModelTitle') : t('next.picker.modelTitle')}</span>
                </button>
              </div>
              {segmented && (
                <div className="hx-seg" data-n={listed.length} data-a={browseIndex}>
                  <span className="hx-seg__glider" />
                  {listed.map((a) => (
                    <button
                      key={agentKey(a)}
                      type="button"
                      className="hx-seg__item"
                      aria-pressed={agentKey(a) === browse}
                      onClick={() => setBrowse(agentKey(a))}
                    >
                      <i className="hx-agent" data-agent={a.vendor ?? a.provider} aria-hidden="true" />
                      {a.short}
                    </button>
                  ))}
                </div>
              )}
              <div className="hx-carousel">
                <div className="hx-carousel__track" data-n={listed.length} data-a={browseIndex}>
                  {listed.map((a, index) => {
                    const active = index === browseIndex;
                    const across = crosses(a);
                    return (
                      <div key={agentKey(a)} className="hx-options" data-active={active} role="listbox">
                        {!a.available && <div className="hx-options__note">{unavailableNote(a)}</div>}
                        {a.available && across && (
                          <div className="hx-options__note">
                            {t('next.handoff.pickNote', { agent: a.short })}
                          </div>
                        )}
                        {a.available &&
                          a.models.map((m) => (
                            <div key={m.id} className="hx-option-row" data-multi={multi ? '' : undefined}>
                              {multi && !across && (
                                <button
                                  type="button"
                                  role="checkbox"
                                  className="hx-option__box"
                                  tabIndex={active ? 0 : -1}
                                  disabled={frozen}
                                  aria-checked={isChosen(a, m)}
                                  aria-label={t('next.picker.multiAdd', { model: m.label })}
                                  onClick={() => pick(a, m, true)}
                                >
                                  <Icon name="check" size="sm" />
                                </button>
                              )}
                              <button
                                type="button"
                                role="option"
                                className="hx-option"
                                tabIndex={active ? 0 : -1}
                                disabled={frozen && !across}
                                data-handoff={across || undefined}
                                aria-selected={isChosen(a, m)}
                                onClick={(event) =>
                                  pick(a, m, event.shiftKey || event.ctrlKey || event.metaKey)
                                }
                              >
                                <span className="hx-option__text">
                                  <span className="hx-option__name">{m.label}</span>
                                  {across && (
                                    <span className="hx-option__desc">
                                      {t('next.handoff.pickOption', { agent: a.short })}
                                    </span>
                                  )}
                                  {!across && m.hint && <span className="hx-option__desc">{m.hint}</span>}
                                </span>
                                {across ? (
                                  <Icon name="arrow-left-right" size="sm" className="hx-option__go" />
                                ) : (
                                  <Icon name="check" size="sm" className="hx-option__check" />
                                )}
                              </button>
                            </div>
                          ))}
                      </div>
                    );
                  })}
                </div>
              </div>
              {multi && <div className="hx-picker__hint">{t('next.picker.multiHint')}</div>}
              {blocked && <div className="hx-picker__hint">{blocked}</div>}
            </div>

            {machine && (
              <div className="hx-picker__pane" data-pane="machines" data-active={view === VIEW.machines}>
                <div className="hx-picker__head">
                  <button
                    type="button"
                    className="hx-picker__back"
                    aria-label={t('next.picker.back')}
                    onClick={() => setView(VIEW.effort)}
                  >
                    <Icon name="chevron-left" size="sm" />
                    <span>{t('next.picker.machineTitle')}</span>
                  </button>
                </div>
                <div className="hx-options" data-active="true" role="listbox">
                  {machine.choices.map((c) => (
                    <button
                      key={c.id}
                      type="button"
                      role="option"
                      className="hx-option"
                      tabIndex={view === VIEW.machines ? 0 : -1}
                      disabled={c.off}
                      aria-selected={c.id === machine.current}
                      onClick={() => {
                        machine.onPick(c.id);
                        setView(VIEW.effort);
                      }}
                    >
                      <span className="hx-option__text">
                        <span className="hx-option__name">{c.name}</span>
                        {c.note && <span className="hx-option__desc">{c.note}</span>}
                      </span>
                      <Icon name="check" size="sm" className="hx-option__check" />
                    </button>
                  ))}
                </div>
                <div className="hx-picker__hint">{t('next.picker.machineHint')}</div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default ModelPicker;
