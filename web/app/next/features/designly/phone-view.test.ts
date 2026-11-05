import { describe, expect, it } from 'vitest';
import { parsePhoneView, PHONE_VIEW_DEFAULT } from '@/next/features/designly/phone-view';

describe('how a phone is drawn', () => {
  it('reads what was kept', () => {
    expect(parsePhoneView({ frame: false, status: false, touch: true })).toEqual({
      frame: false,
      status: false,
      touch: true,
    });
  });

  it('keeps the default for what is missing or broken', () => {
    expect(parsePhoneView({ frame: 'no', touch: true })).toEqual({ ...PHONE_VIEW_DEFAULT, touch: true });
    expect(parsePhoneView(null)).toEqual(PHONE_VIEW_DEFAULT);
    expect(parsePhoneView([1])).toEqual(PHONE_VIEW_DEFAULT);
  });
});
