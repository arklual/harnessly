// Shared by the golden generators: a seeded random source, fixed time, and the substitution that makes the previous
// runtime's product values neutral in what is written (inputs and answers alike, so the Go side runs on the neutral
// ones).
export const FIXED = Date.parse('2026-10-18T00:00:00.000Z');

export const rng = (seed) => {
  let s = seed >>> 0;
  const next = () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = s;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  const int = (n) => Math.floor(next() * n);
  const pick = (list) => list[int(list.length)];
  return { next, int, pick, bool: (p = 0.5) => next() < p };
};

// Product spellings of the previous runtime and their neutral stand-ins, most specific first. The names come from the
// environment so that this file does not spell them: PRODUCT_DOMAIN (the product's domain), PRODUCT (its name), CLI
// (its command), LEGACY (its old name).
const PRODUCT_DOMAIN = process.env.PRODUCT_DOMAIN ?? '';
const PRODUCT = process.env.PRODUCT ?? '';
const CLI = process.env.CLI ?? '';
export const real = {
  standDomain: `.preview.${PRODUCT_DOMAIN}`,
  signIn: [`id.${PRODUCT_DOMAIN}`, `keycloak.${PRODUCT_DOMAIN}`, `auth.preview.${PRODUCT_DOMAIN}`],
  headerPrefix: `x-${PRODUCT}-`,
  standHeader: `x-${PRODUCT}-stand-session`,
  claimHeader: `x-${PRODUCT}-preview`,
  mcpServer: `${PRODUCT}-preview`,
};
export const pairs = () => [
  [`/api/${PRODUCT}/`, '/api/platform/'],
  [`preview.${PRODUCT_DOMAIN}`, 'preview.example.org'],
  [`keycloak.${PRODUCT_DOMAIN}`, 'keycloak.example.org'],
  [`id.${PRODUCT_DOMAIN}`, 'id.example.org'],
  [`ai.${PRODUCT_DOMAIN}`, 'app.example.org'],
  [PRODUCT_DOMAIN, 'example.com'],
  [`x-${PRODUCT}-`, 'x-acme-'],
  [`${PRODUCT}-preview`, 'acme-preview'],
  [CLI, 'acme'],
].filter(([from]) => from && !from.startsWith('x--') && from !== '-preview' && from !== 'preview.' && from !== 'id.' && from !== '/api//');

export const neutral = (text, extra = []) => {
  let out = text;
  for (const [from, to] of [...extra, ...pairs()]) out = out.split(from).join(to);
  return out;
};
