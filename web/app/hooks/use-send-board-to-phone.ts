import { useCallback, useState } from 'react';
import { toast } from 'sonner';
import { currentMachineId } from '@/lib/app-path';
import { sendBoardToPhone } from '@/lib/hl-design-library-client';
import { sentLabel } from '@/lib/hl-design-library/phone';
import { t } from '@/lib/i18n';

export interface IBoardToSend {
  artifact: string;
  /** Under `project/`, with its query, as the manifest records it. */
  board: string;
  title?: string;
}

/** "Send to phone": one request at a time, answered with a short toast either way. */
const useSendBoardToPhone = () => {
  const [sending, setSending] = useState(false);
  const send = useCallback(async (board: IBoardToSend) => {
    setSending(true);
    try {
      const { sent } = await sendBoardToPhone(currentMachineId(), board);
      toast.success(sentLabel(sent));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('hooks.useSendBoardToPhone.failed'));
    } finally {
      setSending(false);
    }
  }, []);
  return { send, sending };
};

export default useSendBoardToPhone;
