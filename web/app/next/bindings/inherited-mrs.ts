// The merge requests a chat inherits: those its subchats and subagents opened, off GET …/made (the runtime marks each
// with `from`; inherited-made.ts asks for it).
import type { IDoneMr } from '@/next/features/chat/mr-done';
import { fromOf } from '@/next/features/pane/made-model';
import type { MadeItem } from '@/next/protocol/v2';

/** The inherited merge requests of a list of what the chat made, each as the block's row. */
export const inheritedMrs = (items: readonly MadeItem[]): IDoneMr[] =>
  items
    .filter((item) => item.kind === 'mr' && item.from)
    .map((item) => ({
      output: { kind: 'mr', op: item.op, ref: item.ref, title: item.title },
      from: fromOf(item),
    }));
