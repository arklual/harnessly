import { useEffect, useState } from 'react';
import dynamic from 'next/dynamic';
import { Download, ExternalLink, FileText, Paperclip } from 'lucide-react';
import { appFetch } from '@/lib/app-path';
import { fileDownloadUrl, filePreviewUrl } from '@/lib/file-preview-url';
import { fileKindOf, type IViewableFile } from '@/lib/hl-artifacts';
import type { ISentFiles, ITimelineToolCall } from '@/types/timeline';
import { t } from '@/lib/i18n';

const FileViewer = dynamic(() => import('@/components/agents/inspector/file-viewer'), { ssr: false });

interface IFileInfo {
  path: string;
  size?: number;
  preview: boolean;
  download: boolean;
  error?: string;
}

type TInfoState =
  { status: 'loading' } | { status: 'ready'; files: Map<string, IFileInfo> } | { status: 'unreachable' };

const UNREACHABLE = t('timeline.sentFilesItem.unreachable');

const formatSize = (bytes: number) => {
  if (bytes < 1024) return t('timeline.sentFilesItem.sizeBytes', { size: bytes });
  if (bytes < 1024 * 1024) return t('timeline.sentFilesItem.sizeKb', { size: Math.round(bytes / 1024) });
  return t('timeline.sentFilesItem.sizeMb', {
    size: (bytes / 1024 / 1024).toFixed(bytes < 10 * 1024 * 1024 ? 1 : 0),
  });
};

const useFileInfo = (paths: string[]): TInfoState => {
  const key = paths.join('\n');
  const [state, setState] = useState<{ key: string; value: TInfoState }>({
    key,
    value: { status: 'loading' },
  });
  useEffect(() => {
    let alive = true;
    appFetch('/api/platform/file-preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths: key.split('\n'), details: true }),
    })
      .then(async (res) => {
        if (!res.ok) throw new Error(String(res.status));
        const body = (await res.json()) as { files?: IFileInfo[] };
        return { status: 'ready' as const, files: new Map((body.files ?? []).map((f) => [f.path, f])) };
      })
      .catch(() => ({ status: 'unreachable' as const }))
      .then((value) => {
        if (alive) setState({ key, value });
      });
    return () => {
      alive = false;
    };
  }, [key]);
  return state.key === key ? state.value : { status: 'loading' };
};

const nameOf = (path: string) => path.split('/').at(-1) ?? path;

interface ISentFileProps {
  path: string;
  info: IFileInfo | undefined;
  unreachable: boolean;
  inline: boolean;
  onOpen: (file: IViewableFile) => void;
  timestamp: number;
}

const SentFile = ({ path, info, unreachable, inline, onOpen, timestamp }: ISentFileProps) => {
  const [broken, setBroken] = useState(false);
  const kind = fileKindOf(path);
  const name = nameOf(path);
  const missing =
    unreachable || broken
      ? UNREACHABLE
      : info && !info.download
        ? (info.error ?? t('timeline.sentFilesItem.notFound'))
        : null;
  const viewable = !missing && info?.preview && kind !== 'text';
  const open = () => onOpen({ path, kind, origin: 'created', timestamp });

  if (inline && !missing && kind === 'image') {
    return (
      <button
        type="button"
        className="tw-sent-media"
        title={t('timeline.sentFilesItem.openNamed', { name })}
        onClick={open}
      >
        <img src={filePreviewUrl(path)} alt={name} loading="lazy" onError={() => setBroken(true)} />
      </button>
    );
  }
  if (inline && !missing && kind === 'video') {
    return (
      <div className="tw-sent-media">
        <video
          src={filePreviewUrl(path)}
          controls
          playsInline
          preload="metadata"
          onError={() => setBroken(true)}
        />
        <div className="tw-sent-media-bar">
          <span title={path}>{name}</span>
          {info?.size !== undefined && <small>{formatSize(info.size)}</small>}
          <a
            className="tw-icon-btn"
            href={fileDownloadUrl(path)}
            download={name}
            title={t('timeline.sentFilesItem.download')}
            aria-label={t('timeline.sentFilesItem.download')}
          >
            <Download />
          </a>
        </div>
      </div>
    );
  }
  return (
    <div className="tw-sent-file" data-missing={missing ? true : undefined}>
      <FileText />
      <div className="tw-sent-file-name">
        <span title={path}>{name}</span>
        <small>{missing ?? (info?.size !== undefined ? formatSize(info.size) : ' ')}</small>
      </div>
      {viewable && (
        <button
          type="button"
          className="tw-icon-btn"
          title={t('timeline.sentFilesItem.open')}
          aria-label={t('timeline.sentFilesItem.open')}
          onClick={open}
        >
          <ExternalLink />
        </button>
      )}
      {!missing && info?.download && (
        <a
          className="tw-icon-btn"
          href={fileDownloadUrl(path)}
          download={name}
          title={t('timeline.sentFilesItem.download')}
          aria-label={t('timeline.sentFilesItem.download')}
        >
          <Download />
        </a>
      )}
    </div>
  );
};

/** Files the agent sent with SendUserFile: pictures and videos in place, everything else as a card to open or save. */
const SentFilesItem = ({ call }: { call: ITimelineToolCall & { sentFiles: ISentFiles } }) => {
  const { paths, caption, display } = call.sentFiles;
  const info = useFileInfo(paths);
  const [viewing, setViewing] = useState<IViewableFile | null>(null);
  const viewable = paths
    .filter((p) => fileKindOf(p) !== 'text')
    .map((path) => ({ path, kind: fileKindOf(path), origin: 'created' as const, timestamp: call.timestamp }));
  return (
    <div className="tw-wrapped tw-wrapped-files">
      <div className="tw-wrapped-head">
        <Paperclip />
        <span>
          {paths.length === 1
            ? t('timeline.sentFilesItem.sentOne')
            : t('timeline.sentFilesItem.sentMany', { count: paths.length })}
        </span>
      </div>
      {caption && <p className="tw-sent-caption">{caption}</p>}
      <div className="tw-sent-list">
        {paths.map((path) => (
          <SentFile
            key={path}
            path={path}
            info={info.status === 'ready' ? info.files.get(path) : undefined}
            unreachable={info.status === 'unreachable'}
            inline={display !== 'attach'}
            onOpen={setViewing}
            timestamp={call.timestamp}
          />
        ))}
      </div>
      <FileViewer file={viewing} files={viewable} onNavigate={setViewing} onClose={() => setViewing(null)} />
    </div>
  );
};

export default SentFilesItem;
