import { describe, expect, it } from 'vitest';
import { commentPlaceholder, composeAnnotationComment, pickedRanges } from '@/lib/hl-annotation-composer';
import { ANNOTATION_BLOCK_RE, appendAnnotations, parseAnnotationBlock } from '@/lib/hl-annotation-format';
import type { IAnnotation } from '@/lib/hl-annotations-store';
import { textAnnotationLabel, type ITextAnnotation, type TTextIntent } from '@/lib/hl-text-annotation';

const fragment = (id: string, intent: TTextIntent = 'ask', comment = '', range?: Range): ITextAnnotation => ({
  kind: 'text',
  id,
  n: Number(id.slice(1)),
  intent,
  comment,
  range,
  quote: { quote: 'the cache lives five minutes', before: 'Now', after: 'and is reset', truncated: false },
  source: { entryId: 'e-1', role: 'agent' },
});

const picked = { id: 'p1', n: 1, comment: '', resolve: { status: 'none' } } as unknown as IAnnotation;

const rangeIn = (connected: boolean) =>
  ({ commonAncestorContainer: { isConnected: connected } }) as unknown as Range;

describe('commentPlaceholder', () => {
  it('has none without a fragment in the composer', () => {
    expect(commentPlaceholder([])).toBeNull();
    expect(commentPlaceholder([picked])).toBeNull();
  });

  it('asks for the comment of a sole uncommented fragment', () => {
    expect(commentPlaceholder([fragment('a1', 'note')])).toBe('Comment on this fragment…');
    expect(commentPlaceholder([fragment('a1', 'ask'), picked])).toBe(
      'What do you want to ask about this fragment…',
    );
  });

  it('asks for the message itself when the fragments carry their own comments', () => {
    const message = 'A note for the whole message (optional)…';
    expect(commentPlaceholder([fragment('a1', 'note'), fragment('a2', 'note')])).toBe(message);
    expect(commentPlaceholder([fragment('a1', 'note', 'already there')])).toBe(message);
  });
});

describe('composeAnnotationComment', () => {
  it('leaves the text alone without fragments', () => {
    const list = [picked];
    expect(composeAnnotationComment('Hello', list)).toEqual({ text: 'Hello', annotations: list });
    expect(composeAnnotationComment('Hello', [])).toEqual({ text: 'Hello', annotations: [] });
  });

  it('turns the typed text into the comment of a sole uncommented fragment', () => {
    const { text, annotations } = composeAnnotationComment('  why five?\n', [fragment('a1', 'note')]);
    expect(text).toBe('');
    expect(annotations.map((a) => a.comment)).toEqual(['why five?']);
  });

  it('keeps the typed text as the message when several fragments have their own comments', () => {
    const list = [fragment('a1', 'note', 'about the cache'), fragment('a2', 'note', 'about the timeout')];
    const { text, annotations } = composeAnnotationComment('Look at both places', list);
    expect(text).toBe('Look at both places');
    expect(annotations.map((a) => a.comment)).toEqual(['about the cache', 'about the timeout']);
  });

  it('keeps the typed text as the message when the only fragment is already commented', () => {
    const { text, annotations } = composeAnnotationComment('and more', [fragment('a1', 'note', 'before')]);
    expect(text).toBe('and more');
    expect(annotations[0].comment).toBe('before');
  });

  it('sends a fragment without a comment when nothing is typed', () => {
    const list = [fragment('a1', 'note')];
    expect(composeAnnotationComment('   ', list)).toEqual({ text: '', annotations: list });
  });
});

describe('pickedRanges', () => {
  it('keeps the fragments that are still in the page', () => {
    const live = rangeIn(true);
    expect(
      pickedRanges([fragment('a1', 'note', '', live), fragment('a2', 'note', '', rangeIn(false)), picked]),
    ).toEqual([live]);
  });
});

describe('annotations with their own comments', () => {
  const noted = (n: number, quote: string, comment: string): ITextAnnotation => ({
    kind: 'text',
    id: `a${n}`,
    n,
    intent: 'note',
    comment,
    quote: { quote, before: '', after: '', truncated: false },
    source: { entryId: 'e-1', role: 'agent' },
  });

  it('name an annotation as one, and the older choices by their card names', () => {
    expect(textAnnotationLabel(fragment('a1', 'note'))).toBe(
      '#1 Annotation · «the cache lives five minutes»',
    );
    expect(textAnnotationLabel(fragment('a3', 'fix'))).toBe('#3 Correction · «the cache lives five minutes»');
  });

  it('keep each comment inside the block of the fragment it is about', () => {
    const message = appendAnnotations('Look at both places', [
      noted(1, 'the cache lives five minutes', 'why five, not an hour?'),
      noted(2, 'timeout is 30 seconds', 'that is too long'),
    ]);
    expect(message.startsWith('Look at both places\n\n')).toBe(true);
    const blocks = message.match(ANNOTATION_BLOCK_RE) ?? [];
    expect(
      blocks.map((block) => {
        const parsed = parseAnnotationBlock(block);
        return [parsed?.quote?.text, parsed?.comment, parsed?.label];
      }),
    ).toEqual([
      [
        'the cache lives five minutes',
        'why five, not an hour?',
        '#1 Annotation · «the cache lives five minutes»',
      ],
      ['timeout is 30 seconds', 'that is too long', '#2 Annotation · «timeout is 30 seconds»'],
    ]);
  });
});
