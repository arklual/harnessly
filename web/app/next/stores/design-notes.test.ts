import { beforeEach, describe, expect, it } from 'vitest';
import { setScheduler } from '@/next/core/store';
import { NO_EDIT } from '@/next/features/designly/element-style';
import {
  clearDesignNotes,
  designNotesStore,
  patchDesignNotes,
  type IDesignNote,
} from '@/next/stores/design-notes';
import type { ConvId } from '@/next/protocol/v2';

const CONV = 'c1' as ConvId;

const note = (id: string): IDesignNote => ({
  id,
  picked: {
    id: 1,
    look: null,
    selector: 'main > a.cta',
    tag: 'a',
    text: 'Найти',
    rect: { x: 0, y: 0, width: 10, height: 10 },
    component: null,
    html: '',
  },
  page: '/d/x/home.html',
  text: 'выше',
  at: { x: 1, y: 2, right: false },
  style: NO_EDIT,
  port: 5555,
});

describe('design notes', () => {
  beforeEach(() => {
    setScheduler('sync');
    clearDesignNotes(CONV);
  });

  it('keep the notes under the chat they were made beside', () => {
    patchDesignNotes(CONV, (have) => [...have, note('a')]);
    patchDesignNotes(CONV, (have) => [...have, note('b')]);
    expect(designNotesStore.get(CONV)?.map((n) => n.id)).toEqual(['a', 'b']);
    expect(designNotesStore.get('c2' as ConvId)).toBeUndefined();
  });

  it('drop the chat when its last note goes', () => {
    patchDesignNotes(CONV, (have) => [...have, note('a')]);
    patchDesignNotes(CONV, (have) => have.filter((n) => n.id !== 'a'));
    expect(designNotesStore.get(CONV)).toBeUndefined();
  });

  it('let go of all of them when the message went', () => {
    patchDesignNotes(CONV, (have) => [...have, note('a'), note('b')]);
    clearDesignNotes(CONV);
    expect(designNotesStore.get(CONV)).toBeUndefined();
  });
});
