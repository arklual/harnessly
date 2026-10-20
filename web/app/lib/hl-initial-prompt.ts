export const MAX_INITIAL_PROMPT_LENGTH = 20_000;

/** ANSI-C quoting keeps multi-line prompts on one typed command line (bash and zsh both support $'…'). */
export const ansiCQuote = (value: string): string => {
  const body = value.replace(/[\\'\n\r\t]|[\u0000-\u001f\u007f]/g, (ch) => {
    if (ch === '\\') return '\\\\';
    if (ch === "'") return "\\'";
    if (ch === '\n') return '\\n';
    if (ch === '\r') return '\\r';
    if (ch === '\t') return '\\t';
    return `\\x${ch.charCodeAt(0).toString(16).padStart(2, '0')}`;
  });
  return `$'${body}'`;
};

/**
 * Longer prompts are not typed into the shell at all: tmux feeding a 16 KB command line into zsh
 * stalled mid-string and the agent never started (automation run 29.09). The caller
 * writes them to a file and the command line only reads it back.
 */
export const INLINE_PROMPT_MAX = 2_000;

export const initialPromptText = (prompt: unknown): string =>
  typeof prompt === 'string' ? prompt.trim().slice(0, MAX_INITIAL_PROMPT_LENGTH) : '';

/** Reads the prompt file once and deletes it, so it never outlives the launch. */
export const promptFromFile = (file: string): string => {
  const quoted = `'${file.replace(/'/g, `'\\''`)}'`;
  return `"$(cat -- ${quoted} && rm -f -- ${quoted})"`;
};

export const withInitialPrompt = (
  providerId: string,
  command: string,
  prompt: unknown,
  promptFile?: string,
): string => {
  const text = initialPromptText(prompt);
  if (!text) return command;
  const arg = promptFile ? promptFromFile(promptFile) : ansiCQuote(text);
  if (providerId === 'claude') return `${command} -- ${arg}`;
  if (providerId === 'codex') return `${command} --prompt ${arg}`;
  return command;
};
