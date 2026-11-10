// One browser tab of the pane: the page in a preview slot of the machine, asked for again on every address and reload.
import { useEffect, useEffectEvent, useRef, useState } from 'react';
import BrowserView from '@/next/features/pane/browser-view';
import { fetchPorts, openPreview, slotNonce, type IListeningPort } from '@/next/core/net/preview';
import { neighboursOf } from '@/next/features/pane/neighbours';
import { slotLeases } from '@/next/features/pane/slot-lease';
import { useTheme } from '@/platform/theme';
import { SIGN_IN, standSignInUrl, useStandSignIn } from '@/next/features/pane/stand-sign-in';
import { useBrowserPins } from '@/next/stores/browser-pins';
import { worthRemembering } from '@/next/features/pane/pins';
import { useMe, personKey, type MeState } from '@/next/stores/me';
import { LOCAL_PERSON } from '@/next/core/persist';
import { useLinkRestored } from '@/next/stores/connection';
import { t } from '@/next/i18n';
import type { Conversation } from '@/next/protocol/v2';
import type { TTab } from '@/next/features/pane/pane-tabs';
import { useFrameBridge } from '@/next/features/pane/use-frame-bridge';
import { siteUrlOf } from '@/next/features/pane/frame-bridge';
import {
  addAnnotation,
  annotationAnchors,
  editAnnotation,
  removeAnnotation,
  type IAnnotation,
} from '@/next/features/pane/annotations';
import type { IAnnotationEditing } from '@/next/features/pane/annotation-layer';
import { FILL_SCREEN, type IBrowserScreen } from '@/next/features/pane/browser-device';
import { parseAddress } from '@/next/features/pane/address';
import { LINK_PLACE, linkPlace, openOutside } from '@/next/features/chat/pane-sites';
import { paneSitesSnapshot } from '@/next/stores/pane-sites';
import { followsRedirect, makeHops } from '@/next/features/pane/redirect-hop';
import { appConfig } from '@/platform/config';
import { addDraftAnnotations, draftsStore, setDraft } from '@/next/stores/drafts';
import { toast } from '@/next/ui/toast';
import { fetchSlotTarget, mintFillToken, VaultError } from '@/next/core/net/vault';
import { useVaultEntries } from '@/next/stores/vault-entries';
import { sameVaultOrigin } from '@/next/features/pane/vault-origin';
import { performVaultLogin, VAULT_OUTCOME, type TVaultOutcome } from '@/next/features/pane/vault-login';
import { useAgentActs } from '@/next/bindings/use-agent-acts';
import { useDomBridge } from '@/next/features/designly/use-dom-bridge';
import { DEV_TAB, type TDevTab } from '@/next/features/designly/devtools-data';
import type { ILivePage } from '@/next/core/net/live-browser';
import type { ILiveControl } from '@/next/features/pane/live-view';
import type { IFrameActed } from '@/next/features/pane/frame-bridge';
import type { IFrameAct } from '@/next/features/pane/use-frame-bridge';

/** How long the marks fade before the list is cleared by «Отправить». */
const SEND_FADE_MS = 220;

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
  // An outside site goes to the machine's live browser; the proxy keeps the machine's servers and the team's
  // own sites, where the page is rewritten, inspected and edited.
  const liveUrl = at !== null && at.target.kind === 'url' ? `${at.target.origin}${at.path}` : null;
  // A site of the team's own that the proxy could not fetch (a reset, a timeout, a certificate) goes to the live
  // browser as well, for as long as this tab stays on it.
  const [liveForced, setLiveForced] = useState<string | null>(null);
  const live =
    liveUrl !== null &&
    (linkPlace(liveUrl, paneSitesSnapshot()) === LINK_PLACE.outside ||
      (at.target.kind === 'url' && liveForced === at.target.origin));
  const [livePageSeen, setLivePage] = useState<ILivePage | null>(null);
  const livePage = live ? livePageSeen : null;
  const livePageRef = useRef<ILivePage | null>(null);
  const liveControl = useRef<ILiveControl | null>(null);
  const liveWaiters = useRef<((loaded: boolean) => void)[]>([]);
  // The tab as the socket's callbacks see it: the latest, whenever they fire.
  const liveTab = useRef({ at, tab, onChange });
  useEffect(() => {
    liveTab.current = { at, tab, onChange };
  });
  const key = at ? `${at.text}|${reloadKey}` : '';
  const [screen, setScreen] = useState<IBrowserScreen>(FILL_SCREEN);
  const [dev, setDev] = useState(false);
  const [devTab, setDevTab] = useState<TDevTab>(DEV_TAB.console);

  useEffect(() => {
    if (!at || live) return;
    let alive = true;
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
        if (!alive) return;
        if (target.kind === 'port' && list !== null && !list.some((p) => p.port === target.port)) {
          failed(t('next.pane.portDown', { port: target.port }));
          return;
        }
        const neighbours = target.kind === 'port' && list !== null ? neighboursOf(list, target.port) : [];
        return openPreview(conv.machineId, target, slotNo, at.path, neighbours).then((opened) => {
          if (alive)
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
      .catch((failure: Error) => alive && failed(failure.message));
    return () => {
      alive = false;
    };
  }, [conv.machineId, conv.cwd, tab.id, at, reloadKey, live]);

  // The live page's moves: its address goes to the bar (in place of the entry, not as history: the machine's browser
  // keeps that), and whoever waits for a load hears of it.
  const onLivePage = (page: ILivePage) => {
    livePageRef.current = page;
    setLivePage(page);
    if (!page.loading) liveWaiters.current.splice(0).forEach((resolve) => resolve(true));
    const now = liveTab.current;
    const next = page.url && page.url !== 'about:blank' ? parseAddress(page.url) : null;
    if (next && now.at && next.text !== now.at.text)
      now.onChange({ ...now.tab, hist: now.tab.hist.map((entry, i) => (i === now.tab.hi ? next : entry)) });
  };
  // An address typed while the page is live goes to the same page.
  useEffect(() => {
    if (!live || liveUrl === null) return;
    const shown = livePageRef.current?.url ?? null;
    // The bar keeps no scheme: an http page's address comes back as https and is the same page.
    const bare = (url: string) => url.replace(/^https?:\/\//i, '');
    if (shown !== null && bare(shown) !== bare(liveUrl)) liveControl.current?.go(liveUrl);
  }, [live, liveUrl]);
  useEffect(() => {
    if (!live) livePageRef.current = null;
  }, [live]);
  // The agent's action on the live page: the machine runs it on its browser; without a socket, no answer, as
  // with a frame that does not answer.
  const liveAct = (message: IFrameAct, timeoutMs: number): Promise<IFrameActed | null> =>
    liveControl.current?.act(message, timeoutMs) ?? Promise.resolve(null);
  const liveHello = (timeoutMs: number) =>
    new Promise<boolean>((resolve) => {
      liveWaiters.current.push(resolve);
      window.setTimeout(() => resolve(false), timeoutMs);
    });

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

  const hops = useRef(makeHops());
  const go = (next: NonNullable<ReturnType<typeof parseAddress>>) => {
    if (worthRemembering(next)) pins?.visit(next.text);
    onChange({ ...tab, hist: [...tab.hist.slice(0, tab.hi + 1), next], hi: tab.hi + 1 });
  };

  // The marks put on the page: they stay while the tab does, and the bridge keeps each on its element.
  const [marks, setMarks] = useState<IAnnotation[]>([]);
  const [editing, setEditing] = useState<IAnnotationEditing | null>(null);
  const [marksHidden, setMarksHidden] = useState(false);
  const [sending, setSending] = useState(false);

  const bridge = useFrameBridge({
    frame,
    origin: slot?.origin ?? null,
    // A click while annotating puts the next mark and opens its comment; picking goes on.
    onPicked: (picked) => {
      const next = addAnnotation(marks, picked);
      if (next.length === marks.length) {
        toast(t('next.pane.annot.limit'), { tone: 'warning' });
        return;
      }
      setMarks(next);
      setMarksHidden(false);
      setEditing({ id: next[next.length - 1].id, fresh: true });
    },
    // The proxy could not fetch the site: the machine's own browser gets a go at it, with the same checks of the
    // address. A port of the machine stays where it is: there is no other way to it.
    onFailed: () => {
      const now = liveTab.current.at;
      if (now?.target.kind === 'url') setLiveForced(now.target.origin);
    },
    // A redirect to another site: ours opens in this tab, any other in the person's browser.
    onOpen: (url) => {
      const next = linkPlace(url, paneSitesSnapshot()) === LINK_PLACE.pane ? parseAddress(url) : null;
      if (next) go(next);
      else openOutside(url);
    },
    // Between our sites (a sign-in and back) the tab follows by itself, in place of the page that redirected.
    // A Teleport application's sign-in, the proxy's own sign-in and back go the same way (redirect-hop).
    onRedirect: (url) => {
      const from = at?.target.kind === 'url' ? at.target.origin : null;
      if (!followsRedirect(url, from, paneSitesSnapshot(), appConfig().standSignInHosts)) return false;
      const next = parseAddress(url);
      if (!next || !hops.current(Date.now())) return false;
      onChange({ ...tab, hist: [...tab.hist.slice(0, tab.hi), next], hi: tab.hi });
      return true;
    },
  });

  // A step that asks for a security key or Touch ID (WebAuthn) cannot happen on the slot's origin: the person takes it
  // in an ordinary tab of their browser, and when they come back to this window the page loads again.
  const [keyWait, setKeyWait] = useState(false);
  const keyStep = keyWait ? SIGN_IN.waiting : bridge.keyStep ? SIGN_IN.asked : SIGN_IN.none;
  const endKeyStep = () => {
    setKeyWait(false);
    setReloadKey((n) => n + 1);
  };
  const openKeyStep = () => {
    if (at?.target.kind !== 'url') return;
    const site = slot?.origin ? siteUrlOf(bridge.url ?? '', slot.origin, at.target.origin) : null;
    openOutside(site ?? `${at.target.origin}${at.path}`);
    setKeyWait(true);
  };
  useEffect(() => {
    if (!keyWait) return;
    const back = () => {
      if (document.visibilityState === 'visible') endKeyStep();
    };
    window.addEventListener('focus', back);
    return () => window.removeEventListener('focus', back);
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
    pageUrl: () => (live ? (livePageRef.current?.url ?? liveUrl) : pageUrl(bridge.url)),
    act: live ? liveAct : bridge.act,
    nextHello: live ? liveHello : bridge.nextHello,
    back: live ? () => liveControl.current?.back() : bridge.back,
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

  // The marks follow their elements: the bridge is told which blocks, again for every new document of the frame.
  const anchorItems = annotationAnchors(marks);
  const anchorsKey = JSON.stringify(anchorItems);
  const tellAnchors = useEffectEvent(() => bridge.post({ type: 'tk:anchors', items: anchorItems }));
  useEffect(() => {
    if (bridge.hellos > 0) tellAnchors();
  }, [anchorsKey, bridge.hellos]);

  // The marks, in words, land in the chat's field; the session is over.
  const closeMarks = () => {
    bridge.endPick();
    setMarks([]);
    setEditing(null);
  };
  const sendMarks = () => {
    // Each mark is a chip in the field, its page address as the person sees it (the draft keeps no frame addresses).
    const added = addDraftAnnotations(
      conv.id,
      marks.map((a) => ({ ...a, url: pageUrl(a.url) ?? a.url })),
    );
    toast(t(added < marks.length ? 'next.pane.annot.limitField' : 'next.pane.annot.sent'));
    // The marks fade where they stand, then the list is cleared and the bar leaves.
    setSending(true);
    window.setTimeout(() => {
      closeMarks();
      setSending(false);
    }, SEND_FADE_MS);
  };
  const dropMark = (id: string) => {
    setMarks((list) => removeAnnotation(list, id));
    setEditing(null);
  };

  return (
    <BrowserView
      address={at?.text ?? ''}
      canBack={live ? (livePage?.canBack ?? false) : tab.hi > 0}
      canForward={live ? (livePage?.canForward ?? false) : tab.hi < tab.hist.length - 1}
      src={live ? null : src}
      error={!live && slot?.key === key ? slot.error : null}
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
      onFrame={(el) => {
        frame.current = el;
      }}
      onFrameLoad={() => {
        bridge.onFrameLoad();
        stand.onFrameLoad();
      }}
      keyStep={keyStep}
      onKeyStep={openKeyStep}
      onKeyStepRefresh={endKeyStep}
      signIn={stand.state}
      onSignIn={stand.start}
      onSignInRefresh={stand.refresh}
      theme={resolvedTheme}
      onGo={go}
      onHistory={(step) => {
        if (live) {
          if (step < 0) liveControl.current?.back();
          else liveControl.current?.forward();
          return;
        }
        onChange({ ...tab, hi: Math.max(0, Math.min(tab.hist.length - 1, tab.hi + step)) });
      }}
      onReload={() => {
        if (live && liveControl.current) liveControl.current.reload();
        else setReloadKey((n) => n + 1);
      }}
      onNewTab={onNewTab}
      picking={slotOrigin !== null && !live ? bridge.picking : null}
      onPick={() => {
        if (dom.inspect) dom.setInspect(false);
        bridge.togglePick();
      }}
      annotations={{
        list: marks,
        editing,
        hidden: marksHidden,
        sending,
        at: bridge.url,
        anchors: bridge.anchors,
        onOpen: (id) => setEditing({ id, fresh: false }),
        onCommit: (id, comment) => {
          setMarks((list) => editAnnotation(list, id, comment));
          setEditing(null);
        },
        onRemove: dropMark,
        // Leaving a mark just put, without a word, takes it back.
        onCancel: () => {
          const open = editing;
          if (open?.fresh && marks.find((a) => a.id === open.id)?.comment === '') dropMark(open.id);
          else setEditing(null);
        },
        onToggleHidden: () => setMarksHidden((on) => !on),
        onClear: () => {
          setMarks([]);
          setEditing(null);
        },
        onSend: sendMarks,
        onClose: closeMarks,
      }}
      screen={screen}
      onScreen={setScreen}
      outsideUrl={live ? (livePage?.url ?? liveUrl) : outsideUrl}
      live={
        live && liveUrl !== null
          ? {
              machine: conv.machineId,
              tab: tab.id,
              url: liveUrl,
              onPage: onLivePage,
              onControl: (control) => {
                liveControl.current = control;
              },
            }
          : null
      }
      vault={
        vaultEntries.length > 0
          ? { entries: vaultEntries, busy: vaultBusy, onLogin: (id) => void vaultLogin(id) }
          : null
      }
      devtools={
        live
          ? null
          : {
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
            }
      }
      agent={{ cursor: agent.cursor }}
    />
  );
};

export default BrowserTab;
