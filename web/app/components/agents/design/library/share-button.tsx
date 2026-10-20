import { Check, Share2 } from 'lucide-react';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton, TwTooltip } from '@/components/agents/ui';
import useDesignLink from '@/hooks/use-design-link';
import type { IDesignLocation } from '@/lib/hl-conversation-url';
import { t } from '@/lib/i18n';

interface IShareButtonProps {
  location: IDesignLocation;
  /** The name the share sheet shows. */
  title: string;
  size?: (typeof BUTTON_SIZE)[keyof typeof BUTTON_SIZE];
}

/** "Share": the link of the place goes to the share sheet or the clipboard; the icon says it was copied. */
export const ShareButton = ({ location, title, size = BUTTON_SIZE.sm }: IShareButtonProps) => {
  const { share, shared } = useDesignLink();
  return (
    <TwTooltip content={t('design.shareButton.share')}>
      <TwButton
        size={size}
        variant={BUTTON_VARIANT.ghost}
        icon={shared ? <Check /> : <Share2 />}
        aria-label={t('design.shareButton.share')}
        onClick={() => void share(location, title)}
      />
    </TwTooltip>
  );
};
