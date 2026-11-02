// A call that draws pictures (Codex's image tool, harnessly's generate_images) as a card in the feed: a soft blurred
// tile for each picture while it is being drawn, the picture coming in from blur to sharp when it is ready, a grid when
// there are several. A press opens the picture in the viewer; the brief is the caption (one line, all of it on a
// press). A picture that failed leaves a quiet line. Bytes come from the machine's file viewer (features/pane/delivered).
import { createContext, memo, useContext, useEffect, useState } from 'react';
import { previewUrl } from '@/next/features/pane/delivered';
import {
  JOB_POLL_MS,
  cardView,
  columnsOf,
  followsJob,
  jobOf,
  listJobs,
  shapeOf,
  shapeOfSize,
  type IImageJob,
  type Shape,
} from '@/next/features/chat/image-jobs';
import type { ToolEntry } from '@/next/features/chat/types';
import type { ConvId, ImageGen, MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import { useChatEditing } from '@/next/features/image-edit/chat-host';
import type { IImageEditHost } from '@/next/features/image-edit/host';
import ImageEditView from '@/next/features/image-edit/image-editor';
import type { IImageViewItem } from '@/next/ui/image-view';

/** Where the chat is: its pictures are read from this machine, its running jobs asked about in this conversation. */
export interface IImageHost {
  machine: MachineId;
  conv: ConvId;
  /** Where a picture's bytes are; absent: the machine's file viewer (the gallery has no machine). */
  srcOf?: (path: string) => string;
  /** Where the pictures are changed from; absent: this machine's `<platform>/images` (a gallery brings its own). */
  edit?: IImageEditHost;
}
export const ImageHost = createContext<IImageHost | null>(null);

type ImageEntry = ToolEntry & { images: ImageGen };

/** The card replaces the plain step unless the call ended with nothing to show. */
export const imageEntryOf = (entry: ToolEntry): ImageEntry | null => {
  const images = entry.images;
  if (!images) return null;
  if (images.state === 'done' && images.done.length === 0 && images.failed === null) return null;
  return { ...entry, images };
};

/** The running job of a call, asked about while the call is drawing; null until the machine lists it. */
const useLiveJob = (host: IImageHost, entry: ImageEntry): IImageJob | null => {
  const [job, setJob] = useState<IImageJob | null>(null);
  const { machine, conv } = host;
  const { images, tool, at } = entry;
  const active = images.state === 'drawing' && followsJob(tool);
  useEffect(() => {
    if (!active) return;
    const abort = new AbortController();
    let timer: number | undefined;
    const tick = () => {
      listJobs(machine, conv, abort.signal)
        .then((jobs) => {
          const mine = jobOf(jobs, images, at);
          setJob(mine);
          if (mine === null || mine.state === 'running') timer = window.setTimeout(tick, JOB_POLL_MS);
        })
        .catch(() => {
          // The machine did not answer this time (a reload, a sleeping link): ask again, the card keeps what it has.
          if (!abort.signal.aborted) timer = window.setTimeout(tick, JOB_POLL_MS);
        });
    };
    tick();
    return () => {
      abort.abort();
      window.clearTimeout(timer);
    };
  }, [active, machine, conv, images, at]);
  return active ? job : null;
};

const srcOfPath = (host: IImageHost, path: string): string =>
  host.srcOf ? host.srcOf(path) : previewUrl(host.machine, path);

const nameOf = (path: string): string => path.slice(path.lastIndexOf('/') + 1);

interface ITileProps {
  src: string | null;
  name: string;
  shape: Shape | null;
  tone: number;
  onOpen: () => void;
}

const TONES = 4;

const Tile = ({ src, name, shape, tone, onOpen }: ITileProps) => {
  const [loaded, setLoaded] = useState(false);
  const [natural, setNatural] = useState<Shape | null>(null);
  const [broken, setBroken] = useState(false);
  const ready = src !== null && loaded;
  const frame = shape ?? natural ?? 'square';
  return (
    <button
      type="button"
      className="hx-img"
      data-shape={frame}
      data-ready={ready}
      data-broken={broken || undefined}
      disabled={src === null}
      aria-label={src === null ? t('next.images.drawing') : t('next.pictures.open', { name })}
      onClick={onOpen}
    >
      <span className="hx-img__ph" data-tone={tone % TONES} aria-hidden="true" />
      {src !== null && !broken && (
        <img
          className="hx-img__pic"
          src={src}
          alt=""
          decoding="async"
          draggable={false}
          onLoad={(event) => {
            setNatural(shapeOfSize(event.currentTarget.naturalWidth, event.currentTarget.naturalHeight));
            setLoaded(true);
          }}
          onError={() => setBroken(true)}
        />
      )}
    </button>
  );
};

const Card = ({ host, entry }: { host: IImageHost; entry: ImageEntry }) => {
  const { images } = entry;
  const live = useLiveJob(host, entry);
  const view = cardView(images, live);
  const [viewing, setViewing] = useState<number | null>(null);
  const [wide, setWide] = useState(false);
  const files = view.tiles.filter((path): path is string => path !== null);
  const shape = shapeOf(images.shape);
  const cols = columnsOf(view.tiles.length);
  const current = viewing === null ? undefined : files[viewing];
  const item: IImageViewItem | null =
    current === undefined ? null : { src: srcOfPath(host, current), name: nameOf(current) };
  const step = (delta: 1 | -1) => {
    if (viewing === null) return;
    setViewing((viewing + delta + files.length) % files.length);
  };
  const editing = useChatEditing(
    host,
    current === undefined ? null : { id: current, src: srcOfPath(host, current) },
  );
  return (
    <div className="hx-imgs" data-cols={cols} data-count={view.tiles.length} data-shape={shape ?? undefined}>
      {view.tiles.length > 0 && (
        <div className="hx-imgs__grid">
          {view.tiles.map((path, i) => (
            <Tile
              key={i}
              src={path === null ? null : srcOfPath(host, path)}
              name={path === null ? '' : nameOf(path)}
              shape={shape}
              tone={i}
              onOpen={() => setViewing(files.indexOf(path ?? ''))}
            />
          ))}
        </div>
      )}
      {view.failed !== null && (
        <p className="hx-imgs__err">{t('next.images.failed', { why: view.failed })}</p>
      )}
      {images.prompt !== '' && (
        <button
          type="button"
          className="hx-imgs__cap"
          data-wide={wide || undefined}
          title={images.prompt}
          aria-expanded={wide}
          onClick={() => setWide((value) => !value)}
        >
          {images.prompt}
        </button>
      )}
      <ImageEditView
        item={item}
        editing={editing}
        onClose={() => setViewing(null)}
        position={viewing === null ? undefined : { at: viewing + 1, of: files.length }}
        onStep={step}
      />
    </div>
  );
};

export const ImageCard = memo(({ entry }: { entry: ImageEntry }) => {
  const host = useContext(ImageHost);
  if (host === null) return null;
  return <Card host={host} entry={entry} />;
});
