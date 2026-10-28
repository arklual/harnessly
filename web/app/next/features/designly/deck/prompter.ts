// The prompter that follows the voice: it aligns what the recognition has heard so far against the slide's script and
// moves a cursor over the script's words. It looks only at the last 5–8 heard words, only inside a window of 40 words
// ahead of the cursor, compares numbers by value («двадцать пять» = 25), tolerates endings and improvisation, and never
// goes back. Pure: no React, no clock.

/** How many of the last heard words are compared (5–8 once that many have been heard). */
export const TAIL_MAX = 8;
/** How far ahead of the cursor a match may be found, in script words. */
export const WINDOW = 40;
/** A line longer than this many words is split at a comma or in the middle. */
export const LINE_WORDS = 14;

const WORD = /[\p{L}\p{N}]+/gu;
/** Script markers such as [пауза] and [клик] are for the person, not for the voice. */
const MARKER = /\[[^\]]*\]/g;

const ONES: Record<string, number> = {
  ноль: 0,
  один: 1,
  одна: 1,
  одну: 1,
  одно: 1,
  два: 2,
  две: 2,
  три: 3,
  четыре: 4,
  пять: 5,
  шесть: 6,
  семь: 7,
  восемь: 8,
  девять: 9,
  десять: 10,
  одиннадцать: 11,
  двенадцать: 12,
  тринадцать: 13,
  четырнадцать: 14,
  пятнадцать: 15,
  шестнадцать: 16,
  семнадцать: 17,
  восемнадцать: 18,
  девятнадцать: 19,
  zero: 0,
  one: 1,
  two: 2,
  three: 3,
  four: 4,
  five: 5,
  six: 6,
  seven: 7,
  eight: 8,
  nine: 9,
  ten: 10,
  eleven: 11,
  twelve: 12,
  thirteen: 13,
  fourteen: 14,
  fifteen: 15,
  sixteen: 16,
  seventeen: 17,
  eighteen: 18,
  nineteen: 19,
};
const TENS: Record<string, number> = {
  двадцать: 20,
  тридцать: 30,
  сорок: 40,
  пятьдесят: 50,
  шестьдесят: 60,
  семьдесят: 70,
  восемьдесят: 80,
  девяносто: 90,
  twenty: 20,
  thirty: 30,
  forty: 40,
  fifty: 50,
  sixty: 60,
  seventy: 70,
  eighty: 80,
  ninety: 90,
};
const HUNDREDS: Record<string, number> = {
  сто: 100,
  двести: 200,
  триста: 300,
  четыреста: 400,
  пятьсот: 500,
  шестьсот: 600,
  семьсот: 700,
  восемьсот: 800,
  девятьсот: 900,
};
const HUNDRED_WORD = new Set(['hundred']);
const THOUSAND_WORD = new Set(['тысяча', 'тысячи', 'тысяч', 'thousand']);

/** A normalised token and the index of the last raw word it came from. */
export interface IToken {
  t: string;
  to: number;
}

const lower = (s: string) => s.toLowerCase().replaceAll('ё', 'е');

// the spoken forms are stored with «е» too, so the lookup is by the folded word
const fold = (table: Record<string, number>): Map<string, number> =>
  new Map(Object.entries(table).map(([k, v]) => [lower(k), v]));
const onesF = fold(ONES);
const tensF = fold(TENS);
const hundredsF = fold(HUNDREDS);

/** Can the number word of value `v` follow the number `cur` that is being spoken? */
const continues = (cur: number, v: number): boolean => {
  if (cur === 0) return true;
  const rest = cur % 100;
  if (cur >= 100 && rest === 0) return v < 100;
  if (rest >= 20 && rest % 10 === 0) return v < 10 && v > 0;
  return false;
};

/** Raw words → lower-case tokens; number words are folded into one digit token. */
export const normalise = (raw: readonly string[]): IToken[] => {
  const parts: IToken[] = [];
  raw.forEach((word, to) => {
    for (const m of lower(word.replace(MARKER, ' ')).matchAll(WORD)) parts.push({ t: m[0], to });
  });
  const out: IToken[] = [];
  let total = 0;
  let cur = 0;
  let active = false;
  let to = 0;
  const flush = () => {
    if (active) out.push({ t: String(total + cur), to });
    total = 0;
    cur = 0;
    active = false;
  };
  for (const part of parts) {
    const one = onesF.get(part.t) ?? tensF.get(part.t) ?? hundredsF.get(part.t);
    if (one !== undefined) {
      if (active && !continues(cur, one)) flush();
      cur += one;
      active = true;
      to = part.to;
    } else if (active && HUNDRED_WORD.has(part.t) && cur > 0 && cur < 10) {
      cur *= 100;
      to = part.to;
    } else if (active && THOUSAND_WORD.has(part.t)) {
      total += (cur || 1) * 1000;
      cur = 0;
      to = part.to;
    } else if (!active && THOUSAND_WORD.has(part.t) && /^\d+$/.test(out[out.length - 1]?.t ?? '')) {
      // «10 тысяч»: the digits before it take the thousand
      const prev = out.pop();
      if (prev) out.push({ t: String(Number(prev.t) * 1000), to: part.to });
    } else {
      flush();
      out.push(part);
    }
  }
  flush();
  return out;
};

/** The same word, allowing for an ending («сервис» ≈ «сервисы», «агента» ≈ «агент»); numbers must be equal. */
export const sameWord = (a: string, b: string): boolean => {
  if (a === b) return true;
  if (/\d/.test(a) || /\d/.test(b)) return false;
  const min = Math.min(a.length, b.length);
  if (min < 4) return false;
  let p = 0;
  while (p < min && a[p] === b[p]) p += 1;
  return p >= Math.max(4, min - 2);
};

/** Short words match anything; they count for little and never anchor a match. */
const weight = (t: string) => (t.length >= 3 || /\d/.test(t) ? 1 : 0.3);

export interface IPromptLine {
  /** Index of the first word of the line in the script's words. */
  from: number;
  /** Index after the last. */
  to: number;
  text: string;
}

export interface IPrompter {
  /** The script's words as shown. */
  words: string[];
  tokens: IToken[];
  lines: IPromptLine[];
  /** How many words of the script have been said. */
  cursor: number;
}

/** The script's lines: sentences, long ones split at a comma or in the middle. */
export const splitLines = (words: readonly string[]): IPromptLine[] => {
  const lines: IPromptLine[] = [];
  let from = 0;
  const push = (to: number) => {
    if (to > from) lines.push({ from, to, text: words.slice(from, to).join(' ') });
    from = to;
  };
  for (let i = 0; i < words.length; i += 1) {
    const w = words[i] ?? '';
    const len = i + 1 - from;
    if (/[.!?…]["»”)]*$/.test(w)) push(i + 1);
    else if (len >= LINE_WORDS && /[,;:—–]$/.test(w)) push(i + 1);
    else if (len >= LINE_WORDS * 1.5) push(i + 1);
  }
  push(words.length);
  return lines;
};

export const createPrompter = (script: string): IPrompter => {
  const words = script.replace(MARKER, ' ').split(/\s+/).filter(Boolean);
  return { words, tokens: normalise(words), lines: splitLines(words), cursor: 0 };
};

/** The index in `tokens` of the first token that belongs to a word not yet said. */
const tokenCursor = (p: IPrompter) => {
  const i = p.tokens.findIndex((tok) => tok.to >= p.cursor);
  return i < 0 ? p.tokens.length : i;
};

/**
 * Moves the cursor to where the speaker is. `heard` is everything recognised so far in the current stretch of speech.
 * Nothing moves when the last words match nothing ahead (improvisation), and nothing ever moves back.
 */
export const advance = (p: IPrompter, heard: string): IPrompter => {
  const tail = normalise(heard.split(/\s+/).filter(Boolean)).slice(-TAIL_MAX);
  if (tail.length < 2 || p.tokens.length === 0) return p;
  const n = tail.length;
  const total = tail.reduce((sum, tok) => sum + weight(tok.t), 0);
  const need = Math.max(2, 0.6 * total);
  const from = tokenCursor(p);
  const stop = Math.min(p.tokens.length, from + WINDOW);
  let best = { score: 0, end: -1 };
  // the tail may begin with words already said, so the search starts a tail's length before the cursor
  for (let s = Math.max(0, from - n); s < stop; s += 1) {
    const span = Math.min(p.tokens.length, s + n + 4);
    // weighted LCS of the tail and tokens[s..span); end = the last script token matched
    const rows = n + 1;
    const cols = span - s + 1;
    const dp: number[][] = Array.from({ length: rows }, () => new Array<number>(cols).fill(0));
    for (let i = 1; i < rows; i += 1) {
      for (let j = 1; j < cols; j += 1) {
        const a = tail[i - 1]?.t ?? '';
        const b = p.tokens[s + j - 1]?.t ?? '';
        const take = sameWord(a, b) ? (dp[i - 1]?.[j - 1] ?? 0) + weight(a) : 0;
        dp[i]![j] = Math.max(take, dp[i - 1]?.[j] ?? 0, dp[i]?.[j - 1] ?? 0);
      }
    }
    // the end is the earliest column where the full score is reached, anchored on a word that counts
    const score = dp[n]?.[cols - 1] ?? 0;
    if (score < need || score <= best.score) continue;
    let end = -1;
    for (let j = 1; j < cols; j += 1) {
      if ((dp[n]?.[j] ?? 0) >= score) {
        end = s + j - 1;
        break;
      }
    }
    while (end > s && weight(p.tokens[end]?.t ?? '') < 1) end -= 1;
    // a match that ends before the cursor is a repeated phrase of what is already said: it never moves the cursor
    if (end < from) continue;
    best = { score, end };
  }
  if (best.end < 0) return p;
  const cursor = (p.tokens[best.end]?.to ?? -1) + 1;
  return cursor > p.cursor ? { ...p, cursor } : p;
};

export interface IPromptView {
  /** The line before the current one. */
  was: string;
  /** The words of the current line already said, with a trailing space when the rest follows. */
  said: string;
  rest: string;
  next: string;
  /** Index of the current line, -1 when the script is empty. */
  line: number;
}

export const view = (p: IPrompter): IPromptView => {
  if (p.lines.length === 0) return { was: '', said: '', rest: '', next: '', line: -1 };
  // a cursor on the border of two lines belongs to the next one; at the very end it stays on the last
  let line = p.lines.findIndex((l) => p.cursor < l.to);
  if (line < 0) line = p.lines.length - 1;
  const cur = p.lines[line]!;
  const split = Math.min(Math.max(p.cursor, cur.from), cur.to);
  const said = p.words.slice(cur.from, split).join(' ');
  const rest = p.words.slice(split, cur.to).join(' ');
  return {
    was: p.lines[line - 1]?.text ?? '',
    said: said && rest ? `${said} ` : said,
    rest,
    next: p.lines[line + 1]?.text ?? '',
    line,
  };
};

/** The whole script has been said (the last word reached). */
export const finished = (p: IPrompter): boolean => p.cursor > 0 && p.cursor >= p.words.length - 1;
