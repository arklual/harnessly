import type { IDesignLocation } from '@/lib/hl-conversation-url';

/** A request, from anywhere in the app, to show a place of the design library in the centre of the shell. */
export const OPEN_DESIGN_EVENT = 'hl:open-design-location' as const;

export const openDesignLocation = (location: IDesignLocation): void => {
  window.dispatchEvent(new CustomEvent<IDesignLocation>(OPEN_DESIGN_EVENT, { detail: location }));
};
