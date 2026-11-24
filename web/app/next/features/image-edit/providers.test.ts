import { describe, expect, it } from 'vitest';
import { settleChoice } from '@/next/features/image-edit/provider-picker';
import { BASE_PROVIDERS, creditsText, modelsFor, readProviders } from '@/next/features/image-edit/providers';

const answer = {
  providers: [
    { id: 'codex', name: 'Codex', transparent: true, edit: true },
    { id: 'antigravity', name: 'Antigravity', experimental: true, edit: true },
    {
      id: 'higgsfield',
      name: 'Higgsfield',
      paid: true,
      edit: true,
      models: [
        { id: 'gpt_image_2_6', name: 'GPT Image 2.6', maker: 'OpenAI', refs: true },
        { id: 'z_image', name: 'Z Image', maker: 'Tongyi-MAI' },
        { name: 'no id' },
      ],
    },
    { id: 'midjourney', name: 'Unknown to this page' },
  ],
};

describe('image providers', () => {
  it('reads the machine’s generators and leaves out what it does not know', () => {
    const list = readProviders(answer);
    expect(list.map((p) => p.id)).toEqual(['codex', 'antigravity', 'higgsfield']);
    const hf = list[2];
    expect(hf.paid).toBe(true);
    expect(hf.models.map((m) => m.id)).toEqual(['gpt_image_2_6', 'z_image']);
    expect(modelsFor(hf, true).map((m) => m.id)).toEqual(['gpt_image_2_6']);
  });

  it('falls back to the base list on a strange answer', () => {
    expect(readProviders({ nope: 1 })).toEqual(BASE_PROVIDERS);
    expect(readProviders({ providers: [{ id: 'higgsfield', name: 'H' }] })).toEqual(BASE_PROVIDERS);
  });

  it('settles a choice to a generator and model the machine has', () => {
    const list = readProviders(answer);
    expect(settleChoice(list, { provider: 'higgsfield', model: '' }, false)).toMatchObject({
      provider: 'higgsfield',
      model: 'gpt_image_2_6',
    });
    expect(settleChoice(list, { provider: 'higgsfield', model: 'z_image' }, false).model).toBe('z_image');
    // A change needs a model that takes the picture.
    expect(settleChoice(list, { provider: 'higgsfield', model: 'z_image' }, true).model).toBe(
      'gpt_image_2_6',
    );
    // A generator this machine lacks falls back to the first.
    expect(settleChoice(BASE_PROVIDERS, { provider: 'higgsfield', model: 'x' }, false)).toMatchObject({
      provider: 'codex',
    });
    expect(settleChoice(list, { provider: 'codex', model: 'z_image' }, false).model).toBeUndefined();
  });

  it('writes credits as a person reads them', () => {
    expect(creditsText(2)).toBe('2');
    expect(creditsText(0.15 * 3)).toBe('0.45');
  });
});
