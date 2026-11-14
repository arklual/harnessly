import { describe, expect, it } from 'vitest';

import { formOfLinks, linksChanged, linksOfForm } from './settings-links';

describe('settings links form', () => {
  it('shows hosts as a line and sends them as a list, without empty fields', () => {
    const form = formOfLinks({
      standDomain: 'preview.example.com',
      gitlabHosts: ['a.example.com', 'b.example.com'],
    });
    expect(form.gitlabHosts).toBe('a.example.com, b.example.com');
    expect(linksOfForm({ ...form, gitlabHosts: ' a.example.com,, b.example.com  c.example.com ' })).toEqual({
      standDomain: 'preview.example.com',
      gitlabHosts: ['a.example.com', 'b.example.com', 'c.example.com'],
    });
  });

  it('clears everything when every field is empty', () => {
    expect(linksOfForm(formOfLinks({}))).toEqual({});
  });

  it('notices a change but not spaces around the same words', () => {
    const saved = formOfLinks({ trackerUrl: 'https://t.example.com' });
    expect(linksChanged({ ...saved, trackerUrl: ' https://t.example.com ' }, saved)).toBe(false);
    expect(linksChanged({ ...saved, boardsUrl: 'https://b.example.com' }, saved)).toBe(true);
  });
});
