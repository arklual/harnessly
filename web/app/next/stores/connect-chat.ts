// The chat whose «Где работает…» was asked for from a menu: the chat view opens its sheet, now or once it is there.
import { useEffect, useRef } from 'react';
import { requestUi, UI_REQUEST, useUiRequest } from '@/next/stores/ui-requests';

let connectFor: string | null = null;

/** Asks the chat `id` to open its «Где работает…» sheet, now or as soon as it is on the page. */
export const requestConnect = (id: string): void => {
  connectFor = id;
  requestUi(UI_REQUEST.connect);
};

/** Runs `handler` when the chat `id` is asked where it runs, also when it was asked before it mounted. */
export const useConnectRequest = (id: string, handler: () => void): void => {
  const latest = useRef(handler);
  useEffect(() => {
    latest.current = handler;
  });
  const take = () => {
    if (connectFor !== id) return;
    connectFor = null;
    latest.current();
  };
  useUiRequest(UI_REQUEST.connect, take);
  useEffect(take, [id]);
};
