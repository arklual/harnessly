// One browser tab of the pane: the page in a preview slot of the machine, asked for again on every address and reload.
import { useEffect, useEffectEvent, useRef, useState } from 'react';
import BrowserView from '@/next/features/pane/browser-view';
import { fetchPorts, openPreview, slotNonce, type IListeningPort } from '@/next/core/net/preview';
import { neighboursOf } from '@/next/features/pane/neighbours';
import { slotLeases } from '@/next/features/pane/slot-lease';
import { useTheme } from '@/platform/theme';
import { standSignInUrl, useStandSignIn } from '@/next/features/pane/stand-sign-in';
import { useBrowserPins } from '@/next/stores/browser-pins';
import { worthRemembering } from '@/next/features/pane/pins';
import { useMe, personKey, type MeState } from '@/next/stores/me';
import { LOCAL_PERSON } from '@/next/core/persist';
import { useLinkRestored } from '@/next/stores/connection';
import { t } from '@/next/i18n';
import type { Conversation } from '@/next/protocol/v2';
import type { TTab } from '@/next/features/pane/pane-tabs';
import { useFrameBridge } from '@/next/features/pane/use-frame-bridge';
import { pickedWords, siteUrlOf } from '@/next/features/pane/frame-bridge';
import { FILL_SCREEN, type IBrowserScreen } from '@/next/features/pane/browser-device';
import { parseAddress } from '@/next/features/pane/address';
import { LINK_PLACE, linkPlace, openOutside } from '@/next/features/chat/pane-sites';
import { paneSitesSnapshot } from '@/next/stores/pane-sites';
import { followsRedirect, makeHops } from '@/next/features/pane/redirect-hop';
import { appConfig } from '@/platform/config';
import { draftsStore, setDraft } from '@/next/stores/drafts';
import { toast } from '@/next/ui/toast';
import { fetchSlotTarget, mintFillToken, VaultError } from '@/next/core/net/vault';
import { useVaultEntries } from '@/next/stores/vault-entries';
import { sameVaultOrigin } from '@/next/features/pane/vault-origin';
import { performVaultLogin, VAULT_OUTCOME, type TVaultOutcome } from '@/next/features/pane/vault-login';
import { useAgentActs } from '@/next/bindings/use-agent-acts';
import { useDomBridge } from '@/next/features/designly/use-dom-bridge';
import { DEV_TAB, type TDevTab } from '@/next/features/designly/devtools-data';

interface IBrowserTabProps {
  conv: Conversation;
  tab: Extract<TTab, { kind: 'browser' }>;
  onChange: (tab: Extract<TTab, { kind: 'browser' }>) => void;
  onNewTab: () => void;
}

/** Whose pins these are: the account's, the local person's when nobody signs in; unknown while it loads or failed. */
const personOf = (me: MeState): string | null => {
  if (me.k === 'ready') return personKey(me.me);
  return me.k === 'signed-out' ? LOCAL_PERSON : null;
};

/** One browser tab: the page in its slot, asked for again on every address and reload. */
/** What a login came to, in words. */
const tellVault = (outcome: TVaultOutcome, login: string): void => {
  switch (outcome.kind) {
    case VAULT_OUTCOME.loggedIn:
      toast(t('next.pane.vault.loggedIn', { login }), { tone: 'success' });
      return;
    case VAULT_OUTCOME.stillLogin:
      toast(t('next.pane.vault.stillLogin'), { tone: 'warning' });
      return;
    case VAULT_OUTCOME.unverified:
      toast(t('next.pane.vault.unverified'));
      return;
    case VAULT_OUTCOME.filled:
      toast(t('next.pane.vault.filled'));
      return;
    case VAULT_OUTCOME.refused:
      toast(t('next.pane.vault.refused', { reason: t(`next.pane.vault.reason.${outcome.reason}`) }), {
        tone: 'error',
      });
  }
};

const BrowserTab = ({ conv, tab, onChange, onNewTab }: IBrowserTabProps) => {
  const [reloadKey, setReloadKey] = useState(0);
  // A reload asked from outside the tab (the agent published the page again) is one more press of «Обновить».
  const asked = tab.reload ?? 0;
  const [heard, setHeard] = useState(asked);
  if (asked !== heard) {
    setHeard(asked);
    setReloadKey((n) => n + 1);
  }
  const [slot, setSlot] = useState<{
    key: string;
    src: string | null;
    error: string | null;
    slot: number | null;
    nonce: string | null;
    origin: string | null;
  } | null>(null);
  const [ports, setPorts] = useState<IListeningPort[] | null>(null);
  const [portsError, setPortsError] = useState<string | null>(null);
  const [portsAttempt, setPortsAttempt] = useState(0);
  const restored = useLinkRestored(conv.machineId);
  const { resolvedTheme } = useTheme();
  const me = useMe();
  const person = personOf(me);
  const pins = useBrowserPins(person);
  const at = tab.hist[tab.hi] ?? null;
  const frame = useRef<HTMLIFrameElement>(null);
  const key = at ? `${at.text}|${reloadKey}` : '';
  const [screen, setScreen] = useState<IBrowserScreen>(FILL_SCREEN);
  const [dev, setDev] = useState(false);
  const [devTab, setDevTab] = useState<TDevTab>(DEV_TAB.console);

  useEffect(() => {
    if (!at) return;
    let live = true;
    const { target } = at;
    const asked = `${at.text}|${reloadKey}`;
    const failed = (error: string) =>
      setSlot({ key: asked, src: null, error, slot: null, nonce: null, origin: null });
    // Every tab holds a slot of its own: a second tab on the same one would take the first one's page over.
    const slotNo = slotLeases.lease(target.kind === 'port' ? 'port' : 'site', tab.id);
    // The machine's servers say whether a port is down and which other servers the page leans on (an MFE's remotes).
    const listing =
      target.kind === 'port'
        ? fetchPorts(conv.machineId, [conv.cwd]).then(
            (list) => list,
            () => null,
          )
        : Promise.resolve(null);
    listing
      .then((list) => {
        if (!live) return;
        if (target.kind === 'port' && list !== null && !list.some((p) => p.port === target.port)) {
          failed(t('next.pane.portDown', { port: target.port }));
          return;
        }
        const neighbours = target.kind === 'port' && list !== null ? neighboursOf(list, target.port) : [];
        return openPreview(conv.machineId, target, slotNo, at.path, neighbours).then((opened) => {
          if (live)
            setSlot({
              key: asked,
              src: opened.enterUrl,
              error: null,
              slot: slotNo,
              nonce: opened.nonce,
              origin: opened.origin,
            });
        });
      })
      .catch((failure: Error) => live && failed(failure.message));
    return () => {
      live = false;
    };
  }, [conv.machineId, conv.cwd, tab.id, at, reloadKey]);

  // The tab's slot went to a tab used less recently; it asks again, and gets another.
  useEffect(
    () =>
      slotLeases.onTaken((taken) => {
        if (taken === tab.id) setReloadKey((n) => n + 1);
      }),
    [tab.id],
  );

  // Another window (or the older panels) opened the slot: its cookie is not ours any more.
  const heldSlot = slot?.slot ?? null;
  const heldNonce = slot?.nonce ?? null;
  useEffect(() => {
    if (heldSlot === null) return;
    const check = () => {
      const now = slotNonce(heldSlot);
      if (now !== null && now !== heldNonce) setReloadKey((n) => n + 1);
    };
    window.addEventListener('storage', check);
    return () => window.removeEventListener('storage', check);
  }, [heldSlot, heldNonce]);

  // The servers are asked for on every tab: a new tab lists them, the omnibox suggests them.
  useEffect(() => {
    const abort = new AbortController();
    fetchPorts(conv.machineId, [conv.cwd], abort.signal).then(
      (list) => {
        const shown = list.filter((p) => p.responding && p.kind === 'site');
        setPorts([...shown.filter((p) => p.group === 'chat'), ...shown.filter((p) => p.group !== 'chat')]);
        setPortsError(null);
      },
      (failure: Error) => {
        if (!abort.signal.aborted) setPortsError(failure.message);
      },
    );
    return () => abort.abort();
  }, [conv.machineId, conv.cwd, portsAttempt, restored]);

  const stand = useStandSignIn({
    frame,
    origin: slot?.origin ?? null,
    signInUrl: at?.target.kind === 'url' ? standSignInUrl(at.target.origin, at.path) : null,
    onReload: () => setReloadKey((n) => n + 1),
  });

  const go = (next: NonNullable<ReturnType<typeof parseAddress>>) => {
    if (worthRemembering(next)) pins?.visit(next.text);
    onChange({ ...tab, hist: [...tab.hist.slice(0, tab.hi + 1), next], hi: tab.hi + 1 });
  };

  const hops = useRef(makeHops());
  const bridge = useFrameBridge({
    frame,
    origin: slot?.origin ?? null,
    // A redirect to another site: ours opens in this tab, any other in the person's browser.
    onOpen: (url) => {
      const next = linkPlace(url, paneSitesSnapshot()) === LINK_PLACE.pane ? parseAddress(url) : null;
      if (next) go(next);
      else openOutside(url);
    },
    // Between our sites (a sign-in and back) the tab follows by itself, in place of the page that redirected.
    onRedirect: (url) => {
      const from = at?.target.kind === 'url' ? at.target.origin : null;
      if (!followsRedirect(url, from, paneSitesSnapshot(), appConfig().standSignInHosts)) return;
      const next = parseAddress(url);
      if (!next || !hops.current(Date.now())) return;
      onChange({ ...tab, hist: [...tab.hist.slice(0, tab.hi), next], hi: tab.hi });
    },
  });

  // The page's own address: the site's origin in place of the slot's, as far as the page has gone; a server of the
  // machine has none outside.
  const siteOrigin = at?.target.kind === 'url' ? at.target.origin : null;
  const slotOrigin = slot?.key === key ? slot.origin : null;
  // The safe's passwords for this site; a login is the person's pick, and its token is minted for this document only.
  const vaultAll = useVaultEntries();
  const [vaultBusy, setVaultBusy] = useState(false);
  const vaultEntries =
    siteOrigin !== null && slotOrigin !== null && vaultAll !== null
      ? vaultAll.filter((entry) => sameVaultOrigin(entry.origin, siteOrigin))
      : [];
  const vaultLogin = async (id: string) => {
    const entry = vaultEntries.find((one) => one.id === id);
    if (!entry || siteOrigin === null || slotOrigin === null || vaultBusy) return;
    setVaultBusy(true);
    try {
      tellVault(
        await performVaultLogin(siteOrigin, {
          target: siteOrigin,
          slot: slotOrigin,
          nonce: bridge.nonceNow(),
          slotTarget: () => fetchSlotTarget(slotOrigin),
          mint: (fill) => mintFillToken(entry.id, siteOrigin, fill, conv.machineId),
          send: bridge.post,
          next: bridge.nextMessage,
        }),
        entry.login,
      );
    } catch (failure) {
      toast(failure instanceof VaultError ? failure.message : t('next.pane.vault.noAnswer'), {
        tone: 'error',
      });
    } finally {
      setVaultBusy(false);
    }
  };

  const pageUrl = (from: string | null): string | null => {
    if (!at) return null;
    if (from !== null && slotOrigin !== null) {
      const own = siteUrlOf(from, slotOrigin, siteOrigin ?? `http://${at.text.split('/')[0]}`);
      if (own !== null) return own;
    }
    return siteOrigin !== null ? `${siteOrigin}${at.path}` : `http://${at.text}`;
  };
  const outsideUrl = siteOrigin !== null ? pageUrl(bridge.url) : null;

  // DevTools' «Elements»: the page's tree through its bridge, which answers once it is asked to (a design's own script
  // answers in the design's view instead, so the bridge keeps quiet there).
  const src = slot?.key === key ? slot.src : null;
  const wantDom = dev && devTab === DEV_TAB.elements && slotOrigin !== null;
  const dom = useDomBridge(frame, src, reloadKey, wantDom);
  const { hellos } = bridge;
  const domOn = useEffectEvent(() => bridge.post({ type: 'tk:dom-on' }));
  useEffect(() => {
    if (wantDom && hellos > 0) domOn();
  }, [wantDom, hellos]);
  const toggleInspect = () => {
    if (dom.inspect) {
      dom.setInspect(false);
      return;
    }
    if (bridge.picking) bridge.endPick();
    setDev(true);
    setDevTab(DEV_TAB.elements);
    dom.setInspect(true);
  };

  // The chat's agent acts on the page while the tab is in view.
  const agent = useAgentActs({
    machine: conv.machineId,
    conv: conv.id,
    pageUrl: () => pageUrl(bridge.url),
    act: bridge.act,
    nextHello: bridge.nextHello,
    back: bridge.back,
    go,
  });

  // What goes to the chat lands in its field, after what is typed there.
  const toChat = (words: string) => {
    const have = draftsStore.get(conv.id);
    const text = have?.text ?? '';
    setDraft(conv.id, {
      text: text.trim() === '' ? words : `${text.trimEnd()}\n\n${words}`,
      attachments: have?.attachments ?? [],
    });
  };

  // A comment on a picked element lands in the chat's field.
  const sendPicked = (comment: string) => {
    const picked = bridge.picked;
    if (!picked) return;
    const words = pickedWords(picked, pageUrl(picked.url) ?? picked.url, comment, {
      element: t('next.pane.pick.words.element'),
      component: t('next.pane.pick.words.component'),
      comment: t('next.pane.pick.words.comment'),
    });
    toChat(words);
    bridge.endPick();
    toast(t('next.pane.pick.sent'));
  };

  return (
    <BrowserView
      address={at?.text ?? ''}
      canBack={tab.hi > 0}
      canForward={tab.hi < tab.hist.length - 1}
      src={src}
      error={slot?.key === key ? slot.error : null}
      ports={ports}
      portsError={ports === null ? portsError : null}
      onRetryPorts={() => {
        setPortsError(null);
        setPortsAttempt((n) => n + 1);
      }}
      onWantPorts={() => setPortsAttempt((n) => n + 1)}
      title={at?.title ?? null}
      pins={pins?.pins ?? null}
      visits={pins?.visits ?? []}
      onPin={(pin) => pins?.pin(pin)}
      onUnpin={(text) => pins?.unpin(text)}
      reloadKey={reloadKey}
      onFrameLoad={(el) => {
        frame.current = el;
        bridge.onFrameLoad();
        stand.onFrameLoad();
      }}
      signIn={stand.state}
      onSignIn={stand.start}
      onSignInRefresh={stand.refresh}
      theme={resolvedTheme}
      onGo={go}
      onHistory={(step) =>
        onChange({ ...tab, hi: Math.max(0, Math.min(tab.hist.length - 1, tab.hi + step)) })
      }
      onReload={() => setReloadKey((n) => n + 1)}
      onNewTab={onNewTab}
      picking={slotOrigin !== null ? bridge.picking : null}
      onPick={() => {
        if (dom.inspect) dom.setInspect(false);
        bridge.togglePick();
      }}
      picked={bridge.picked}
      onPickSend={sendPicked}
      onPickCancel={bridge.endPick}
      screen={screen}
      onScreen={setScreen}
      outsideUrl={outsideUrl}
      vault={
        vaultEntries.length > 0
          ? { entries: vaultEntries, busy: vaultBusy, onLogin: (id) => void vaultLogin(id) }
          : null
      }
      devtools={{
        machine: conv.machineId,
        slot: slotOrigin,
        open: dev,
        onToggle: () => setDev((on) => !on),
        tab: devTab,
        onTab: setDevTab,
        dom,
        inspect: { on: dom.inspect, disabled: slotOrigin === null, onToggle: toggleInspect },
        onSend: (words) => {
          toChat(words);
          toast(t('next.pane.pick.sent'));
        },
      }}
      agent={{
        cursor: agent.cursor,
        asking: agent.asking,
        working: agent.working,
        paused: agent.paused,
        onAllow: agent.allow,
        onDeny: agent.deny,
        onStop: agent.stop,
        onResume: agent.resume,
      }}
    />
  );
};

export default BrowserTab;
