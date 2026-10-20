// The key bound to an action of the agents page (react-hotkeys-hook syntax).
const DEFAULT_KEYS: Record<string, string> = {
  'app.voice_hold': 'ctrl+space',
};

export const useResolvedKey = (action: string): string | null => DEFAULT_KEYS[action] ?? null;
