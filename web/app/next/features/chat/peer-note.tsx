// What other agents wrote to this session (Claude Code's teammates and background subagents, a `peer` entry): one quiet
// card on the feed's neutral surface. Its first line says where it comes from — «Фоновый агент» and the task it was
// started for — and when; under it the state (готово / ошибка) is the only colour, the agent's own name is small and
// muted. The words fold to two lines of plain text and open by height on a click; files the agent names are chips that
// open in the pane. Several notes of one delivery are one card with a count. The harness's words never reach here.
import { memo, useContext, useEffect, useState } from 'react';
import Md from '@/next/features/chat/md';
import { RefContext } from '@/next/features/chat/ref-chip';
import { fileNameOf, pathsOf, withFileNames } from '@/next/features/chat/report-paths';
import type { PeerEntry, PeerNote } from '@/next/features/chat/types';
import { openSubagent } from '@/next/features/subagents/open';
import { summaryOf } from '@/next/features/subagents/excerpt';
import { clock, formatAgoFull } from '@/next/i18n/format';
import { plural, t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import '@/next/styles/peer-note.css';

/** «5 мин назад» moves once in this long. */
const TICK_MS = 30_000;

/** How long the words take to fold back, as long as the transition in peer-note.css. */
const FOLD_MS = 280;

/** How many of the files an agent names get a button of their own; the rest are chips in its words. */
const FILE_ACTIONS = 2;

const useNow = (): number => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(timer);
  }, []);
  return now;
};

/** «shutdown_request» → «shutdown request»: the protocol's name of a notice the feed has no words for. */
const eventName = (event: string | null): string => (event ?? '').replace(/[_-]+/g, ' ').trim();

/** The team's own notices the feed has words for. */
const EVENT_KEYS: Readonly<Record<string, string>> = {
  shutdown_request: 'next.peers.note.shutdownRequest',
  shutdown_approved: 'next.peers.note.shutdownApproved',
  shutdown_rejected: 'next.peers.note.shutdownRejected',
} as const;

const TONE = { ok: 'ok', failed: 'failed', neutral: 'neutral' } as const;
type TTone = (typeof TONE)[keyof typeof TONE];

/** The note's state as a word and its tone: the only colour on the card. */
const stateOf = (note: PeerNote): { label: string; tone: TTone } => {
  if (note.failed) return { label: t('next.peers.note.failed'), tone: TONE.failed };
  if (note.kind === 'done') return { label: t('next.peers.note.done'), tone: TONE.ok };
  if (note.kind === 'event') {
    const known = note.event ? EVENT_KEYS[note.event] : undefined;
    const label = known ? t(known) : eventName(note.event) || t('next.peers.note.event');
    const tone = note.event === 'shutdown_approved' ? TONE.ok : TONE.neutral;
    return { label, tone };
  }
  return { label: t(`next.peers.note.${note.kind}`), tone: TONE.neutral };
};

/** A note of an agent this session started (a named teammate or a background subagent), not of another session. */
const isOwnAgent = (note: PeerNote): boolean =>
  Boolean(note.task || note.agentId || note.opens || note.color);

/** A teammate or a subagent the delivery says is done with what it was given. */
const isFinished = (note: PeerNote): boolean =>
  note.kind === 'done' ||
  note.kind === 'idle' ||
  (note.kind === 'event' && note.event === 'shutdown_approved');

/** What the card is about: the task the agent was started for, else its name. */
const titleOf = (note: PeerNote): string =>
  note.task?.trim() || note.from || t(note.agentId ? 'next.peers.sub.unknown' : 'next.peers.kind.session');

/** The agent's own name and its model, the muted part; the name only when the title is not already it. */
const metaOf = (note: PeerNote): string[] => {
  const name = note.task?.trim() && note.from ? note.from : '';
  return [name, note.model ?? ''].filter(Boolean);
};

/** The agent's own conversation the pane opens: what the runtime named, or a subagent's id from an older runtime. */
const convOf = (note: PeerNote): string | null => note.opens ?? note.agentId ?? null;

interface IActionsProps {
  note: PeerNote;
  files: readonly string[];
}

const Actions = ({ note, files }: IActionsProps) => {
  const { onOpen } = useContext(RefContext);
  const shown = onOpen ? files.slice(0, FILE_ACTIONS) : [];
  const conv = convOf(note);
  if (!conv && shown.length === 0) return null;
  return (
    <div className="hx-pnote__acts">
      {conv && (
        <button type="button" className="hx-pnote__act" onClick={() => openSubagent(conv)}>
          <Icon name="message-square-text" size="sm" />
          {t('next.peers.note.openChat')}
        </button>
      )}
      {shown.map((path) => (
        <button
          key={path}
          type="button"
          className="hx-pnote__act"
          title={path}
          onClick={() => onOpen?.('file', path, null)}
        >
          <Icon name="file-text" size="sm" />
          {shown.length === 1
            ? t('next.peers.note.openFile')
            : t('next.peers.note.openNamed', { name: fileNameOf(path) })}
        </button>
      ))}
    </div>
  );
};

const Body = ({ note, open, files }: { note: PeerNote; open: boolean; files: readonly string[] }) => {
  const { mounted, state } = usePresentFlag(open, FOLD_MS);
  if (!mounted) return null;
  return (
    <div className="hx-pnote__body" data-state={state}>
      <div className="hx-pnote__fold">
        <div className="hx-pnote__text">
          <Md md={note.md} paths />
        </div>
        <Actions note={note} files={files} />
      </div>
    </div>
  );
};

const Note = ({ note, at, now }: { note: PeerNote; at: number; now: number }) => {
  const [open, setOpen] = useState(false);
  const when = note.sentAt ?? at;
  const state = stateOf(note);
  const gist = note.summary?.trim() || summaryOf(withFileNames(note.md));
  const files = pathsOf(note.md);
  const opens = Boolean(note.md.trim()) || convOf(note) !== null;
  const toggle = () => setOpen((value) => !value);
  const meta = metaOf(note);
  const head = (
    <>
      <span className="hx-pnote__line">
        <span className="hx-pnote__lead">
          {t(isOwnAgent(note) ? 'next.peers.kind.subagent' : 'next.peers.kind.session')}
        </span>
        <span className="hx-pnote__title">{titleOf(note)}</span>
      </span>
      <time className="hx-pnote__at" dateTime={new Date(when).toISOString()} title={clock(when)}>
        {formatAgoFull(when, now)}
      </time>
      {opens && <Icon name="chevron-down" size="sm" className="hx-pnote__chev" />}
    </>
  );
  return (
    <article className="hx-pnote" data-open={open} data-tone={state.tone}>
      {opens ? (
        <button type="button" className="hx-pnote__head" aria-expanded={open} onClick={toggle}>
          {head}
        </button>
      ) : (
        <div className="hx-pnote__head">{head}</div>
      )}
      <div className="hx-pnote__meta">
        <span className="hx-pnote__state">{state.label}</span>
        {meta.length > 0 && <span className="hx-pnote__name">{meta.join(' · ')}</span>}
      </div>
      {gist && (
        <button type="button" className="hx-pnote__gist" tabIndex={-1} aria-hidden="true" onClick={toggle}>
          <span className="hx-pnote__gist-in">{gist}</span>
        </button>
      )}
      {opens && <Body note={note} open={open} files={files} />}
    </article>
  );
};

/** The count over several notes: how many agents finished, or how many wrote. */
const groupTitle = (notes: readonly PeerNote[]): string =>
  notes.every(isFinished)
    ? plural(notes.length, 'next.peers.note.groupDone')
    : plural(notes.length, 'next.peers.note.groupSome');

/** The messages of one delivery (or of deliveries that follow each other), in order. */
const PeerNotes = ({ entry }: { entry: PeerEntry }) => {
  const now = useNow();
  const many = entry.peers.length > 1;
  return (
    <div className="hx-msg" data-kind="peer-note">
      <section className="hx-pnotes" data-many={many || undefined}>
        {many && <header className="hx-pnotes__head">{groupTitle(entry.peers)}</header>}
        {entry.peers.map((note, index) => (
          <Note key={`${note.from}-${index}`} note={note} at={entry.at} now={now} />
        ))}
      </section>
    </div>
  );
};

export default memo(PeerNotes);
