// What a screen's agent is told of harnessly: where everything is, each place with its address, so its answer
// can say «это в Настройки → Подключения» and the island turns the link into a jump there.
import { t } from '@/next/i18n';
import { SETTINGS_SECTIONS } from '@/next/routes/routes';

/** The window's own places beside Settings, with their addresses. */
const PAGES = [
  ['next.keys.cmd.global.newChat', '/'],
  ['next.apps.nav', '/tasks'],
  ['next.reviewApp.title', '/review'],
  ['next.sidebar.apps', '/apps'],
  ['next.sidebar.manage', '/manage'],
  ['next.sidebar.stats', '/stats'],
  ['next.sidebar.efficiency', '/efficiency'],
  ['next.learn.title', '/learn'],
  ['next.settings.help.title', '/help'],
] as const;

/** The map as lines the agent reads: a place, its address. Sinopia Design is named as the product calls it. */
export const harnesslyMap = (): string => {
  const settings = t('next.settings.title');
  const lines = [
    ...SETTINGS_SECTIONS.map((id) => `- ${settings} → ${t(`next.settings.section.${id}`)}: /settings/${id}`),
    '- Sinopia Design: /designly',
    ...PAGES.map(([key, path]) => `- ${t(key)}: ${path}`),
  ];
  return `${t('next.settings.agentIsle.mapIntro')}\n${lines.join('\n')}`;
};

/** A piece of the agent's answer: words, or a link to a place of the window. */
export type TAnswerPart = { k: 'text'; text: string } | { k: 'link'; text: string; path: string };

const LINK = /\[([^\]\n]{1,80})\]\((\/[^\s)]{0,120})\)/g;

/** The answer with its markdown links to the window's own places (only addresses that start with «/»). */
export const answerParts = (text: string): TAnswerPart[] => {
  const parts: TAnswerPart[] = [];
  let at = 0;
  for (const m of text.matchAll(LINK)) {
    if (m.index > at) parts.push({ k: 'text', text: text.slice(at, m.index) });
    parts.push({ k: 'link', text: m[1], path: m[2] });
    at = m.index + m[0].length;
  }
  if (at < text.length) parts.push({ k: 'text', text: text.slice(at) });
  return parts;
};
