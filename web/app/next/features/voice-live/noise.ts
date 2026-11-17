// What the speech recognizer makes of a noise («фоновая активность… и он просто текст в чат отправляет»):
// a room's sound, music or a far-off voice comes back as one of a few well-known phantom lines of the subtitles the
// recognizers learnt on («Продолжение следует…», «Субтитры сделал…», «Спасибо за просмотр»), as a lone filler word,
// or as letters with no word in them. Such a line is not a task: it never goes to a chat. Pure.

const PHANTOMS: readonly RegExp[] = [
  /продолжение следует/i,
  /субтитр/i,
  /редактор(ом)? субтитров/i,
  /спасибо за (просмотр|внимание)/i,
  /подписывайтесь на (канал|наш)/i,
  /ставьте лайк/i,
  /до (новых )?встреч(и)? в (следующем|новом) (видео|выпуске)/i,
  /dimatorzok|amara\.org/i,
  /thank(s| you) for watching/i,
  /please subscribe/i,
  /^\s*\[?(музыка|music|аплодисменты|applause|смех|laughter|шум|noise)\]?\s*$/i,
];

const FILLERS = new Set([
  'а',
  'э',
  'ээ',
  'эм',
  'мм',
  'м',
  'ну',
  'угу',
  'ага',
  'хм',
  'uh',
  'um',
  'hmm',
  'mm',
  'ah',
  'oh',
]);

/** Whether the words heard are a noise's phantom rather than something the person said. */
export const isNoiseText = (text: string): boolean => {
  const s = text.trim();
  if (!s) return true;
  if (PHANTOMS.some((re) => re.test(s))) return true;
  const words = s
    .toLowerCase()
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean);
  if (words.length === 0) return true;
  return words.every((w) => FILLERS.has(w));
};
