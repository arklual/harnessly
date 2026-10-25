export interface ITextPart {
  text: string;
  hit: boolean;
}

/** The text cut into plain and matching parts: every word of the query, case and «ё» ignored. */
export const partsOf = (text: string, query: string): ITextPart[] => {
  const words = query
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map((word) => word.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').replace(/[её]/gi, '[её]'));
  if (words.length === 0) return [{ text, hit: false }];
  const pattern = new RegExp(`(${words.join('|')})`, 'gi');
  return text
    .split(pattern)
    .filter((part) => part !== '')
    .map((part) => ({ text: part, hit: new RegExp(`^(${words.join('|')})$`, 'i').test(part) }));
};
