import { describe, expect, it } from 'vitest';
import { SMART_KIND } from '@/next/features/smart-notice/smart-notice';
import { parseNotices, type INotice } from '@/next/stores/notices';
import { itemsOfNotices } from './smart-notice';

const access: INotice = {
  key: 'access-request:r1',
  chatId: '',
  kind: 'access',
  icon: 'key-round',
  title: 'Ann asks to comment',
  text: 'please',
  at: 5,
  link: '/m/m1/designly/p?access=requests',
};

// a request for access is a notice with no chat that leads to the page of the requests.
describe('access notices', () => {
  it('survive the kept list with their link', () => {
    expect(parseNotices(JSON.stringify([access]))).toEqual([access]);
    expect(parseNotices(JSON.stringify([{ ...access, link: 5 }]))).toEqual([]);
  });

  it('show as an info card that opens the page', () => {
    const items = itemsOfNotices([access], new Map(), new Set([access.key]));
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ kind: SMART_KIND.info, href: access.link, title: access.title });
  });
});
