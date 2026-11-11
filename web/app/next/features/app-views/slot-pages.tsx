// What the UI slots put on the screens that exist: the «Extensions» page of Settings (the settings views and the
// onboarding steps) and the action row at an object. Every piece here is empty without HARNESSLY_UI_SLOTS=on.
import { useCallback, useEffect, useState } from 'react';
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

/** Sets the product name and accent an app brings (the branding slot) on the page; draws nothing. */
export const BrandingHost = ({ machine }: { machine: MachineId }) => {
  const { list } = useInstalledApps(machine);
  const branding = brandingOf(list);
  useEffect(() => (branding ? applyBranding(branding) : undefined), [branding]);
  return null;
};
