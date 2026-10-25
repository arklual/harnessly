// An attachment's square in a message or the queue: the picture itself when the page can reach it, else its icon.
// Where pictures come from is the screen's business (AttachmentSrc); a picture that does not load shows a «Повторить»
// on its square, and a press asks the picture again. A picture opens full size on a click.
import { createContext, useContext, useState } from 'react';
import Icon from '@/next/ui/icon';
import ImageView from '@/next/ui/image-view';
import { t } from '@/next/i18n';

export interface IThumbAttachment {
  id: string;
  kind: string;
  name: string;
}

/** The attachments shown together (one message's): the viewer steps through their pictures. */
export const ThumbSet = createContext<IThumbAttachment[] | null>(null);

/** Where an attachment's picture is, null when there is none to show. */
export const AttachmentSrc = createContext<(item: IThumbAttachment) => string | null>(() => null);

interface IAttachmentThumbProps {
  item: IThumbAttachment;
  className: string;
}

interface IViewerProps {
  item: IThumbAttachment | null;
  set: IThumbAttachment[] | null;
  srcOf: (item: IThumbAttachment) => string | null;
  onShow: (item: IThumbAttachment | null) => void;
}

/** The open picture and its neighbours among the set's pictures. */
const Viewer = ({ item, set, srcOf, onShow }: IViewerProps) => {
  const pictures = (set ?? []).filter((one) => one.kind === 'image' && srcOf(one) !== null);
  const at = item ? pictures.findIndex((one) => one.id === item.id) : -1;
  const src = item ? srcOf(item) : null;
  const step = (delta: 1 | -1) => {
    if (at < 0 || pictures.length < 2) return;
    onShow(pictures[(at + delta + pictures.length) % pictures.length]);
  };
  return (
    <ImageView
      item={item && src ? { src, name: item.name } : null}
      onClose={() => onShow(null)}
      position={at >= 0 ? { at: at + 1, of: pictures.length } : undefined}
      onStep={step}
    />
  );
};

const AttachmentThumb = ({ item, className }: IAttachmentThumbProps) => {
  const srcOf = useContext(AttachmentSrc);
  const [broken, setBroken] = useState(false);
  const [round, setRound] = useState(0);
  const set = useContext(ThumbSet);
  const [viewing, setViewing] = useState<IThumbAttachment | null>(null);
  const picture = item.kind === 'image' ? srcOf(item) : null;
  if (picture && broken)
    return (
      <button
        type="button"
        className={className}
        data-broken="true"
        aria-label={`${item.name}. ${t('next.load.picture')}`}
        title={t('next.load.picture')}
        onClick={(event) => {
          event.stopPropagation();
          setBroken(false);
          setRound((n) => n + 1);
        }}
      >
        <Icon name="rotate-ccw" size="sm" />
      </button>
    );
  const src = picture;
  if (!src)
    return (
      <span className={className}>
        <Icon name={item.kind === 'image' ? 'image' : 'file-text'} size="sm" />
      </span>
    );
  return (
    <>
      <button
        type="button"
        className={className}
        data-pic="true"
        aria-label={item.name}
        onClick={(event) => {
          event.stopPropagation();
          setViewing(item);
        }}
      >
        <img key={round} src={src} alt="" loading="lazy" decoding="async" onError={() => setBroken(true)} />
      </button>
      <Viewer item={viewing} set={set} srcOf={srcOf} onShow={setViewing} />
    </>
  );
};

export default AttachmentThumb;
