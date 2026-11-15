// The picker's tabs by vendor (15.11: «почему стало 4»): Claude · OpenAI · the other vendors. OpenAI's three ways of
// working (harnessly's agent on GPT models, Codex, a plain chat) are one tab with a row of how it works under it.
import type { IPickerAgent } from '@/next/features/composer/agents';

/** The key of OpenAI's tab. */
export const OPENAI = '~openai';

/** How a member of OpenAI's tab works, for its button in the row. */
export const OPENAI_MODE = { harnessly: 'harnessly', codex: 'codex', chat: 'chat' } as const;
export type TOpenAIMode = (typeof OPENAI_MODE)[keyof typeof OPENAI_MODE];

export interface IPickerGroup {
  key: string;
  /** The agents' keys in it, in the order of its row; one for a vendor of its own. */
  members: { key: string; mode: TOpenAIMode | null }[];
  /** The agent the tab is named by (its mark and short name); null for OpenAI. */
  agent: IPickerAgent | null;
}

const ORDER: readonly TOpenAIMode[] = [OPENAI_MODE.harnessly, OPENAI_MODE.codex, OPENAI_MODE.chat];

/** OpenAI's way of working an agent is, or null for another vendor. */
export const openAIMode = (a: IPickerAgent): TOpenAIMode | null => {
  if (a.vendor === 'gpt') return OPENAI_MODE.harnessly;
  if (!a.profile && a.provider === 'codex') return OPENAI_MODE.codex;
  return null;
};

/** The tabs: in the order the agents come, OpenAI where its first member stands; `extraKey` is the plain chat. */
export const pickerGroups = (
  agents: readonly IPickerAgent[],
  keyOf: (a: IPickerAgent) => string,
  extraKey: string | null,
): IPickerGroup[] => {
  const groups: IPickerGroup[] = [];
  let openai: IPickerGroup | null = null;
  const toOpenAI = (key: string, mode: TOpenAIMode) => {
    if (!openai) {
      openai = { key: OPENAI, members: [], agent: null };
      groups.push(openai);
    }
    openai.members.push({ key, mode });
  };
  for (const a of agents) {
    const mode = openAIMode(a);
    if (mode) toOpenAI(keyOf(a), mode);
    else groups.push({ key: keyOf(a), members: [{ key: keyOf(a), mode: null }], agent: a });
  }
  if (extraKey) toOpenAI(extraKey, OPENAI_MODE.chat);
  for (const g of groups)
    g.members.sort((x, y) => ORDER.indexOf(x.mode ?? 'harnessly') - ORDER.indexOf(y.mode ?? 'harnessly'));
  return groups;
};

/** The group a browsed key is in. */
export const groupOfKey = (groups: readonly IPickerGroup[], key: string): IPickerGroup | undefined =>
  groups.find((g) => g.members.some((m) => m.key === key));

/** A model's line under its name in Russian: the runtime's own when it is Russian, else from the words of its id. */
export const modelHint = (id: string, hint: string | undefined | null): string => {
  if (hint && /[а-яё]/i.test(hint)) return hint;
  const m = id.toLowerCase();
  if (/mini|nano|spark|flash|haiku/.test(m)) return 'Быстрая и лёгкая';
  if (/codex.*max|max.*codex/.test(m)) return 'Самая сильная для кода';
  if (/codex/.test(m)) return 'Для кода';
  if (/opus/.test(m)) return 'Самая сильная';
  if (/sonnet/.test(m)) return 'Сильная и быстрая';
  if (/^gpt-\d/.test(m)) return 'Общая модель';
  return '';
};
