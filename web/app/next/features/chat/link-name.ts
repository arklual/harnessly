// What a bare address in the agent's text says on its chip: a link to a document or a board is named by the
// document (the hub's card, link-card-meta.ts), until that arrives by its slug read as words; a file is its name.
// A link written with a text of its own (`[Plan](https://…)`) keeps that text: only an address with no words is renamed.

// The portal ends a slug with a 6-character id (`eval-laboratoriya-proektnaya-doka-uuwhtz`).
const SLUG_ID = /-[a-z0-9]{6}$/i;

/** Whether the link's text is no more than its address (an autolink, `[https://…](https://…)`, an empty text). */
export const isBareAddress = (text: string, href: string): boolean => {
  const bare = (s: string) => s.trim().replace(/\/+$/, '');
  return bare(text) === '' || bare(text) === bare(href);
};

/** A slug read as words: the id at its tail dropped, dashes turned into spaces: «eval laboratoriya proektnaya doka». */
export const slugWords = (slug: string): string => {
  let raw = slug;
  try {
    raw = decodeURIComponent(slug);
  } catch {
    // the slug stays as it is
  }
  const head = raw.replace(SLUG_ID, '');
  const words = (head === '' ? raw : head).replace(/[-_]+/g, ' ').trim();
  return words === '' ? raw : words;
};
