// `harnessly ask …` in the agent's shell, read off the tool step: who it asks and what, so the feed draws the ask's
// card in place of the command. The ask itself comes from the machine's list: by the id the command printed, or, for
// a command still waiting in the background (whose output the step does not hold), by its conversation and question.
import { createContext } from 'react';
import type { IAsk, TAskKind } from '@/next/features/asks/api';
import type { MachineId } from '@/next/protocol/v2';

export interface IAskCall {
  kind: TAskKind;
  to: string;
  question: string;
  /** The ask's id when the step's output already names it. */
  id: string | null;
}

const CLI_ASK = /^\$\s*[\w.-]+\s+ask\b([\s\S]*)$/;
const SUBCOMMANDS = new Set(['wait', 'status', 'show', 'cancel', 'people', 'contacts', 'list', 'ls']);
const WITH_VALUE = new Set([
  '--to',
  '-t',
  '--bot',
  '-b',
  '--details',
  '-d',
  '--link',
  '-l',
  '--option',
  '-o',
  '--timeout',
]);
const ASKED = /\basked (ask_[0-9a-f]{12})\b/;

/** Shell words of a command line: quotes and backslashes as a POSIX shell reads them, nothing expanded. */
export const shellWords = (line: string): string[] => {
  const out: string[] = [];
  let word = '';
  let has = false;
  let quote: '"' | "'" | null = null;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (quote) {
      if (c === quote) quote = null;
      else if (c === '\\' && quote === '"' && i + 1 < line.length && '"\\$`'.includes(line[i + 1]))
        word += line[++i];
      else word += c;
      continue;
    }
    if (c === '"' || c === "'") {
      quote = c;
      has = true;
    } else if (c === '\\' && i + 1 < line.length) {
      word += line[++i];
      has = true;
    } else if (/\s/.test(c)) {
      if (has || word) out.push(word);
      word = '';
      has = false;
    } else if ((c === '|' || c === ';' || c === '&' || c === '>' || c === '<') && !quote) {
      break; // the rest is another command or a redirection
    } else {
      word += c;
      has = true;
    }
  }
  if (has || word) out.push(word);
  return out;
};

/** The ask a shell step is, or null for any other command (and for ask's other subcommands). */
export const parseAskCall = (tool: string, summary: string, output: string | null): IAskCall | null => {
  if (!/^(bash|shell|exec)/i.test(tool)) return null;
  const m = CLI_ASK.exec(summary.split('\n')[0]);
  if (!m) return null;
  const words = shellWords(m[1]);
  let kind: TAskKind = 'question';
  if (words[0] === 'approve' || words[0] === 'approval') {
    kind = 'approval';
    words.shift();
  } else if (words[0] && SUBCOMMANDS.has(words[0])) return null;
  let to = '';
  const rest: string[] = [];
  for (let i = 0; i < words.length; i++) {
    const w = words[i];
    const [name, inline] =
      w.startsWith('--') && w.includes('=')
        ? [w.slice(0, w.indexOf('=')), w.slice(w.indexOf('=') + 1)]
        : [w, null];
    if (WITH_VALUE.has(name)) {
      const value = inline ?? words[++i] ?? '';
      if (name === '--to' || name === '-t') to = value;
      continue;
    }
    if (w === '--no-wait' || w === '--json') continue;
    rest.push(w);
  }
  const question = rest.join(' ').trim();
  const id = output ? (ASKED.exec(output)?.[1] ?? null) : null;
  return { kind, to, question: question === '-' ? '' : question, id };
};

const flat = (s: string) => s.replace(/\s+/g, ' ').trim();

/** The ask of a call among the machine's: by id, else the newest of the conversation with the same question. */
export const askOfCall = (
  asks: readonly IAsk[],
  call: IAskCall,
  conversation: string | null,
): IAsk | null => {
  if (call.id) return asks.find((a) => a.id === call.id) ?? null;
  const q = flat(call.question);
  if (!q) return null;
  return (
    asks.find(
      (a) =>
        a.kind === call.kind &&
        flat(a.question) === q &&
        (!conversation || !a.conversation || a.conversation === conversation),
    ) ?? null
  );
};

/** Where the feed's ask cards read from: the machine and the conversation on screen. */
export interface IAskSource {
  machine: MachineId;
  conversation: string;
}

export const AskSource = createContext<IAskSource | null>(null);
