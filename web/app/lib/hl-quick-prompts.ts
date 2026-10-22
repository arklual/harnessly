export interface IQuickPrompt {
  id: string;
  name: string;
  prompt: string;
  enabled: boolean;
}

/** What `/api/quick-prompts` answers. */
export interface IQuickPromptsData {
  builtins: IQuickPrompt[];
  custom: IQuickPrompt[];
  order: string[];
}

/** Items in the saved order; those the order does not name follow in their own. */
export const applyOrder = <T extends { id: string }>(items: readonly T[], order: readonly string[]): T[] => {
  if (order.length === 0) return [...items];
  const rest = new Map(items.map((item) => [item.id, item]));
  const ordered: T[] = [];
  for (const id of order) {
    const item = rest.get(id);
    if (!item) continue;
    ordered.push(item);
    rest.delete(id);
  }
  return [...ordered, ...rest.values()];
};

/** The prompts the bar under the composer shows: the enabled built-in and custom ones, in the saved order. */
export const enabledQuickPrompts = (data: IQuickPromptsData | undefined): IQuickPrompt[] => {
  if (!data) return [];
  const all = [...(data.builtins ?? []), ...(data.custom ?? [])].filter((p) => p.enabled && p.prompt.trim());
  return applyOrder(all, data.order ?? []);
};
