// What a text answer is when the voice tells it: no code, no markup, not too long.
const RESULT_MAX = 1500;

/** The answer (markdown) as something to say. */
export const spokenResult = (md: string): string => {
  const plain = md
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/^[#>*\-\s]+/gm, '')
    .replace(/[*_~]+/g, '')
    .replace(/\s+/g, ' ')
    .trim();
  return plain.length > RESULT_MAX ? `${plain.slice(0, RESULT_MAX)}…` : plain;
};

/**
 * What a spoken task brings the agent (15.11: «попросил комментарий — он перерисовал UI»): the person's own words as
 * heard, and the voice's reading of them only as a note under them. Without heard words, the voice's instruction.
 */
export const spokenTask = (task: { text: string; said?: string }): string => {
  const said = task.said?.trim() ?? '';
  const read = task.text.trim();
  if (!said) return read;
  if (!read || read === said) return said;
  return `${said}\n\n(Голосом, дословно по расшифровке. Голос понял так: ${read})`;
};
