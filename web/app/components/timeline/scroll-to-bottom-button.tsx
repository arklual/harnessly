import { ArrowDown } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { t } from '@/lib/i18n';

export default function ScrollToBottomButton({
  visible,
  onClick,
}: {
  visible: boolean;
  onClick: () => void;
}) {
  return (
    <AnimatePresence>
      {visible && (
        <motion.button
          type="button"
          className="tw-scroll-bottom"
          aria-label={t('timeline.scrollToBottomButton.toLatest')}
          onClick={onClick}
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 8 }}
          transition={{ duration: 0.16 }}
        >
          <ArrowDown size={16} />
        </motion.button>
      )}
    </AnimatePresence>
  );
}
