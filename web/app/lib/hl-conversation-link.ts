import { toast } from 'sonner';
import { copyToClipboard } from '@/lib/clipboard';
import { conversationUrl } from '@/lib/hl-conversation-url';
import { t } from '@/lib/i18n';

export const copyConversationLink = async (key: string) => {
  const copied = await copyToClipboard(conversationUrl(window.location.origin, key));
  if (copied) toast.success(t('lib.hlConversationLink.copied'));
  else toast.error(t('lib.hlConversationLink.copyFailed'));
};
