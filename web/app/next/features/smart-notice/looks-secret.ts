// The page's side of the slot's rule (internal/noticeslot/secret.go says the same): a text that is, or holds, a key, a
// token or a private key never leaves the notice. The page stops it before sending and points to the safe; the machine
// refuses it again either way.

const KEY_PREFIXES = [
  'sk-',
  'sk_live_',
  'sk_test_',
  'rk_live_',
  'pk_live_',
  'ghp_',
  'gho_',
  'ghu_',
  'ghs_',
  'ghr_',
  'github_pat_',
  'glpat-',
  'gldt-',
  'xoxb-',
  'xoxp-',
  'xoxa-',
  'xoxs-',
  'akia',
  'asia',
  'aiza',
  'ya29.',
  'npm_',
  'pypi-',
  '-----begin',
] as const;

const JWT = /^eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}$/;
const BOT_TOKEN = /^[0-9]{8,10}:[A-Za-z0-9_-]{35}$/;
const HEX_ONLY = /^[0-9a-fA-F-]+$/;
const WORD_CHARS = /^[A-Za-z0-9_+=/-]+$/;

/** Words and numbers joined by _ - . (a table, a branch, a file): no piece mixes letters and digits as a key does. */
const wordsOnly = (w: string): boolean =>
  w.split(/[_.-]/).every((piece) => !(/[0-9]/.test(piece) && /[A-Za-z]/.test(piece)));

/** One long random word: 24+ letters and digits mixed, no spaces, high entropy; a hash or a UUID is not one. */
const randomWord = (w: string): boolean => {
  if (w.length < 24 || w.includes('://') || (w.match(/\//g)?.length ?? 0) > 1) return false;
  if (!WORD_CHARS.test(w) || HEX_ONLY.test(w) || wordsOnly(w)) return false;
  if (!/[0-9]/.test(w) || !/[A-Za-z]/.test(w)) return false;
  const counts = new Map<string, number>();
  for (const ch of w) counts.set(ch, (counts.get(ch) ?? 0) + 1);
  let bits = 0;
  for (const c of counts.values()) {
    const p = c / w.length;
    bits -= p * Math.log2(p);
  }
  return bits >= 3.6;
};

export const looksSecret = (text: string): boolean => {
  for (const word of text.split(/\s+/)) {
    const w = word.replace(/^["'.,;()[\]{}<>]+|["'.,;()[\]{}<>]+$/g, '');
    const low = w.toLowerCase();
    if (KEY_PREFIXES.some((p) => low.startsWith(p) && w.length >= p.length + 8)) return true;
    if (JWT.test(w) || BOT_TOKEN.test(w) || randomWord(w)) return true;
  }
  return text.toLowerCase().includes('-----begin');
};
