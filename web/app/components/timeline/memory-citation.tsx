import { useEffect, useState, type ReactNode } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Brain, ChevronRight } from 'lucide-react';
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { appFetch } from '@/lib/app-path';
import type { IMemoryNote } from '@/types/server/memory-notes';
import { t } from '@/lib/i18n';

const notes = new Map<string, Promise<IMemoryNote | null>>();

const loadNote = (name: string) => {
  let pending = notes.get(name);
  if (!pending) {
    pending = appFetch(`/api/platform/memory-note?name=${encodeURIComponent(name)}`)
      .then((res) => (res.ok ? (res.json() as Promise<IMemoryNote>) : null))
      .catch(() => null);
    notes.set(name, pending);
  }
  return pending;
};

const MemoryNoteRow = ({ name, onRead }: { name: string; onRead: (note: IMemoryNote) => void }) => {
  const [note, setNote] = useState<IMemoryNote | null | undefined>(undefined);
  useEffect(() => {
    let alive = true;
    void loadNote(name).then((value) => {
      if (alive) setNote(value);
    });
    return () => {
      alive = false;
    };
  }, [name]);
  if (note === undefined)
    return (
      <div className="tw-memory-row">
        <strong className="tw-memory-pending">{name}</strong>
      </div>
    );
  if (!note)
    return (
      <div className="tw-memory-row">
        <strong>{name}</strong>
        <p>{t('timeline.memoryCitation.fileMissing')}</p>
      </div>
    );
  return (
    <button type="button" className="tw-memory-row tw-memory-row-open" onClick={() => onRead(note)}>
      <strong>{note.title}</strong>
      {note.description && <p>{note.description}</p>}
      <small>
        {name}
        <ChevronRight />
      </small>
    </button>
  );
};

/** A sentence the agent based on its saved memory, with the cited memories one click away. */
const MemoryCitation = ({ files, children }: { files?: string; children?: ReactNode; node?: unknown }) => {
  const names = (files ?? '').split(',').filter(Boolean);
  const [reading, setReading] = useState<IMemoryNote | null>(null);
  // A closed dialog per citation was a dialog root per sentence in every answer; it mounts on the first read.
  const [viewerMounted, setViewerMounted] = useState(false);
  const read = (note: IMemoryNote) => {
    setViewerMounted(true);
    setReading(note);
  };
  const label =
    names.length === 1
      ? t('timeline.memoryCitation.fromMemory')
      : t('timeline.memoryCitation.fromMemoryCount', { count: names.length });
  return (
    <span className="tw-memory">
      {children}
      <Popover>
        <PopoverTrigger
          render={
            <button
              type="button"
              className="tw-memory-chip"
              aria-label={`${label}: ${names.join(', ')}`}
              title={label}
            />
          }
        >
          <Brain />
        </PopoverTrigger>
        <PopoverContent side="top" align="start" sideOffset={6} className="tw-link-card tw-memory-card">
          <div className="tw-link-card-head">
            <Brain className="tw-link-card-icon" />
            {label}
          </div>
          {names.map((name) => (
            <MemoryNoteRow key={name} name={name} onRead={read} />
          ))}
        </PopoverContent>
      </Popover>
      {viewerMounted && (
        <Dialog
          open={reading !== null}
          onOpenChange={(open) => {
            if (!open) setReading(null);
          }}
        >
          <DialogContent className="tw-island-dialog tw-viewer">
            <div className="tw-viewer-head">
              <DialogTitle className="tw-island-dialog-title">{reading?.title}</DialogTitle>
              <small>{reading?.name}</small>
            </div>
            <div className="tw-viewer-body">
              <article className="tw-viewer-md">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{reading?.body ?? ''}</ReactMarkdown>
              </article>
            </div>
          </DialogContent>
        </Dialog>
      )}
    </span>
  );
};

export default MemoryCitation;
