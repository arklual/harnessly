// Saved prompts the composer offers (the runtime's /api/quick-prompts).
export interface IQuickPrompt {
  id: string;
  name: string;
  prompt: string;
  enabled?: boolean;
}

export interface IQuickPromptsData {
  prompts: IQuickPrompt[];
}
