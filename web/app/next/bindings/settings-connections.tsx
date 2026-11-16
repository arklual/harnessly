// Settings → Connections on the machine the person has open: the list read when the page opens, every change taken
// only from the machine's answer (a refused one leaves the list as it was). Its MCP servers are a part of their own
// under it, read on their own: a machine without them still shows its trackers and wikis.
import { useCallback, useEffect, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import {
  checkConnection,
  connectLink,
  detectLink,
  isConnRefusal,
  loadConnections,
  makePrimary,
  removeConnection,
  renameConnection,
  type IConn,
  type TConnOutcome,
} from '@/next/features/connections/api';
import ConnectionsSection from '@/next/features/connections/connections-section';
import {
  checkConnector,
  connectConnector,
  discoverConnector,
  isConnectorRefusal,
  loadConnectors,
  reissueConnector,
  loadConnectorRules,
  setConnectorRule,
  removeConnector,
  type IConnectorConn,
  type IConnectorRuleInput,
  type IConnectorsList,
  type TConnectorOutcome,
} from '@/next/features/connectors/api';
import ConnectorsSection from '@/next/features/connectors/connectors-section';
import {
  addMcp,
  checkMcp,
  enableMcp,
  loadMcp,
  probeMcp,
  removeMcp,
  renameMcp,
  setMcpHeader,
  startMcpSignIn,
  type IMcpList,
  type IMcpServer,
} from '@/next/features/connections/mcp-api';
import { mcpReasonText, needsPerson, withServer } from '@/next/features/connections/mcp-model';
import McpSection, { type TSignInStart } from '@/next/features/connections/mcp-section';
import { reasonText, withConn, withPrimary } from '@/next/features/connections/model';
import type { MachineId } from '@/next/protocol/v2';

type TLoad = { k: 'loading' } | { k: 'failed'; text: string } | { k: 'ready'; conns: readonly IConn[] };

/** An answer as the words of why it was not taken, or the value. */
const outcome = <T,>(res: TConnOutcome<T>): { value: T } | { error: string } => {
  if (!('ok' in res)) return { error: pageText(res) };
  if (isConnRefusal(res.value)) return { error: reasonText(res.value.refused) };
  return { value: res.value };
};

type TMcpLoad =
  | { k: 'loading' }
  | { k: 'failed'; text: string }
  /** A machine whose runtime has no MCP routes yet: the part is not shown. */
  | { k: 'absent' }
  | { k: 'ready'; mcp: IMcpList };

/** An answer of the MCP routes as the words of why it was not taken, or the value. */
const mcpOutcome = <T,>(res: TConnOutcome<T>): { value: T } | { error: string } => {
  if (!('ok' in res)) return { error: pageText(res) };
  if (isConnRefusal(res.value)) return { error: mcpReasonText(res.value.refused) };
  return { value: res.value };
};

/** The part of the page under trackers and wikis: the machine's MCP servers and the catalog. */
const McpPart = ({ machine }: { machine: MachineId }) => {
  const [state, setState] = useState<TMcpLoad>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    void loadMcp(machine).then((res) => {
      if (!live) return;
      if ('r' in res && res.r === 'failed' && res.code === 'http' && res.status === 404) {
        setState((cur) => (cur.k === 'ready' ? cur : { k: 'absent' }));
        return;
      }
      const got = mcpOutcome(res);
      if ('value' in got) setState({ k: 'ready', mcp: got.value });
      else setState((cur) => (cur.k === 'ready' ? cur : { k: 'failed', text: got.error }));
    });
    return () => {
      live = false;
    };
  }, [machine, attempt]);

  const retry = useCallback(() => {
    setState({ k: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  /** Reads the list again in place: a failed read keeps what the page shows. */
  const refresh = useCallback(async () => {
    const got = mcpOutcome(await loadMcp(machine));
    if ('value' in got) setState({ k: 'ready', mcp: got.value });
  }, [machine]);

  const servers = state.k === 'ready' ? state.mcp.servers : null;
  const waitsForPerson = servers !== null && servers.some(needsPerson);
  // A sign-in finished in a window the page lost track of (a new tab, a cut opener) shows when the person is back.
  useEffect(() => {
    if (!waitsForPerson) return;
    const onFocus = () => void refresh();
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, [waitsForPerson, refresh]);

  /** Takes the machine's answer into the list. */
  const change = (next: (servers: readonly IMcpServer[]) => readonly IMcpServer[]) =>
    setState((cur) =>
      cur.k === 'ready' ? { k: 'ready', mcp: { ...cur.mcp, servers: next(cur.mcp.servers) } } : cur,
    );

  /** One server's change: the answer replaces its row. */
  const replaced = async (res: Promise<TConnOutcome<IMcpServer>>): Promise<string | null> => {
    const got = mcpOutcome(await res);
    if ('error' in got) return got.error;
    change((all) => withServer(all, got.value));
    return null;
  };

  const signIn = useCallback(
    async (id: string): Promise<TSignInStart> => {
      const got = mcpOutcome(
        await startMcpSignIn(
          machine,
          id,
          window.location.origin,
          `${window.location.pathname}${window.location.search}`,
        ),
      );
      return 'error' in got ? { error: got.error } : { authUrl: got.value };
    },
    [machine],
  );

  if (state.k === 'absent') return null;
  return (
    <McpSection
      mcp={state.k === 'ready' ? state.mcp : null}
      failed={state.k === 'failed' ? { reason: state.text, onRetry: retry } : null}
      onProbe={async (url) => {
        const got = mcpOutcome(await probeMcp(machine, url));
        return 'error' in got ? { error: got.error } : { probe: got.value };
      }}
      onAdd={async (input) => {
        const got = mcpOutcome(await addMcp(machine, input));
        if ('error' in got) return { error: got.error };
        change((all) => withServer(all, got.value.server));
        return got.value;
      }}
      onSignIn={signIn}
      onRefresh={refresh}
      onHeader={(id, header) => replaced(setMcpHeader(machine, id, header))}
      onCheck={(id) => replaced(checkMcp(machine, id))}
      onRename={(id, name) => replaced(renameMcp(machine, id, name))}
      onEnable={(id, enabled) => replaced(enableMcp(machine, id, enabled))}
      onRemove={async (id) => {
        const got = mcpOutcome(await removeMcp(machine, id));
        if ('error' in got) return got.error;
        change((all) => all.filter((server) => server.id !== id));
        return null;
      }}
    />
  );
};

/** How often the connectors are read again while a pairing step is open. */
const STEP_POLL_MS = 3_000;

type TConnectorsLoad =
  | { k: 'loading' }
  | { k: 'failed'; text: string }
  /** A machine whose runtime has no connector routes yet: the part is not shown. */
  | { k: 'absent' }
  | { k: 'ready'; list: IConnectorsList };

/** An answer of the connector routes as the words of why it was not taken, or the value. */
const connectorOutcome = <T,>(res: TConnectorOutcome<T>): { value: T } | { error: string } => {
  if (!('ok' in res)) return { error: pageText(res) };
  if (isConnectorRefusal(res.value)) return { error: res.value.message || reasonText(res.value.refused) };
  return { value: res.value };
};

const withConnector = (all: readonly IConnectorConn[], conn: IConnectorConn): readonly IConnectorConn[] =>
  all.some((c) => c.id === conn.id) ? all.map((c) => (c.id === conn.id ? conn : c)) : [...all, conn];

/** The part of the page with the person's mail, calendars, feeds: the connections and the catalog. */
const ConnectorsPart = ({ machine }: { machine: MachineId }) => {
  const [state, setState] = useState<TConnectorsLoad>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    void loadConnectors(machine).then((res) => {
      if (!live) return;
      if ('r' in res && res.r === 'failed' && res.code === 'http' && res.status === 404) {
        setState((cur) => (cur.k === 'ready' ? cur : { k: 'absent' }));
        return;
      }
      const got = connectorOutcome(res);
      if ('value' in got) setState({ k: 'ready', list: got.value });
      else setState((cur) => (cur.k === 'ready' ? cur : { k: 'failed', text: got.error }));
    });
    return () => {
      live = false;
    };
  }, [machine, attempt]);

  const retry = useCallback(() => {
    setState({ k: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  // While a connection waits for a step of the person (a bot's pairing link), the list is read again every few
  // seconds: the step goes away by itself once the bot got the code. «Check» stays the way to do it at once.
  const stepOpen = state.k === 'ready' && state.list.connections.some((c) => c.next !== null);
  useEffect(() => {
    if (!stepOpen) return undefined;
    let live = true;
    const timer = window.setInterval(() => {
      void loadConnectors(machine).then((res) => {
        if (!live) return;
        const got = connectorOutcome(res);
        if ('value' in got) setState({ k: 'ready', list: got.value });
      });
    }, STEP_POLL_MS);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, [machine, stepOpen]);

  const change = (next: (conns: readonly IConnectorConn[]) => readonly IConnectorConn[]) =>
    setState((cur) =>
      cur.k === 'ready'
        ? { k: 'ready', list: { ...cur.list, connections: next(cur.list.connections) } }
        : cur,
    );

  const loadRules = useCallback(
    async (id: string) => {
      const got = connectorOutcome(await loadConnectorRules(machine, id));
      return 'error' in got ? got.error : got.value;
    },
    [machine],
  );
  const setRule = useCallback(
    async (id: string, input: IConnectorRuleInput) => {
      const got = connectorOutcome(await setConnectorRule(machine, id, input));
      return 'error' in got ? got.error : got.value;
    },
    [machine],
  );

  const replaced = async (res: Promise<TConnectorOutcome<IConnectorConn>>): Promise<string | null> => {
    const got = connectorOutcome(await res);
    if ('error' in got) return got.error;
    change((all) => withConnector(all, got.value));
    return null;
  };

  if (state.k === 'absent') return null;
  return (
    <ConnectorsSection
      ruled={state.k === 'ready' ? state.list.ruled : undefined}
      onRules={loadRules}
      onSetRule={setRule}
      catalog={state.k === 'ready' ? state.list.catalog : null}
      connections={state.k === 'ready' ? state.list.connections : null}
      failed={state.k === 'failed' ? { reason: state.text, onRetry: retry } : null}
      onConnect={(input) => replaced(connectConnector(machine, input))}
      onDiscover={async (input) => {
        const got = connectorOutcome(await discoverConnector(machine, input));
        return 'error' in got ? got.error : got.value;
      }}
      onCheck={(id) => replaced(checkConnector(machine, id))}
      onReissue={(id) => replaced(reissueConnector(machine, id))}
      onRemove={async (id) => {
        const got = connectorOutcome(await removeConnector(machine, id));
        if ('error' in got) return got.error;
        change((all) => all.filter((c) => c.id !== id));
        return null;
      }}
    />
  );
};

export const ConnectionsPage = ({ machine, start }: { machine: MachineId; start?: string }) => {
  const [state, setState] = useState<TLoad>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    void loadConnections(machine).then((res) => {
      if (!live) return;
      const got = outcome(res);
      if ('value' in got) setState({ k: 'ready', conns: got.value });
      else setState((cur) => (cur.k === 'ready' ? cur : { k: 'failed', text: got.error }));
    });
    return () => {
      live = false;
    };
  }, [machine, attempt]);

  const retry = useCallback(() => {
    setState({ k: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  /** Takes the machine's answer into the list. */
  const change = (next: (conns: readonly IConn[]) => readonly IConn[]) =>
    setState((cur) => (cur.k === 'ready' ? { k: 'ready', conns: next(cur.conns) } : cur));

  /** One connection's change: the answer replaces its row. */
  const replaced = async (res: Promise<TConnOutcome<IConn>>): Promise<string | null> => {
    const got = outcome(await res);
    if ('error' in got) return got.error;
    change((conns) => withConn(conns, got.value));
    return null;
  };

  return (
    <>
      <ConnectionsSection
        connections={state.k === 'ready' ? state.conns : null}
        failed={state.k === 'failed' ? { reason: state.text, onRetry: retry } : null}
        start={start}
        onDetect={async (url) => {
          const got = outcome(await detectLink(machine, url));
          return 'error' in got ? { error: got.error } : { detection: got.value };
        }}
        onConnect={(input) => replaced(connectLink(machine, input))}
        onCheck={(id) => replaced(checkConnection(machine, id))}
        onRename={(id, name) => replaced(renameConnection(machine, id, name))}
        onPrimary={async (id) => {
          const got = outcome(await makePrimary(machine, id));
          if ('error' in got) return got.error;
          change((conns) => withPrimary(conns, got.value));
          return null;
        }}
        onRemove={async (id) => {
          const got = outcome(await removeConnection(machine, id));
          if ('error' in got) return got.error;
          change((conns) => conns.filter((conn) => conn.id !== id));
          return null;
        }}
      />
      <ConnectorsPart machine={machine} />
      <McpPart machine={machine} />
    </>
  );
};
