// What the voice does only after «да»: deleting, sending outside or to everyone, stopping chats, rolling out.
// A spoken task that sounds like one of them is held and asked about; the next words decide.

/** Deleting, clearing, sending, publishing, stopping, rolling out, merging — in Russian or English. */
const DANGER_RE =
  /(удал|сотри|стер[еи]|очист|снеси|отправ|разошл|опублик|поделись|останов|прерв|заглуши|убей|выкат|выкати|релиз|задепло|деплой|смерж|мерж|запуш|залей в|delete|remove|erase|wipe|send|publish|share|stop|kill|abort|deploy|release|roll ?out|merge|push)/i;
const YES_RE = /^(да|ага|угу|подтверждаю|давай|делай|конечно|yes|yep|yeah|sure|confirm)(\s|[.,!]|$)/i;
const NO_RE = /^(нет|не надо|не нужно|отмена|отбой|no|nope|cancel)(\s|[.,!]|$)/i;

export const ANSWER = { yes: 'yes', no: 'no', other: 'other' } as const;
export type TAnswer = (typeof ANSWER)[keyof typeof ANSWER];

/** The task is one of the actions that wait for «да». */
export const needsYes = (task: string): boolean => DANGER_RE.test(task);

/** What the words said after the question are: «да», «нет», or something else (the held task is dropped). */
export const answerOf = (words: string): TAnswer => {
  const said = words.trim();
  if (YES_RE.test(said)) return ANSWER.yes;
  if (NO_RE.test(said)) return ANSWER.no;
  return ANSWER.other;
};
