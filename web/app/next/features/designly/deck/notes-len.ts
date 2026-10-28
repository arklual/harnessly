// How long a slide's notes take to say, as the runtime counts it (internal/design/deck_types.go, DeckNotesSeconds):
// max(10, round(words / 2.2 / 5) * 5) seconds. No React here.

/** The words of a text: runs of anything but white space. */
export const deckWords = (text: string): number => (text.trim() === '' ? 0 : text.trim().split(/\s+/).length);

export const deckNotesSeconds = (text: string): number =>
  Math.max(10, Math.round(deckWords(text) / 2.2 / 5) * 5);
