// «Разобрать»: the agent beside the review weighs a comment of a merge request's discussion (or the whole discussion)
// and drafts an answer from the person. It is asked in the chat's side chat — a fork of its session that is never kept,
// so the analysis is seen by the person only and nothing reaches GitLab until «Применить ответ». The answer comes as a
// fenced block the screen reads.

export const ANALYSIS_FENCE = 'review-analysis';

export interface IAnalysis {
  /** Each participant's point weighed: one for a comment, one per note for a discussion. */
  points: { who: string; why: string }[];
  /** The conclusion of a discussion; null for one comment. */
  sum: string | null;
  /** Ready answers from the person, the first the agent's choice. */
  replies: string[];
}

export interface IAnalysisNote {
  who: string;
  body: string;
}

export interface IAnalysisWords {
  /** «Разбери комментарий {who} …» with the place: what is asked of the agent. */
  askOne: string;
  askAll: string;
  /** How to answer: the block and its fields. */
  formatOne: string;
  formatAll: string;
}

const MAX = 4000;
const clip = (s: string) => (s.length > MAX ? `${s.slice(0, MAX)}…` : s);

/** The question for the side chat: the discussion, what is weighed, the block to answer in. */
export const analysisQuestion = (
  notes: readonly IAnalysisNote[],
  index: number | null,
  where: string,
  words: IAnalysisWords,
): string => {
  const talk = notes.map((n, i) => `${i + 1}. ${n.who}: ${clip(n.body)}`).join('\n');
  const one = index === null ? null : notes[index];
  const ask = one
    ? words.askOne.replace('{who}', one.who).replace('{n}', String((index ?? 0) + 1))
    : words.askAll;
  const format = one ? words.formatOne : words.formatAll;
  const sample = one
    ? '{"why":"…","replies":["…","…","…"]}'
    : '{"points":[{"who":"…","why":"…"}],"sum":"…","replies":["…","…","…"]}';
  return `${ask}\n${where}\n\n${talk}\n\n${format}\n\`\`\`${ANALYSIS_FENCE}\n${sample}\n\`\`\``;
};

const BLOCK = new RegExp('```' + ANALYSIS_FENCE + '[^\\n]*\\n([\\s\\S]*?)\\n```', 'g');

const strings = (v: unknown): string[] =>
  Array.isArray(v)
    ? v.filter((s): s is string => typeof s === 'string' && s.trim() !== '').map((s) => s.trim())
    : [];

/** The analysis in the agent's answer; null when the answer has none (then the screen says it failed). */
export const parseAnalysis = (text: string, one: IAnalysisNote | null): IAnalysis | null => {
  let last: string | null = null;
  for (const m of text.matchAll(BLOCK)) last = m[1] ?? null;
  if (last === null) return null;
  let raw: unknown;
  try {
    raw = JSON.parse(last);
  } catch {
    return null;
  }
  if (!raw || typeof raw !== 'object') return null;
  const o = raw as Record<string, unknown>;
  const replies = strings(o.replies);
  if (one) {
    const why = typeof o.why === 'string' ? o.why.trim() : '';
    if (!why && replies.length === 0) return null;
    return { points: why ? [{ who: one.who, why }] : [], sum: null, replies };
  }
  const points = (Array.isArray(o.points) ? o.points : []).flatMap((p) => {
    if (!p || typeof p !== 'object') return [];
    const { who, why } = p as Record<string, unknown>;
    return typeof who === 'string' && typeof why === 'string' && why.trim()
      ? [{ who: who.trim(), why: why.trim() }]
      : [];
  });
  const sum = typeof o.sum === 'string' && o.sum.trim() ? o.sum.trim() : null;
  if (points.length === 0 && !sum && replies.length === 0) return null;
  return { points, sum, replies };
};
