// The settings as the pages of Settings name them. The catalog of the machine (sources-api) knows the settings an
// agent may change, under its own names; the person looks for what they read on the page («Enter отправляет»,
// «Вкладки сверху») and for other words for the same («горячие клавиши», «шрифт»). Each line here is a label of a
// page with the section it is in and the words it is also found by (next.search.syn.<id>, a list in the dictionary).
import { ROW_WIDGET, type ISearchWidget } from '@/next/features/search/search-dialog';
import { SETTING_ON } from '@/next/features/search/sources-api';
import { t } from '@/next/i18n';
import type { SettingsSection } from '@/next/routes/routes';
import type { PersonSettings } from '@/next/stores/me';

export interface IPageSetting {
  id: string;
  section: SettingsSection;
  title: string;
  /** Other words it is found by; not shown. */
  words: string;
}

const LABELS: readonly { id: string; section: SettingsSection; label: string }[] = [
  { id: 'language', section: 'general', label: 'next.settings.general.language' },
  { id: 'editor', section: 'general', label: 'next.settings.general.editor' },
  { id: 'enter', section: 'general', label: 'next.settings.general.enter' },
  { id: 'dayStart', section: 'general', label: 'next.settings.general.dayStart' },
  { id: 'idleStop', section: 'general', label: 'next.settings.general.idleStop' },
  { id: 'topTabs', section: 'general', label: 'next.settings.general.topTabs' },
  { id: 'paneAuto', section: 'general', label: 'next.settings.general.paneAuto' },
  { id: 'feed', section: 'general', label: 'next.settings.general.feed' },
  { id: 'limitContinue', section: 'general', label: 'next.settings.general.limitContinue' },
  { id: 'restartContinue', section: 'general', label: 'next.settings.general.restartContinue' },
  { id: 'signOut', section: 'general', label: 'next.settings.general.signOut' },
  { id: 'theme', section: 'appearance', label: 'next.settings.general.theme' },
  { id: 'textSize', section: 'appearance', label: 'next.settings.general.textSize' },
  { id: 'palette', section: 'appearance', label: 'next.settings.look.palette' },
  { id: 'voiceMic', section: 'voice', label: 'next.settings.voice.mic' },
  { id: 'voiceLang', section: 'voice', label: 'next.settings.voice.lang' },
  { id: 'voiceEngine', section: 'voice', label: 'next.settings.voice.engine' },
  { id: 'voiceWords', section: 'voice', label: 'next.settings.voice.words' },
];

/** The sections themselves are found by other words too («горячие клавиши» for the keys). */
export const sectionWords = (section: SettingsSection): string => t(`next.search.syn.section.${section}`);

export const pageSettings = (): readonly IPageSetting[] =>
  LABELS.map((item) => ({
    id: item.id,
    section: item.section,
    title: t(item.label),
    words: t(`next.search.syn.${item.id}`),
  }));

/** The key a person's own setting is changed by from the search: `person:<field>` (the machine's catalog has none). */
export const PERSON_KEY = 'person:';

export const PERSON_FIELD = {
  enterSends: 'enterSends',
  textSize: 'textSize',
} as const;
export type TPersonField = (typeof PERSON_FIELD)[keyof typeof PERSON_FIELD];

const PERSON_OF: Readonly<Record<string, TPersonField>> = {
  enter: PERSON_FIELD.enterSends,
  textSize: PERSON_FIELD.textSize,
};

const TEXT_SIZES = ['s', 'm', 'l'] as const;

/** The control of a page's setting that is the person's own and simple (a switch, a short list); null for the rest. */
export const personWidget = (
  id: string,
  person: PersonSettings | null,
): { key: string; widget: ISearchWidget } | null => {
  const field = PERSON_OF[id];
  if (person === null || field === undefined) return null;
  const key = `${PERSON_KEY}${field}`;
  if (field === PERSON_FIELD.enterSends)
    return { key, widget: { kind: ROW_WIDGET.toggle, on: person.enterSends } };
  const pick = (value: string, values: readonly string[], label: (v: string) => string) => ({
    key,
    widget: {
      kind: ROW_WIDGET.pick,
      value,
      label: label(value),
      options: values.map((v) => ({ value: v, label: label(v) })),
    },
  });
  return pick(person.textSize, TEXT_SIZES, (v) => t(`next.settings.general.textSize.${v}`));
};

/** The patch a person's setting takes from a value of its control; null for a value it does not know. */
export const personPatch = (field: string, value: string): Partial<PersonSettings> | null => {
  if (field === PERSON_FIELD.enterSends) return { enterSends: value === SETTING_ON };
  if (field === PERSON_FIELD.textSize) {
    const size = TEXT_SIZES.find((v) => v === value);
    return size ? { textSize: size } : null;
  }
  return null;
};
