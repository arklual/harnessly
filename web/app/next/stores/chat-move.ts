// The chat whose «Продолжить на другой ВМ» was asked for from a menu, and what the page knows of the switch.
import { useEffect, useRef, useState } from 'react';
import { moveEnabled } from '@/next/features/chat-move/api';
import { requestUi, UI_REQUEST, useUiRequest } from '@/next/stores/ui-requests';

let moveFor: string | null = null;

/** Asks the chat `id` to open its «Продолжить на другой ВМ» sheet, now or as soon as it is on the page. */
export const requestMoveVm = (id: string): void => {
  moveFor = id;
  requestUi(UI_REQUEST.moveVm);
};

/** Runs `handler` when the chat `id` is asked to continue on another machine, also when it was asked before it mounted. */
export const useMoveVmRequest = (id: string, handler: () => void): void => {
  const latest = useRef(handler);
  useEffect(() => {
    latest.current = handler;
  });
  const take = () => {
    if (moveFor !== id) return;
    moveFor = null;
    latest.current();
  };
  useUiRequest(UI_REQUEST.moveVm, take);
  useEffect(take, [id]);
};

const NOTHING: ReadonlySet<string> = new Set();

/** The machines «Продолжить на другой ВМ» is on for, of those asked; none until the gateway says so. */
export const useMoveEnabledOn = (machines: readonly string[]): ReadonlySet<string> => {
  const key = machines.join('\n');
  const [on, setOn] = useState<{ key: string; set: ReadonlySet<string> }>({ key: '', set: new Set() });
  useEffect(() => {
    let alive = true;
    const ids = key === '' ? [] : key.split('\n');
    void Promise.all(ids.map(async (m) => ((await moveEnabled(m)) ? m : null))).then((list) => {
      if (alive) setOn({ key, set: new Set(list.filter((m): m is string => m !== null)) });
    });
    return () => {
      alive = false;
    };
  }, [key]);
  return on.key === key ? on.set : NOTHING;
};

/** Whether «Продолжить на другой ВМ» is on for the machine; false until the gateway says so. */
export const useMoveEnabled = (machine: string | null): boolean => {
  const [on, setOn] = useState<{ machine: string; value: boolean } | null>(null);
  useEffect(() => {
    if (!machine) return;
    let alive = true;
    void moveEnabled(machine).then((value) => {
      if (alive) setOn({ machine, value });
    });
    return () => {
      alive = false;
    };
  }, [machine]);
  return machine !== null && on?.machine === machine && on.value;
};
