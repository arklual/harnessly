import { Button, EmptyState, IconButton, Spinner, Tabs, TextField, useTheme } from '@kit';
import { ExternalLink, FileText, PenLine, RotateCw, X } from 'lucide-react';
import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react';

import { type ConversationDocument, integrations } from '../api/integrations';
import { formatAgo } from '../lib/format';
import { findPane } from '../lib/layout-tree';
import { type BoardLink, type DocumentLink, linkTitle, recognize, themedEmbed } from '../lib/links';
import { layoutsStore } from '../stores/runtime';
import {
  integrationsStore,
  openInPane,
  reloadPane,
  showPane,
  type SidePaneTab,
  sidePaneStore,
} from '../stores/side-pane';
import { useStore } from '../stores/store';

/** The tab whose conversation the pane follows: the active tab of the workspace's active pane. */
function useConversation(workspaceId: string): string | null {
  return useStore(layoutsStore, (all) => {
    const l = all[workspaceId];
    if (!l) return null;
    return findPane(l.root, l.activePaneId)?.activeTabId ?? null;
  });
}

/** Pastes a link into the pane when there is nothing open yet. */
function OpenLink({ kind }: { kind: 'board' | 'document' }) {
  const services = useStore(integrationsStore);
  const [text, setText] = useState('');
  const [error, setError] = useState<string | null>(null);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const link = recognize(text, services);
    if (!link || link.kind !== kind) {
      setError(
        kind === 'board'
          ? 'Not a link to a board on this boards server'
          : 'Not a link to a document in this catalog',
      );
      return;
    }
    setError(null);
    setText('');
    openInPane(link);
  };
  return (
    <form className="side__open" onSubmit={submit}>
      <TextField
        size="s"
        aria-label={kind === 'board' ? 'Board link' : 'Document link'}
        placeholder={kind === 'board' ? 'Paste a board link' : 'Paste a document link'}
        value={text}
        error={error ?? undefined}
        onChange={(e) => setText(e.target.value)}
      />
      <Button size="s" type="submit" disabled={!text.trim()}>
        Open
      </Button>
    </form>
  );
}

function BoardView({ board, reload, hidden }: { board: BoardLink | null; reload: number; hidden: boolean }) {
  const [src, setSrc] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!board) return;
    let live = true;
    // A hand-off link works once: every load of the board asks for a new one.
    integrations
      .boardSignIn(board.path)
      .then((r) => {
        if (!live) return;
        setError(null);
        setSrc(r.url);
      })
      .catch((err: unknown) => {
        if (!live) return;
        setSrc(null);
        setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      live = false;
      setSrc(null);
    };
  }, [board, reload]);

  return (
    <div className="side__view" hidden={hidden} data-testid="board-view">
      {!board ? (
        <EmptyState icon={<PenLine size={20} />} title="No board open" actions={<OpenLink kind="board" />}>
          Board links in a conversation open here.
        </EmptyState>
      ) : error ? (
        <EmptyState
          icon={<PenLine size={20} />}
          title="The board did not open"
          actions={
            <>
              <Button size="s" icon={<RotateCw size={14} />} onClick={reloadPane}>
                Try again
              </Button>
              <a
                className="k-btn"
                data-appearance="ghost"
                data-size="s"
                href={board.url}
                target="_blank"
                rel="noreferrer"
              >
                <span className="k-btn__content">
                  <ExternalLink size={14} />
                  <span className="k-btn__label">Open in a new tab</span>
                </span>
              </a>
            </>
          }
        >
          {error}
        </EmptyState>
      ) : !src ? (
        <div className="side__loading">
          <Spinner label="Opening the board" />
        </div>
      ) : (
        <iframe
          key={src}
          className="side__frame"
          title={linkTitle(board)}
          src={src}
          allow="clipboard-read; clipboard-write; fullscreen"
          referrerPolicy="no-referrer"
          data-testid="board-frame"
        />
      )}
    </div>
  );
}

function useDocuments(conversation: string | null, enabled: boolean) {
  const [docs, setDocs] = useState<ConversationDocument[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const load = useCallback(async () => {
    if (!conversation) return;
    try {
      const r = await integrations.documents(conversation);
      setDocs(r.documents);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [conversation]);
  useEffect(() => {
    // the list is the conversation's: a new one starts empty until it loads
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setDocs(null);
    if (!enabled || !conversation) return;
    void load();
    // Agents publish while they work; the catalog has no push to the page.
    const t = setInterval(() => void load(), 15_000);
    return () => clearInterval(t);
  }, [conversation, enabled, load]);
  return { docs, error, load };
}

function DocumentView({
  document: doc,
  reload,
  hidden,
  origin,
  conversation,
}: {
  document: DocumentLink | null;
  reload: number;
  hidden: boolean;
  origin: string;
  conversation: string | null;
}) {
  const { resolved } = useTheme();
  const frame = useRef<HTMLIFrameElement>(null);
  const [signIn, setSignIn] = useState<string | null>(null);
  const { docs, error, load } = useDocuments(conversation, true);
  // The frame loads with the theme of the moment it opens; later changes go through postMessage, without a reload.
  const loadKey = doc ? `${doc.embedUrl}#${reload}` : null;
  const [loaded, setLoaded] = useState<{ key: string; src: string } | null>(null);
  if (loadKey !== (loaded?.key ?? null))
    setLoaded(doc && loadKey ? { key: loadKey, src: themedEmbed(doc.embedUrl, resolved) } : null);
  const src = loaded?.src ?? null;

  useEffect(() => {
    frame.current?.contentWindow?.postMessage({ type: 'artifacts:theme', theme: resolved }, origin);
  }, [resolved, origin]);

  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.origin !== origin || e.source !== frame.current?.contentWindow) return;
      const m = e.data as { type?: string; loginUrl?: string } | null;
      if (m?.type === 'artifacts:auth-required') {
        setSignIn(new URL(m.loginUrl || '/login', origin).toString());
      } else if (m?.type === 'artifacts:ready') {
        setSignIn(null);
        void load();
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [origin, load]);

  const startSignIn = () => {
    if (!signIn) return;
    // Identity providers refuse to be framed: the sign-in runs in a window of its own, and the frame reloads after.
    const w = window.open(signIn, 'artifacts-sign-in', 'popup,width=520,height=680');
    if (!w) return;
    const t = setInterval(() => {
      if (w.closed) {
        clearInterval(t);
        setSignIn(null);
        reloadPane();
      }
    }, 500);
  };

  return (
    <div className="side__view" hidden={hidden} data-testid="document-view">
      {conversation && (
        <div className="side__docs" data-testid="conversation-documents">
          <div className="side__docs-head">
            <span>This conversation</span>
            {docs && docs.length > 0 && <span className="side__docs-count">{docs.length}</span>}
          </div>
          {error ? (
            <div className="side__docs-empty">{error}</div>
          ) : docs === null ? (
            <div className="side__docs-empty">
              <Spinner size="xs" /> Loading documents…
            </div>
          ) : docs.length === 0 ? (
            <div className="side__docs-empty">
              No documents yet. What the agent publishes here appears in this list.
            </div>
          ) : (
            <ul className="side__docs-list">
              {docs.map((d) => (
                <li key={d.slug}>
                  <button
                    type="button"
                    className="side__doc"
                    data-active={doc?.slug === d.slug || undefined}
                    data-testid="document-item"
                    onClick={() => {
                      const link = recognize(d.viewUrl, { boards: null, artifacts: { url: origin } });
                      if (link) openInPane(link);
                    }}
                  >
                    <FileText size={14} />
                    <span className="side__doc-title">{d.title}</span>
                    <span className="side__doc-meta">
                      v{d.version} · {formatAgo(d.updatedAt)}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
      <div className="side__doc-frame">
        {!doc ? (
          <EmptyState
            icon={<FileText size={20} />}
            title="No document open"
            actions={<OpenLink kind="document" />}
          >
            Documents an agent publishes, and document links in a conversation, open here.
          </EmptyState>
        ) : (
          <>
            <iframe
              key={loaded?.key}
              ref={frame}
              className="side__frame"
              title={linkTitle(doc)}
              src={src ?? undefined}
              referrerPolicy="no-referrer"
              data-testid="document-frame"
            />
            {signIn && (
              <div className="side__signin" role="alert">
                <span>Sign in to the catalog to read documents here.</span>
                <Button size="s" appearance="primary" onClick={startSignIn}>
                  Sign in
                </Button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

export function SidePane({ workspaceId }: { workspaceId: string }) {
  const pane = useStore(sidePaneStore);
  const services = useStore(integrationsStore);
  const conversation = useConversation(workspaceId);
  const current = pane.tab === 'board' ? pane.board : pane.document;

  const tabs = [
    { value: 'board', label: 'Board', disabled: !services.boards },
    { value: 'document', label: 'Document', disabled: !services.artifacts },
  ];

  return (
    <aside className="side" aria-label="Boards and documents" data-testid="side-pane">
      <div className="side__bar">
        <Tabs
          label="Boards and documents"
          tabs={tabs}
          selected={pane.tab}
          onSelect={(v) => showPane(true, v as SidePaneTab)}
          className="side__tabs"
        />
        <span className="side__title">{current ? linkTitle(current) : ''}</span>
        {current && (
          <>
            <IconButton label="Reload" size="s" onClick={reloadPane}>
              <RotateCw size={14} />
            </IconButton>
            <IconButton
              label="Open in a new tab"
              size="s"
              onClick={() => window.open(current.url, '_blank', 'noreferrer')}
            >
              <ExternalLink size={14} />
            </IconButton>
          </>
        )}
        <IconButton label="Close the pane" size="s" onClick={() => showPane(false)}>
          <X size={14} />
        </IconButton>
      </div>
      <div className="side__body">
        {services.boards && (
          <BoardView board={pane.board} reload={pane.reload} hidden={pane.tab !== 'board'} />
        )}
        {services.artifacts && (
          <DocumentView
            document={pane.document}
            reload={pane.reload}
            hidden={pane.tab !== 'document'}
            origin={services.artifacts.url}
            conversation={conversation}
          />
        )}
      </div>
    </aside>
  );
}
