// Unsent text of a composer, kept per conversation in this browser so a reload or a switch does not lose it.
const KEY = (id: string) => `hl-draft:${id}`;

export const loadInputDraft = (id: string): string => {
  try {
    return window.localStorage.getItem(KEY(id)) ?? '';
  } catch {
    return '';
  }
};

export const saveInputDraft = (id: string, text: string): void => {
  try {
    if (text.trim()) window.localStorage.setItem(KEY(id), text);
    else window.localStorage.removeItem(KEY(id));
  } catch {
    // storage unavailable: the draft lives as long as the page
  }
};

export const clearInputDraft = (id: string): void => saveInputDraft(id, '');
