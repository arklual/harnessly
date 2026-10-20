import type { IAskUserQuestionItem, IAskUserQuestionOption } from '@/types/timeline';

/** What the person chose for one question: option indexes and the free text of the "Other" row ('' when not used). */
export interface IQuestionChoice {
  picked: number[];
  other: string;
}

/** The part of a question the keys depend on. */
export interface IQuestionShape {
  optionCount: number;
  multiSelect: boolean;
  preview: boolean;
}

export type TKeyStep = { kind: 'key'; key: string } | { kind: 'text'; text: string };

export interface IKeyIo {
  key: (key: string) => Promise<void>;
  text: (text: string) => Promise<void>;
  sleep: (ms: number) => Promise<void>;
}

export const MAX_ANSWER_OPTIONS = 8;
export const KEY_STEP_DELAY_MS = 150;
export const KEY_ADVANCE_DELAY_MS = 350;
export const ANSWER_CONFIRM_POLL_MS = 250;
export const ANSWER_CONFIRM_TIMEOUT_MS = 3000;

export const EMPTY_CHOICE: IQuestionChoice = { picked: [], other: '' };

export const questionShape = (question: IAskUserQuestionItem): IQuestionShape => ({
  optionCount: question.options.length,
  multiSelect: question.multiSelect,
  preview: question.preview,
});

/**
 * Claude Code draws a single-choice question whose options carry previews side by side with the preview: a digit there
 * only moves the cursor, Enter picks, and there is no "Other" row (notes take its place).
 */
const isPreviewLayout = (shape: IQuestionShape): boolean => shape.preview && !shape.multiSelect;

export const takesFreeText = (shape: IQuestionShape): boolean => !isPreviewLayout(shape);

/** One question that takes a single option answers on the key press itself; every other call ends with a review screen. */
export const isInstantAnswer = (questions: readonly IQuestionShape[]): boolean =>
  questions.length === 1 && !questions[0].multiSelect;

export const isChoiceComplete = (choice: IQuestionChoice, otherOn: boolean): boolean => {
  if (otherOn) return choice.other.trim().length > 0;
  return choice.picked.length > 0;
};

const digitKey = (index: number): TKeyStep => ({ kind: 'key', key: String(index + 1) });
const key = (name: string): TKeyStep => ({ kind: 'key', key: name });

const questionSteps = (shape: IQuestionShape, choice: IQuestionChoice): TKeyStep[] => {
  const other = choice.other.trim();
  const picked = [...choice.picked].sort((a, b) => a - b);

  if (isPreviewLayout(shape)) return [digitKey(picked[0]), key('Enter')];
  if (!shape.multiSelect) {
    if (other) return [digitKey(shape.optionCount), { kind: 'text', text: other }, key('Enter')];
    return [digitKey(picked[0])];
  }

  // A digit toggles an option and leaves the cursor on the first row; "Type something" takes text only under the cursor.
  const steps = picked.map(digitKey);
  if (!other) return [...steps, key('Right')];
  const toOther = Array.from({ length: shape.optionCount }, () => key('Down'));
  return [...steps, ...toOther, { kind: 'text', text: other }, key('Down'), key('Enter')];
};

/** Claude Code AskUserQuestion TUI: one tab per question, a review screen at the end ("1" is "Submit answers"). */
export const buildAnswerSteps = (
  shapes: readonly IQuestionShape[],
  choices: readonly IQuestionChoice[],
): TKeyStep[][] => {
  const perQuestion = shapes.map((shape, i) => questionSteps(shape, choices[i]));
  if (isInstantAnswer(shapes)) return perQuestion;
  return [...perQuestion, [key('1')]];
};

export const validateAnswerRequest = (
  shapes: readonly IQuestionShape[],
  choices: readonly IQuestionChoice[],
): string | null => {
  if (shapes.length === 0 || shapes.length !== choices.length)
    return 'questions and choices must be the same non-empty length';
  for (let i = 0; i < shapes.length; i++) {
    const { optionCount, multiSelect } = shapes[i];
    const { picked, other } = choices[i];
    if (!takesFreeText(shapes[i]) && other.trim()) return `question ${i + 1}: no free text next to previews`;
    if (!Number.isInteger(optionCount) || optionCount < 1 || optionCount > MAX_ANSWER_OPTIONS)
      return `question ${i + 1}: bad option count`;
    if (picked.some((p) => !Number.isInteger(p) || p < 0 || p >= optionCount))
      return `question ${i + 1}: option out of range`;
    if (new Set(picked).size !== picked.length) return `question ${i + 1}: repeated option`;
    if (!multiSelect && picked.length + (other.trim() ? 1 : 0) !== 1)
      return `question ${i + 1}: exactly one answer expected`;
    if (multiSelect && picked.length === 0 && !other.trim()) return `question ${i + 1}: no answer`;
    if (!multiSelect && other.trim() && picked.length > 0)
      return `question ${i + 1}: option and free text together`;
  }
  return null;
};

export const playAnswerSteps = async (groups: readonly TKeyStep[][], io: IKeyIo): Promise<void> => {
  for (const group of groups) {
    for (const step of group) {
      if (step.kind === 'key') await io.key(step.key);
      else await io.text(step.text);
      await io.sleep(KEY_STEP_DELAY_MS);
    }
    await io.sleep(KEY_ADVANCE_DELAY_MS);
  }
};

const QUESTION_FOOTER = 'Enter to select';
const REVIEW_PROMPT = 'Ready to submit your answers?';
const SCREEN_TAIL_LINES = 5;

/**
 * Whether the bottom of a Claude Code screen is the AskUserQuestion dialog: one of its question tabs (the footer opens with
 * "Enter to select" and may wrap) or the review screen. Only the tail counts: the conversation above may quote the words.
 */
export const isQuestionOnScreen = (screen: string): boolean => {
  const tail = screen
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .slice(-SCREEN_TAIL_LINES);
  return tail.some((line) => line.startsWith(QUESTION_FOOTER) || line === REVIEW_PROMPT);
};

/** Keys are typed blind: the answer counts as taken only once the dialog has left the screen. */
export const waitForQuestionGone = async (
  readScreen: () => Promise<string | null>,
  sleep: (ms: number) => Promise<void>,
): Promise<boolean> => {
  for (let waited = 0; waited < ANSWER_CONFIRM_TIMEOUT_MS; waited += ANSWER_CONFIRM_POLL_MS) {
    await sleep(ANSWER_CONFIRM_POLL_MS);
    const screen = await readScreen();
    if (screen !== null && !isQuestionOnScreen(screen)) return true;
  }
  return false;
};

const ANSWER_SEPARATOR = ', ';

/**
 * Reads back the answer Claude Code records for a question: the labels of the picked options joined by ", " (in click order)
 * or the free text of "Other". Labels may themselves hold ", ", so the text is consumed by label, not split.
 */
export const parseAnswer = (answer: string, options: readonly IAskUserQuestionOption[]): IQuestionChoice => {
  const picked: number[] = [];
  let rest = answer;
  let matched = true;
  while (rest && matched) {
    const index = options.findIndex(
      (option, i) =>
        !picked.includes(i) && (rest === option.label || rest.startsWith(option.label + ANSWER_SEPARATOR)),
    );
    matched = index !== -1;
    if (matched) {
      picked.push(index);
      rest = rest.slice(options[index].label.length + ANSWER_SEPARATOR.length);
    }
  }
  return { picked, other: rest };
};
