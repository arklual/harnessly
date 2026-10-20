import { useCallback, useEffect, useRef, useState } from 'react';
import {
  ArrowLeft,
  Check,
  ChevronRight,
  FileCode2,
  FileJson,
  FileText,
  Folder,
  FolderCode,
  RefreshCw,
  Save,
  X,
} from 'lucide-react';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import Spinner from '@/components/ui/spinner';
import { appFetch } from '@/lib/app-path';
import { cn } from '@/lib/utils';
import type { WorkspaceFile, WorkspaceFileEntry } from '@/types/server/workspace-files';
import { locale, plural, t } from '@/lib/i18n';

interface Listing {
  name: string;
  path: string;
  entries: WorkspaceFileEntry[];
  truncated: boolean;
}
interface Props {
  sessionName: string;
  onClose: () => void;
  initialPath?: string;
}
const normalized = (text: string) => text.replace(/\r\n?/g, '\n');
const draftKey = (session: string) => `hl:editor-draft:${session}`;
const parentDirectory = (path: string) => path.split('/').slice(0, -1).join('/');
const readStoredDraft = (session: string): { file: WorkspaceFile; draft: string } | null => {
  try {
    const stored = JSON.parse(sessionStorage.getItem(draftKey(session)) || 'null');
    if (
      stored?.file &&
      typeof stored.file.path === 'string' &&
      typeof stored.file.content === 'string' &&
      typeof stored.file.revision === 'string' &&
      typeof stored.draft === 'string'
    ) {
      return { file: stored.file, draft: stored.draft };
    }
  } catch {
    /* Storage may be unavailable in private browsing. */
  }
  return null;
};

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await appFetch(url, init);
  const data = await response.json().catch(() => ({}));
  if (!response.ok)
    throw new Error(data.error || t('workspace.fileEditorDialog.requestFailed', { status: response.status }));
  return data as T;
}
function FileIcon({ name, className }: { name: string; className?: string }) {
  const Icon = /\.(json|jsonc|ya?ml|toml)$/i.test(name)
    ? FileJson
    : /\.(tsx?|jsx?|py|css|html|sh|go|rs|sql|vue|svelte)$/i.test(name)
      ? FileCode2
      : FileText;
  return <Icon className={className} />;
}

export default function FileEditorDialog({ sessionName, onClose, initialPath }: Props) {
  const [directory, setDirectory] = useState('');
  const [listing, setListing] = useState<Listing | null>(null);
  const [listError, setListError] = useState('');
  const [listBusy, setListBusy] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const [file, setFile] = useState<WorkspaceFile | null>(null);
  const [draft, setDraft] = useState('');
  const [fileBusy, setFileBusy] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);
  const [pending, setPending] = useState<(() => void) | null>(null);
  const lineNumbers = useRef<HTMLPreElement>(null);
  const fileRequest = useRef<AbortController | null>(null);
  const textarea = useRef<HTMLTextAreaElement>(null);
  const dirty = !!file && draft !== normalized(file.content);
  const lines = draft.split('\n').length;
  const listRequestKey = JSON.stringify([sessionName, directory, refresh]);
  const [startedListKey, setStartedListKey] = useState(listRequestKey);
  if (startedListKey !== listRequestKey) {
    setStartedListKey(listRequestKey);
    setListBusy(true);
    setListError('');
  }

  const [restoredSession, setRestoredSession] = useState<string | null>(null);
  if (restoredSession !== sessionName) {
    setRestoredSession(sessionName);
    const stored = readStoredDraft(sessionName);
    if (stored) {
      setFile(stored.file);
      setDraft(stored.draft);
      setDirectory(parentDirectory(stored.file.path));
    }
  }

  useEffect(() => () => fileRequest.current?.abort(), [sessionName]);

  useEffect(() => {
    if (!file) return;
    try {
      if (dirty) sessionStorage.setItem(draftKey(sessionName), JSON.stringify({ file, draft }));
      else sessionStorage.removeItem(draftKey(sessionName));
    } catch {
      /* The close and unload guards still protect the current draft. */
    }
  }, [dirty, draft, file, sessionName]);

  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);

  useEffect(() => {
    const controller = new AbortController();
    const params = new URLSearchParams({ session: sessionName, path: directory });
    request<Listing>(`/api/editor/files?${params}`, { signal: controller.signal })
      .then(setListing)
      .catch((cause) => {
        if (!controller.signal.aborted) setListError(cause.message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setListBusy(false);
      });
    return () => controller.abort();
  }, [sessionName, directory, refresh]);

  const guarded = (action: () => void) => {
    if (saving || fileBusy) return;
    if (dirty) setPending(() => action);
    else action();
  };
  const fetchFile = (path: string) => {
    fileRequest.current?.abort();
    const controller = new AbortController();
    fileRequest.current = controller;
    return request<WorkspaceFile>(`/api/editor/file?${new URLSearchParams({ session: sessionName, path })}`, {
      signal: controller.signal,
    })
      .then((next) => {
        if (controller.signal.aborted) return;
        setFile(next);
        setDraft(normalized(next.content));
        if (lineNumbers.current) lineNumbers.current.scrollTop = 0;
        if (textarea.current) textarea.current.scrollTop = 0;
      })
      .catch((cause) => {
        if (!controller.signal.aborted)
          setError(cause instanceof Error ? cause.message : t('workspace.fileEditorDialog.openFailed'));
      })
      .finally(() => {
        if (!controller.signal.aborted) setFileBusy(false);
      });
  };
  const openFile = (path: string) => {
    setFileBusy(true);
    setError('');
    setSaved(false);
    return fetchFile(path);
  };

  const [appliedInitialPath, setAppliedInitialPath] = useState<string | undefined>(undefined);
  if (appliedInitialPath !== initialPath) {
    setAppliedInitialPath(initialPath);
    if (initialPath) {
      setDirectory(parentDirectory(initialPath));
      setFileBusy(true);
      setError('');
      setSaved(false);
    }
  }
  useEffect(() => {
    if (!initialPath) return;
    void fetchFile(initialPath);
  }, [initialPath]); // eslint-disable-line react-hooks/exhaustive-deps

  const save = useCallback(async () => {
    if (!file || !dirty || saving || fileBusy) return;
    setSaving(true);
    setError('');
    setSaved(false);
    const content = file.content.includes('\r\n') ? draft.replace(/\n/g, '\r\n') : draft;
    try {
      const next = await request<WorkspaceFile>(
        `/api/editor/file?${new URLSearchParams({ session: sessionName, path: file.path })}`,
        {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content, revision: file.revision }),
        },
      );
      setFile(next);
      setDraft(normalized(next.content));
      setSaved(true);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('workspace.fileEditorDialog.saveFailed'));
    } finally {
      setSaving(false);
    }
  }, [dirty, draft, file, fileBusy, saving, sessionName]);

  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
        event.preventDefault();
        event.stopPropagation();
        void save();
      }
    };
    window.addEventListener('keydown', shortcut, true);
    return () => window.removeEventListener('keydown', shortcut, true);
  }, [save]);

  const close = () => {
    try {
      sessionStorage.removeItem(draftKey(sessionName));
    } catch {}
    onClose();
  };
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) guarded(close);
      }}
    >
      <DialogContent
        showCloseButton={false}
        className="flex h-[min(760px,92dvh)] w-[min(1180px,96vw)] max-w-[96vw] flex-col gap-0 overflow-hidden p-0 sm:max-w-[96vw]"
      >
        <header className="flex shrink-0 items-center gap-3 border-b px-5 py-4">
          <span className="flex size-9 items-center justify-center rounded-xl bg-muted text-muted-foreground">
            <FolderCode className="size-4" />
          </span>
          <div className="min-w-0 flex-1">
            <DialogTitle>{t('workspace.fileEditorDialog.title')}</DialogTitle>
            <DialogDescription className="mt-1 truncate text-xs">
              {listing?.name || t('workspace.fileEditorDialog.agentFolder')}
            </DialogDescription>
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t('workspace.fileEditorDialog.closeEditor')}
            onClick={() => guarded(close)}
            disabled={saving || fileBusy}
          >
            <X />
          </Button>
        </header>
        {pending && (
          <div
            className="flex shrink-0 flex-wrap items-center gap-2 border-b bg-amber-50 px-4 py-3 text-xs text-amber-950 dark:bg-amber-950 dark:text-amber-100"
            role="alert"
          >
            <span className="mr-auto">{t('workspace.fileEditorDialog.discardPrompt')}</span>
            <Button size="sm" variant="outline" onClick={() => setPending(null)}>
              {t('workspace.fileEditorDialog.keepEditing')}
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={() => {
                const action = pending;
                setPending(null);
                action();
              }}
            >
              {t('workspace.fileEditorDialog.discard')}
            </Button>
          </div>
        )}
        <div className="flex min-h-0 flex-1 flex-col sm:flex-row">
          <aside className="flex max-h-44 w-full shrink-0 flex-col border-b bg-muted/20 sm:max-h-none sm:w-56 sm:border-r sm:border-b-0">
            <div className="flex h-11 shrink-0 items-center gap-1 border-b px-2">
              <Button
                size="icon-xs"
                variant="ghost"
                disabled={!directory || listBusy}
                aria-label={t('workspace.fileEditorDialog.up')}
                onClick={() => setDirectory(parentDirectory(directory))}
              >
                <ArrowLeft />
              </Button>
              <span className="min-w-0 flex-1 truncate px-1 text-xs text-muted-foreground" title={directory}>
                {directory || t('workspace.fileEditorDialog.root')}
              </span>
              <Button
                size="icon-xs"
                variant="ghost"
                aria-label={t('workspace.fileEditorDialog.refresh')}
                disabled={listBusy}
                onClick={() => setRefresh((n) => n + 1)}
              >
                <RefreshCw />
              </Button>
            </div>
            <nav
              className="min-h-0 flex-1 overflow-auto p-2"
              aria-label={t('workspace.fileEditorDialog.title')}
            >
              {listBusy ? (
                <div className="flex justify-center p-5">
                  <Spinner className="size-4" />
                </div>
              ) : listError ? (
                <div className="p-2 text-xs text-destructive" role="alert">
                  {listError}
                  <button className="mt-2 block underline" onClick={() => setRefresh((n) => n + 1)}>
                    {t('workspace.fileEditorDialog.retry')}
                  </button>
                </div>
              ) : (
                <>
                  {listing?.entries.map((entry) => (
                    <button
                      key={entry.path}
                      disabled={fileBusy || saving}
                      onClick={() =>
                        entry.kind === 'directory'
                          ? setDirectory(entry.path)
                          : guarded(() => void openFile(entry.path))
                      }
                      className={cn(
                        'flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-xs transition-colors hover:bg-muted disabled:opacity-50',
                        file?.path === entry.path && 'bg-accent text-accent-foreground',
                      )}
                      title={entry.name}
                    >
                      {entry.kind === 'directory' ? (
                        <Folder className="size-3.5 shrink-0 text-muted-foreground" />
                      ) : (
                        <FileIcon name={entry.name} className="size-3.5 shrink-0 text-muted-foreground" />
                      )}
                      <span className="min-w-0 flex-1 truncate">{entry.name}</span>
                      {entry.kind === 'directory' && (
                        <ChevronRight className="size-3 text-muted-foreground" />
                      )}
                    </button>
                  ))}
                  {listing?.entries.length === 0 && (
                    <p className="p-3 text-xs text-muted-foreground">
                      {t('workspace.fileEditorDialog.emptyFolder')}
                    </p>
                  )}
                  {listing?.truncated && (
                    <p className="p-3 text-xs text-muted-foreground">
                      {t('workspace.fileEditorDialog.truncated', { count: 2000 })}
                    </p>
                  )}
                </>
              )}
            </nav>
          </aside>
          <section
            className="flex min-h-0 min-w-0 flex-1 flex-col"
            aria-label={t('workspace.fileEditorDialog.editorLabel')}
          >
            <div className="flex h-11 shrink-0 items-center gap-2 border-b px-3">
              {file && <FileIcon name={file.path} className="size-3.5 shrink-0 text-muted-foreground" />}
              <span className="min-w-0 flex-1 truncate text-xs" title={file?.path}>
                {file?.path || t('workspace.fileEditorDialog.pickFile')}
              </span>
              {dirty && (
                <span
                  title={t('workspace.fileEditorDialog.unsaved')}
                  className="size-1.5 shrink-0 rounded-full bg-amber-500"
                />
              )}
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={t('workspace.fileEditorDialog.reload')}
                disabled={!file || saving || fileBusy}
                onClick={() => file && guarded(() => void openFile(file.path))}
              >
                <RefreshCw />
              </Button>
              <Button size="sm" disabled={!dirty || saving || fileBusy} onClick={() => void save()}>
                {saving ? <Spinner className="size-3.5" /> : <Save className="size-3.5" />}
                <span>
                  {saving
                    ? t('workspace.fileEditorDialog.savingButton')
                    : t('workspace.fileEditorDialog.save')}
                </span>
              </Button>
            </div>
            {error && (
              <div
                className="shrink-0 border-b bg-destructive/5 px-4 py-3 text-xs leading-relaxed text-destructive"
                role="alert"
              >
                {error}
              </div>
            )}
            {fileBusy ? (
              <div className="flex flex-1 items-center justify-center">
                <Spinner className="size-5" />
              </div>
            ) : file ? (
              <div className="flex min-h-0 flex-1 overflow-hidden bg-background">
                <pre
                  ref={lineNumbers}
                  aria-hidden="true"
                  className="hidden shrink-0 overflow-hidden border-r bg-muted/20 px-3 py-4 text-right font-mono text-[12px] leading-[22px] text-muted-foreground/50 sm:block"
                >
                  {Array.from({ length: Math.min(lines, 20000) }, (_, i) => i + 1).join('\n')}
                </pre>
                <textarea
                  ref={textarea}
                  aria-label={t('workspace.fileEditorDialog.contentOf', { path: file.path })}
                  value={draft}
                  disabled={saving}
                  onChange={(event) => {
                    setDraft(event.target.value);
                    setSaved(false);
                  }}
                  onScroll={(event) => {
                    if (lineNumbers.current) lineNumbers.current.scrollTop = event.currentTarget.scrollTop;
                  }}
                  spellCheck={false}
                  autoCorrect="off"
                  autoCapitalize="off"
                  autoComplete="off"
                  data-lpignore="true"
                  wrap="off"
                  className="min-h-0 min-w-0 flex-1 resize-none overflow-auto border-0 bg-transparent px-4 py-4 font-mono text-[13px] leading-[22px] outline-none disabled:opacity-70"
                  style={{ tabSize: 2 }}
                />
              </div>
            ) : (
              <div className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center text-muted-foreground">
                <FileCode2 className="size-8 opacity-40" />
                <p className="text-sm">
                  {t('workspace.fileEditorDialog.emptyLine1')}
                  <br />
                  {t('workspace.fileEditorDialog.emptyLine2')}
                </p>
                <p className="text-xs opacity-60">{t('workspace.fileEditorDialog.sizeLimit')}</p>
              </div>
            )}
            <footer
              className="flex h-8 shrink-0 items-center gap-3 border-t px-3 text-[10px] text-muted-foreground"
              aria-live="polite"
            >
              <span className="mr-auto flex items-center gap-1">
                {saving ? (
                  t('workspace.fileEditorDialog.saving')
                ) : dirty ? (
                  t('workspace.fileEditorDialog.notSaved')
                ) : saved ? (
                  <>
                    <Check className="size-3" />
                    {t('workspace.fileEditorDialog.saved')}
                  </>
                ) : file ? (
                  t('workspace.fileEditorDialog.allSaved')
                ) : (
                  t('workspace.fileEditorDialog.ready')
                )}
              </span>
              {file && (
                <>
                  <span>
                    {plural(lines, 'workspace.fileEditorDialog.lines', { n: lines.toLocaleString(locale()) })}
                  </span>
                  <span>UTF-8</span>
                </>
              )}
              <span className="hidden sm:inline">⌘ / Ctrl + S</span>
            </footer>
          </section>
        </div>
      </DialogContent>
    </Dialog>
  );
}
