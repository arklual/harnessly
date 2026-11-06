// An app's card whose tool result came in cut (an event carries the first 4 KB of an output): the whole output is asked
// once, the step in the store gets it, and the feed draws the card from it as from any other.
import { useEffect } from 'react';
import { loadOutput } from '@/next/commands/pages';
import { useAppViewHost } from '@/next/features/app-views/host';
import { pluginToolOf } from '@/next/features/app-views/card';
import type { ToolEntry } from '@/next/features/chat/types';

/** A plugin's tool step whose cut output starts like an app's card. */
export const cardOutputPending = (entry: {
  t: string;
  tool?: string;
  output?: ToolEntry['output'];
}): boolean =>
  entry.t === 'tool' &&
  entry.output?.truncated != null &&
  entry.output.text.trimStart().startsWith('{') &&
  entry.output.text.includes('"resourceUri"') &&
  pluginToolOf(entry.tool ?? '') !== null;

const asked = new Set<string>();

const CardOutput = ({ entry }: { entry: ToolEntry }) => {
  const host = useAppViewHost();
  useEffect(() => {
    if (host === null || host.conversation === null) return;
    const key = `${host.machine}|${host.conversation}|${entry.id}`;
    if (asked.has(key)) return;
    asked.add(key);
    void loadOutput(host.machine, host.conversation, entry.id).catch(() => asked.delete(key));
  }, [host, entry.id]);
  return <div className="hx-appcard hx-appcard--loading" aria-busy="true" />;
};

export default CardOutput;
