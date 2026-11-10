// What a board took from the kit: «Из кита: Обложка», «Открыть в ките» (the kit screen with the camera on
// that node) and, for a layout with a picture, «Показать макет» — the source laid over the board to compare.
import { useState } from 'react';
import type { IKitLink } from '@/next/features/designly/kit/kit-api';
import { usePresence, PRESENCE } from '@/next/ui/presence';
import { t } from '@/next/i18n';

interface IKitOriginProps {
  link: IKitLink;
  node: string;
  /** The node's picture by its path in the version. */
  shotUrl: (path: string) => string;
  onOpen: (node: string) => void;
  /** «Править в ките»: the kit's draft opens on that node with its chat; absent when the kit is not edited. */
  onEdit?: (node: string) => void;
}

const EXIT_MS = 160;

const KitOrigin = ({ link, node, shotUrl, onOpen, onEdit }: IKitOriginProps) => {
  const [shown, setShown] = useState<string | null>(null);
  const info = link.nodes[node];
  const shot = info?.shot && node.startsWith('layout:') ? shotUrl(info.shot) : null;
  const layer = usePresence(shown === node ? shot : null, EXIT_MS);
  if (!info) return null;
  return (
    <>
      {layer.value && (
        <img
          className="hx-kit-origin__layer"
          data-state={layer.state === PRESENCE.closing ? 'closing' : 'open'}
          src={layer.value}
          alt=""
        />
      )}
      <div className="hx-kit-origin">
        <span className="hx-kit-origin__name">{t('next.kit.link.from', { name: info.name })}</span>
        <button type="button" className="hx-kit-origin__btn" onClick={() => onOpen(node)}>
          {t('next.kit.link.openNode')}
        </button>
        {onEdit && (
          <button type="button" className="hx-kit-origin__btn" onClick={() => onEdit(node)}>
            {t('next.versions.editInKit')}
          </button>
        )}
        {shot && (
          <button
            type="button"
            className="hx-kit-origin__btn"
            aria-pressed={shown === node}
            onClick={() => setShown((v) => (v === node ? null : node))}
          >
            {t(shown === node ? 'next.kit.link.hideLayout' : 'next.kit.link.showLayout')}
          </button>
        )}
      </div>
    </>
  );
};

export default KitOrigin;
