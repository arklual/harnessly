// What other agents' sessions wrote to this one (Claude Code's teammates, a `peer` entry): a compact card per message
// on the left, not the person's bubble — a dot in the sender's color, its name, what kind of message it is, when it
// was sent, and the body as Markdown text folded to a few lines. The harness's own words around it never reach here.
import { memo, useEffect, useState } from 'react';
import Md from '@/next/features/chat/md';
import type { PeerEntry, PeerNote } from '@/next/features/chat/types';
import { openSubagent } from '@/next/features/subagents/open';
import { providerOf } from '@/next/features/subagents/subs-block';
import { clock, formatAgo } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import '@/next/styles/peer-note.css';

/** The colors Claude Code gives its teammates; any other shows neutral. */
const PEER_COLORS = ['red', 'blue', 'green', 'yellow', 'purple', 'orange', 'pink', 'cyan'] as const;
type TPeerColor = (typeof PEER_COLORS)[number];

const colorOf = (color: string | null): TPeerColor | undefined =>
  PEER_COLORS.find((known) => known === color);

/** «5 min ago» moves once in this long. */
const TICK_MS = 30_000;

const useNow = (): number => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(timer);
  }, []);
  return now;
};

/** A body longer than this many lines or characters starts folded (to about six lines). */
const FOLD_LINES = 6;
const FOLD_CHARS = 420;

/** «shutdown_request» → «shutdown request»: the protocol's name of a notice the feed has no words for. */
const eventName = (event: string | null): string => (event ?? '').replace(/[_-]+/g, ' ').trim();

/** The team's own notices the feed has words for. */
const EVENT_KEYS: Readonly<Record<string, string>> = {
  shutdown_request: 'next.peers.note.shutdownRequest',
  shutdown_approved: 'next.peers.note.shutdownApproved',
  shutdown_rejected: 'next.peers.note.shutdownRejected',
} as const;

const kindLabel = (note: PeerNote): string => {
  const known = note.event ? EVENT_KEYS[note.event] : undefined;
  if (note.kind === 'event' && known) return t(known);
  if (note.kind === 'event') return eventName(note.event) || t('next.peers.note.event');
  return t(`next.peers.note.${note.kind}`);
};

const Body = ({ md }: { md: string }) => {
  const [open, setOpen] = useState(false);
  const long = md.split('\n').length > FOLD_LINES || md.length > FOLD_CHARS;
  return (
    <div className="hx-pnote__body" data-open={open || !long || undefined}>
      <div className="hx-pnote__text">
        <Md md={md} paths />
      </div>
      {long && (
        <button
          type="button"
          className="hx-pnote__more"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
        >
          {open ? t('next.peers.note.less') : t('next.peers.note.more')}
        </button>
      )}
    </div>
  );
};

/** The label of a subagent's note: a plain message says who writes to whom; any other kind keeps its own word. */
const subagentLabel = (note: PeerNote): string =>
  note.kind === 'message' ? t('next.peers.sub.says') : kindLabel(note);

const Note = ({ note, at, now }: { note: PeerNote; at: number; now: number }) => {
  const when = note.sentAt ?? at;
  const agentId = note.agentId;
  const who = note.from || t(agentId ? 'next.peers.sub.unknown' : 'next.peers.kind.session');
  return (
    <article
      className="hx-pnote"
      data-color={agentId ? undefined : colorOf(note.color)}
      data-kind={note.kind}
      data-subagent={agentId ? true : undefined}
    >
      <header className="hx-pnote__head">
        {agentId ? (
          <i className="hx-agent" data-agent={providerOf(note.model)} aria-hidden="true" />
        ) : (
          <span className="hx-pnote__dot" aria-hidden="true" />
        )}
        {agentId ? (
          <button
            type="button"
            className="hx-pnote__who hx-pnote__who--link"
            title={t('next.peers.sub.open')}
            onClick={() => openSubagent(agentId)}
          >
            {who}
          </button>
        ) : (
          <span className="hx-pnote__who">{who}</span>
        )}
        <span className="hx-pnote__kind">{agentId ? subagentLabel(note) : kindLabel(note)}</span>
        {note.model && <span className="hx-pnote__model">{note.model}</span>}
        <time className="hx-pnote__at" dateTime={new Date(when).toISOString()} title={clock(when)}>
          {formatAgo(when, now)}
        </time>
      </header>
      {note.summary && <div className="hx-pnote__sum">{note.summary}</div>}
      {note.md && <Body md={note.md} />}
    </article>
  );
};

/** The messages of one delivery, in order. */
const PeerNotes = ({ entry }: { entry: PeerEntry }) => {
  const now = useNow();
  return (
    <div className="hx-msg" data-kind="peer-note">
      <div className="hx-pnotes">
        {entry.peers.map((note, index) => (
          <Note key={`${note.from}-${index}`} note={note} at={entry.at} now={now} />
        ))}
      </div>
    </div>
  );
};

export default memo(PeerNotes);
