// «Изменить» of a message that has not gone yet (a row of the queue over the field, or a message in the feed that waits
// for the machine): the runtime lets it go first (pessimistic), and only then its words are back in the field, before
// what is typed there, its design notes and attached places as chips.
import { settled } from '@/next/bindings/outcome';
import { cancelMessage } from '@/next/commands';
import { attachedOf, refKey } from '@/next/features/chat/attached';
import { readAnnotated } from '@/next/features/designly/annotation';
import { REFS_MAX, type ConvId } from '@/next/protocol/v2';
import { carryNotes } from '@/next/stores/carried-notes';
import { draftsStore, setDraft } from '@/next/stores/drafts';
import { messagesStore } from '@/next/stores/messages';

export async function takeBackMessage(conv: ConvId, clientId: string): Promise<void> {
  const m = messagesStore.get(clientId);
  if (!m) return;
  const { text, attachments, refs } = m;
  await settled(cancelMessage(clientId));
  // A message that was delivered after all (the copy was stale) only leaves: its words do not come back into the field
  // to be sent a second time.
  const after = messagesStore.get(clientId);
  if (!after || after.state.k === 'done') return;
  const have = draftsStore.get(conv);
  // The words come back into the text, the design's notes and the attached places as chips: nobody edits the
  // `<annotation>` or `<attached>` markup by hand.
  const places = attachedOf(text, refs);
  const back = readAnnotated(places.text);
  carryNotes(conv, back.notes);
  const kept = have?.refs ?? [];
  const keys = new Set(kept.map(refKey));
  setDraft(conv, {
    text: [back.text, have?.text ?? ''].filter(Boolean).join('\n\n'),
    attachments: [...attachments, ...(have?.attachments ?? [])],
    refs: [...(refs?.length ? refs : places.refs).filter((r) => !keys.has(refKey(r))), ...kept].slice(
      0,
      REFS_MAX,
    ),
  });
}
