// The merge requests a chat inherits: those its subchats and subagents opened, off GET …/made (the runtime marks each
// with `from`). Asked once the agent is done, and again after each next task; nothing while it works.
import { useEffect, useState } from 'react';
import { loadMade } from '@/next/commands/pages';
import type { IDoneMr } from '@/next/features/chat/mr-done';
import { fromOf } from '@/next/features/pane/made-model';
import type { ConvId, MachineId, MadeItem } from '@/next/protocol/v2';
import { hasFeature } from '@/next/stores/machines';

const NONE: readonly IDoneMr[] = [];

/** The inherited merge requests of a list of what the chat made, each as the block's row. */
export const inheritedMrs = (items: readonly MadeItem[]): IDoneMr[] =>
  items
    .filter((item) => item.kind === 'mr' && item.from)
    .map((item) => ({
      output: { kind: 'mr', op: item.op, ref: item.ref, title: item.title },
      from: fromOf(item),
    }));

/** `done` is whether the agent finished its task: the list is asked each time it becomes true. */
export const useInheritedMrs = (machine: MachineId, conv: ConvId, done: boolean): readonly IDoneMr[] => {
  const [mrs, setMrs] = useState<{ conv: ConvId; list: readonly IDoneMr[] }>({ conv, list: NONE });
  const announced = hasFeature(machine, 'made');
  useEffect(() => {
    if (!done || !announced) return;
    let live = true;
    void loadMade(machine, conv).then((outcome) => {
      if (live && 'ok' in outcome) setMrs({ conv, list: inheritedMrs(outcome.value.items) });
    });
    return () => {
      live = false;
    };
  }, [machine, conv, done, announced]);
  return mrs.conv === conv ? mrs.list : NONE;
};
