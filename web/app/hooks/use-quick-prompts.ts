import { useMemo } from 'react';
import useSWR from 'swr';
import { homeFetch } from '@/lib/app-path';
import { enabledQuickPrompts, type IQuickPrompt, type IQuickPromptsData } from '@/lib/hl-quick-prompts';

const fetcher = async (url: string): Promise<IQuickPromptsData> => {
  const response = await homeFetch(url);
  if (!response.ok) throw new Error(String(response.status));
  return (await response.json()) as IQuickPromptsData;
};

/** The person's own list lives with the home machine, whichever conversation is on screen. */
export default function useQuickPrompts(): IQuickPrompt[] {
  const { data } = useSWR('/api/quick-prompts', fetcher, { revalidateOnFocus: false });
  return useMemo(() => enabledQuickPrompts(data), [data]);
}
