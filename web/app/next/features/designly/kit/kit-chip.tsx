// The project's kit on its bar (HRN-897): «Кит · v0.6.0», a click opens the kit screen at that version; a newer
// installed version is said quietly beside it, never as a badge.
import type { IKitLink } from '@/next/features/designly/kit/kit-api';
import { t } from '@/next/i18n';

interface IKitChipProps {
  link: IKitLink;
  onOpen: () => void;
}

const KitChip = ({ link, onOpen }: IKitChipProps) => {
  const version = link.version || link.latest;
  const newer = link.version !== '' && link.latest !== '' && link.latest !== link.version;
  return (
    <button
      type="button"
      className="hx-kit-chip hx-tipped"
      aria-label={`${t('next.kit.link.open')}: ${link.kitTitle}`}
      onClick={onOpen}
    >
      <span className="hx-kit-chip__name">{t('next.kit.link.chip', { version })}</span>
      {newer && <span className="hx-kit-chip__note">{t('next.kit.link.update')}</span>}
      <span className="hx-tip" data-pos="below" data-align="end">
        {link.kitTitle}
      </span>
    </button>
  );
};

export default KitChip;
