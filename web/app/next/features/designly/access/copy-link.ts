// The links of «Настройки ссылок»: which are alive, and copying an address made there.
import type { IInvite } from '@/next/features/designly/access/api';
import { t } from '@/next/i18n';

/** Not revoked, not run out, places left. */
export const isAlive = (invite: IInvite, now: number): boolean =>
  invite.revokedAt === null &&
  new Date(invite.expiresAt).getTime() > now &&
  (invite.maxUses === null || invite.uses < invite.maxUses);

/**
 * Puts the text on the clipboard. Safari keeps the click's permission only for a write started in the click, so where
 * the browser takes a pending item the write starts at once and the text arrives when it is known.
 */
export const copyText = async (text: Promise<string>): Promise<void> => {
  const clip = navigator.clipboard;
  if (!clip) throw new Error(t('next.site.copyFailed'));
  if (typeof ClipboardItem !== 'undefined' && typeof clip.write === 'function') {
    const blob = text.then((value) => new Blob([value], { type: 'text/plain' }));
    await clip.write([new ClipboardItem({ 'text/plain': blob })]);
    return;
  }
  await clip.writeText(await text);
};
