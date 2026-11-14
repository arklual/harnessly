// What the UI slots put on the screens that exist: the «Extensions» page of Settings (the settings views and the
// onboarding steps), the action row at an object and the blocks of the start page. Every piece here is empty without HARNESSLY_UI_SLOTS=on.
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { useInstalledApps } from '@/next/features/app-views/api';
import {
  applyBranding,
  brandingOf,
  slotViews,
  type ISlotEntry,
  type ISlotObject,
} from '@/next/features/app-views/slots';
import { ExtensionsManage } from '@/next/features/app-views/extensions-manage';
import UiSlot from '@/next/features/app-views/ui-slot';
import { Group } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';

const DONE_KEY = 'hl.slot.done';

const readDone = (): Set<string> => {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(DONE_KEY) ?? '[]');
    return new Set(Array.isArray(raw) ? raw.filter((x): x is string => typeof x === 'string') : []);
  } catch {
    return new Set();
  }
};

const writeDone = (done: Set<string>) => {
  try {
    localStorage.setItem(DONE_KEY, JSON.stringify([...done]));
  } catch {
    // The marks are a convenience of this browser; without storage the step simply asks again.
  }
};

const keyOf = (e: ISlotEntry) => `${e.app.id}/${e.view.id}`;
const titleOf = (e: ISlotEntry) => e.view.label ?? e.app.name;

/** Whether the Extensions section has anything to show: the nav hides it otherwise. */
export const useHasExtensions = (machine: MachineId): boolean => {
  const { list } = useInstalledApps(machine);
  // With the slots on the page always opens: it is where an extension is installed.
  return list?.slots === true;
};

export const ExtensionsPage = ({ machine }: { machine: MachineId }) => {
  const { list } = useInstalledApps(machine);
  const [done, setDone] = useState(readDone);
  const finish = useCallback((key: string) => {
    setDone((had) => {
      const next = new Set(had).add(key);
      writeDone(next);
      return next;
    });
  }, []);
  if (list === null) return null;
  const steps = slotViews(list, 'onboarding');
  const pages = slotViews(list, 'settings');
  return (
    <>
      <ExtensionsManage machine={machine} />
      {steps.length > 0 && (
        <Group
          title={t('next.slot.steps')}
          foot={t('next.slot.stepsFoot', {
            done: steps.filter((s) => done.has(keyOf(s))).length,
            all: steps.length,
          })}
        >
          {steps.map((s) => (
            <div key={keyOf(s)} className="hx-uislot-step" data-done={done.has(keyOf(s))}>
              <div className="hx-uislot-step__head">
                <Icon name={done.has(keyOf(s)) ? 'check' : (s.view.icon ?? 'circle')} size="sm" />
                <span>{titleOf(s)}</span>
              </div>
              <UiSlot
                machine={machine}
                app={s.app}
                view={s.view}
                target="onboarding"
                viewPort={list.viewPort}
                onDone={() => finish(keyOf(s))}
              />
            </div>
          ))}
        </Group>
      )}
      {pages.map((s) => (
        <Group key={keyOf(s)} title={titleOf(s)}>
          <UiSlot machine={machine} app={s.app} view={s.view} target="settings" viewPort={list.viewPort} />
        </Group>
      ))}
    </>
  );
};

/** The buttons the apps put at an object (a machine, a chat); nothing at all when no app does. */
export const ObjectActions = ({ machine, object }: { machine: MachineId; object: ISlotObject }) => {
  const { list } = useInstalledApps(machine);
  const actions = slotViews(list, 'object-action', object.kind);
  if (list === null || actions.length === 0) return null;
  return (
    <div
      className="hx-uislot-actions"
      role="group"
      aria-label={t('next.slot.actions', { name: object.name })}
    >
      {actions.map((a) => (
        <UiSlot
          key={keyOf(a)}
          machine={machine}
          app={a.app}
          view={a.view}
          target="object-action"
          viewPort={list.viewPort}
          object={object}
        />
      ))}
    </div>
  );
};

/** A view of the start page that takes the whole stage under the field (`"height": "stage"`), not a block under it. */
export const isStageView = (e: ISlotEntry) => e.view.height === 'stage';

/**
 * The blocks the apps put on the start page, under the field of a new chat (a brief of the day, a queue of work):
 * nothing at all when no app fills the `home` slot, so the page stays as it was. A view that takes the stage is drawn by
 * useHomeStage instead, and then the blocks are not drawn at all.
 */
export const HomeSlots = ({ machine }: { machine: MachineId | null }) => {
  // Without a machine to start on there is nobody to ask; the hook still runs, switched off.
  const { list } = useInstalledApps(machine ?? ('' as MachineId), 0, machine !== null);
  const views = slotViews(list, 'home');
  // A view that takes the stage owns the page: the blocks would stand over it.
  const blocks = views.some(isStageView) ? [] : views;
  if (machine === null || list === null || blocks.length === 0) return null;
  return (
    <div className="hx-uislot-home">
      {blocks.map((b) => (
        <UiSlot
          key={keyOf(b)}
          machine={machine}
          app={b.app}
          view={b.view}
          target="home"
          viewPort={list.viewPort}
        />
      ))}
    </div>
  );
};

export interface IHomeGreeting {
  title: string;
  sub: string;
}

/**
 * The view that takes the start page's stage under the field (the first by order when several apps ask for it), and the
 * greeting it put over the field. Null when no app asks for the stage: the page stays as it was.
 */
export const useHomeStage = (
  machine: MachineId | null,
): { stage: ReactNode; greeting: IHomeGreeting | null } => {
  const { list } = useInstalledApps(machine ?? ('' as MachineId), 0, machine !== null);
  const entry = slotViews(list, 'home').find(isStageView) ?? null;
  const key = entry ? keyOf(entry) : null;
  const [greeting, setGreeting] = useState<{ key: string; value: IHomeGreeting } | null>(null);
  const onGreet = (title: string, sub: string) => {
    if (key !== null) setGreeting(title === '' ? null : { key, value: { title, sub } });
  };
  if (machine === null || list === null || entry === null) return { stage: null, greeting: null };
  return {
    stage: (
      <div className="hx-home-stage">
        <UiSlot
          key={keyOf(entry)}
          machine={machine}
          app={entry.app}
          view={entry.view}
          target="home"
          viewPort={list.viewPort}
          onGreet={onGreet}
          fill
        />
      </div>
    ),
    // A greeting is the view's own: another view, or the same one gone, leaves the host's question.
    greeting: greeting !== null && greeting.key === key ? greeting.value : null,
  };
};

/** Sets the product name and accent an app brings (the branding slot) on the page; draws nothing. */
export const BrandingHost = ({ machine }: { machine: MachineId }) => {
  const { list } = useInstalledApps(machine);
  const branding = brandingOf(list);
  useEffect(() => (branding ? applyBranding(branding) : undefined), [branding]);
  return null;
};
