// The voice's own words heard back by the microphone (15.11: «Так, чё думаешь», «Оба очень важны» came in the person's
// line). The browser's echo canceller misses some of it on speakers; what the voice said is known, so its runs of words
// are cut out of the person's words before they reach the chat.

/** A run of the voice's words this long, found in the person's line, is its echo; one word («да») may be theirs. */
export const ECHO_RUN = 2;

const wordsOf = (text: string): string[] =>
  text
    .toLowerCase()
    .replace(/ё/g, 'е')
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean);

/** The person's words without the runs of ECHO_RUN and more words the voice said (`voice`: its recent lines). */
export const dropEcho = (heard: string, voice: readonly string[]): string => {
  const said = voice.map(wordsOf).filter((w) => w.length >= ECHO_RUN);
  if (said.length === 0) return heard.trim();
  const raw = heard.trim().split(/\s+/).filter(Boolean);
  const norm = raw.map((w) => wordsOf(w).join(''));
  const cut = new Array<boolean>(raw.length).fill(false);
  for (const v of said) {
    for (let i = 0; i < norm.length; i++) {
      for (let j = 0; j < v.length; j++) {
        let n = 0;
        while (i + n < norm.length && j + n < v.length && norm[i + n] !== '' && norm[i + n] === v[j + n]) n++;
        if (n >= ECHO_RUN) for (let k = i; k < i + n; k++) cut[k] = true;
      }
    }
  }
  return raw.filter((_, i) => !cut[i]).join(' ');
};
