import { appConfig } from '@/platform/config';
import { t } from '@/lib/i18n';
export type TPushKind = 'done' | 'question' | 'test' | 'doc' | 'design' | 'release';

/** What the service worker and the in-app toast both render; one shape for every channel. */
export interface IPushMessage {
  kind: TPushKind;
  title: string;
  body: string;
  conversationTitle: string;
  detail: string;
  provider: string;
  tabId: string;
  /** Machine of the tab when the sender knows it; the click then opens the conversation by its exact address. */
  machineId?: string;
  workspaceId: string;
  agentSessionId: string | null;
  tag: string;
  at: number;
  /** Where a click leads when it is not a conversation tab (a document thread, a board sent to the phone). */
  url?: string;
}

export const PUSH_KIND_LABELS: Record<TPushKind, string> = {
  done: t('lib.pushMessage.kindDone'),
  question: t('lib.pushMessage.kindQuestion'),
  test: t('lib.pushMessage.kindTest'),
  doc: t('lib.pushMessage.kindDoc'),
  design: t('lib.pushMessage.kindDesign'),
  release: t('lib.pushMessage.kindRelease'),
};

export const clip = (text: string, max: number): string => {
  const flat = text.replace(/\s+/g, ' ').trim();
  return flat.length <= max ? flat : `${flat.slice(0, max - 1).trimEnd()}…`;
};

export const buildTestPushMessage = (): IPushMessage => ({
  kind: 'test',
  title: t('lib.pushMessage.testTitle'),
  body: t('lib.pushMessage.testBody'),
  conversationTitle: appConfig().productName,
  detail: t('lib.pushMessage.testBody'),
  provider: 'claude',
  tabId: '',
  workspaceId: '',
  agentSessionId: null,
  tag: 'hl:test',
  at: Date.now(),
});

export interface IDesignPushInput {
  /** Name of the board: the push reads «Check the mobile version: <name>». */
  title: string;
  /** Where a tap leads (`designLinkPath`). */
  url: string;
  /** One notification per board: a second send of the same board replaces the first. */
  tag: string;
}

const DESIGN_TITLE_MAX = 90;

/** A board of the design library sent to the phone: a plain push with a target `url`, like a doc thread's. */
export const buildDesignPushMessage = ({ title, url, tag }: IDesignPushInput): IPushMessage => {
  const body = t('lib.pushMessage.designBody', { title: clip(title, DESIGN_TITLE_MAX) });
  return {
    kind: 'design',
    title: 'Claude Design',
    body,
    conversationTitle: 'Claude Design',
    detail: body,
    provider: 'claude',
    tabId: '',
    workspaceId: '',
    agentSessionId: null,
    tag: `hl:${tag}`,
    at: Date.now(),
    url,
  };
};

/** A new release of the pages reached the gateway: a tap reloads the app onto it. */
export const buildReleasePushMessage = (version: string | null): IPushMessage => {
  const body = version
    ? t('lib.pushMessage.releaseBodyVersion', { version })
    : t('lib.pushMessage.releaseBody');
  return {
    kind: 'release',
    title: t('lib.pushMessage.releaseTitle', { product: appConfig().productName }),
    body,
    conversationTitle: appConfig().productName,
    detail: body,
    provider: 'claude',
    tabId: '',
    workspaceId: '',
    agentSessionId: null,
    tag: 'hl:release',
    at: Date.now(),
  };
};
