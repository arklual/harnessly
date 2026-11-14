// Ask an agent the way a search launcher asks a question: the prompt is typed in the search bar, Enter sends it to the
// project chosen in the bar's dropdown (remembered), and the answer shows right there while the agent works.
import {
  Action,
  ActionPanel,
  closeMainWindow,
  Color,
  Detail,
  Form,
  Icon,
  Keyboard,
  LaunchProps,
  List,
  LocalStorage,
  open,
  showToast,
  Toast,
  useNavigation,
} from "@raycast/api";
import { useCachedPromise } from "@raycast/utils";
import { useEffect, useRef, useState } from "react";
import { createChat, describeError, getChat, listAgents, listProjects, MachineAgent, Project, stopChat } from "./api";
import { connOf, Instance, loadInstances } from "./instances";
import { ReplyForm } from "./reply";

type Row = { instance: Instance; project: Project };
type Loaded = { instances: Instance[]; rows: Row[]; errors: string[] };

async function loadAll(): Promise<Loaded> {
  const instances = await loadInstances();
  const rows: Row[] = [];
  const errors: string[] = [];
  await Promise.all(
    instances.map(async (instance) => {
      try {
        const r = await listProjects(connOf(instance));
        for (const project of r.projects) rows.push({ instance, project });
      } catch (e) {
        errors.push(`${instance.name}: ${describeError(e)}`);
      }
    }),
  );
  rows.sort((a, b) => b.project.lastUsedAt - a.project.lastUsedAt);
  return { instances, rows, errors };
}

const rowKey = (r: Row) => `${r.instance.id}|${r.project.machineId}|${r.project.projectId || r.project.cwd}`;
const projectName = (r: Row) => r.project.name || r.project.cwd.split("/").pop() || r.project.cwd;

const PROJECT_KEY = "ask.project";
const PROVIDER_KEY = "ask.provider";
const AGENT_KEY = "ask.agent";

/** The agent a new chat runs: the CLI and its model ("" = the CLI's default). */
type Agent = { provider: string; model: string };

const machineKey = (r: Row) => `${r.instance.id}|${r.project.machineId}`;

type Sent = { row: Row; prompt: string; chatId?: string; url?: string; queued: boolean };

type Options = { worktree?: boolean; browser?: boolean };

async function send(row: Row, text: string, agent: Agent, opts: Options): Promise<Sent | undefined> {
  const toast = await showToast({ style: Toast.Style.Animated, title: `Asking ${projectName(row)}...` });
  try {
    const r = await createChat(connOf(row.instance), {
      machineId: row.project.machineId,
      ...(row.project.projectId ? { projectId: row.project.projectId } : { cwd: row.project.cwd }),
      provider: agent.provider || undefined,
      model: agent.model || undefined,
      worktree: opts.worktree || undefined,
      text,
    });
    if (r.delivery === "failed") {
      toast.style = Toast.Style.Failure;
      toast.title = "Could not start the chat";
      toast.message = r.error;
      return undefined;
    }
    await LocalStorage.setItem(PROJECT_KEY, rowKey(row));
    const queued = r.delivery === "queued";
    toast.style = Toast.Style.Success;
    toast.title = queued ? "Queued" : "Sent";
    toast.message = queued ? "The machine is off; the chat starts when it connects" : undefined;
    if (opts.browser && r.url) {
      await open(r.url);
      await closeMainWindow();
    }
    return { row, prompt: text, chatId: r.id, url: r.url, queued };
  } catch (e) {
    toast.style = Toast.Style.Failure;
    toast.title = "Could not start the chat";
    toast.message = describeError(e);
    return undefined;
  }
}

export function AskView(props: { prompt?: string; instanceId?: string; machineId?: string; projectId?: string }) {
  const { data, isLoading } = useCachedPromise(loadAll);
  const [text, setText] = useState(props.prompt ?? "");
  const [key, setKey] = useState<string | undefined>();
  const [agent, setAgent] = useState<Agent>({ provider: "claude", model: "" });
  const [sent, setSent] = useState<Sent | undefined>();
  const [sending, setSending] = useState(false);
  const autoSent = useRef(false);

  useEffect(() => {
    void (async () => {
      const [p, v, a] = await Promise.all([
        LocalStorage.getItem<string>(PROJECT_KEY),
        LocalStorage.getItem<string>(PROVIDER_KEY),
        LocalStorage.getItem<string>(AGENT_KEY),
      ]);
      if (p && !props.instanceId) setKey((k) => k ?? p);
      try {
        if (a) setAgent(JSON.parse(a) as Agent);
        else if (v) setAgent({ provider: v, model: "" });
      } catch {
        // a broken value: keep the default agent
      }
    })();
  }, []);

  const rows = data?.rows ?? [];
  const multi = (data?.instances.length ?? 0) > 1;
  const preselected = props.instanceId
    ? rows.find(
        (r) =>
          r.instance.id === props.instanceId &&
          r.project.machineId === props.machineId &&
          (!props.projectId || r.project.projectId === props.projectId),
      )
    : undefined;
  const current = rows.find((r) => rowKey(r) === key) ?? preselected ?? rows[0];
  const others = rows.filter((r) => r !== current).slice(0, 8);

  // The CLIs and models of the chosen machine, as the machine reports them; an offline machine says nothing.
  const agents = useCachedPromise(
    async (inst: Instance | undefined, machineId: string | undefined) =>
      inst && machineId ? (await listAgents(connOf(inst), machineId)).agents : [],
    [current?.instance, current?.project.machineId],
    { execute: !!current?.project.online, keepPreviousData: false, onError: () => undefined },
  );
  const offered: MachineAgent[] = (agents.data ?? []).filter((a) => a.status !== "missing");
  const fallback = (current?.project.providers?.length ? current.project.providers : ["claude", "codex"]).map(
    (provider) => ({ provider, status: "", models: [] }) as MachineAgent,
  );
  const menu = offered.length ? offered : fallback;
  // A model the machine does not have falls back to its default.
  const machineAgent = menu.find((a) => a.provider === agent.provider);
  const effective: Agent =
    machineAgent && agent.model && machineAgent.models.length && !machineAgent.models.some((m) => m.id === agent.model)
      ? { provider: agent.provider, model: "" }
      : agent;
  const modelLabel = (a: Agent) => {
    const m = menu.find((x) => x.provider === a.provider)?.models.find((x) => x.id === a.model);
    return a.model ? `${a.provider} · ${m?.label || a.model}` : a.provider;
  };

  // One entry per machine: switching keeps the project when the machine has a folder of the same name.
  const machines: Row[] = [];
  for (const r of rows) if (!machines.some((m) => machineKey(m) === machineKey(r))) machines.push(r);
  const onMachine = (m: Row): Row =>
    (current && rows.find((r) => machineKey(r) === machineKey(m) && projectName(r) === projectName(current))) || m;

  async function ask(row: Row | undefined, opts: Options = {}, prompt = text) {
    const q = prompt.trim();
    if (!row || !q || sending) return;
    setSending(true);
    const s = await send(row, q, row === current ? effective : { provider: agent.provider, model: "" }, opts);
    setSending(false);
    if (s && !opts.browser) setSent(s);
  }

  // Launched with the prompt as an argument from the root search: send at once to the remembered project.
  useEffect(() => {
    if (autoSent.current || !props.prompt?.trim() || !data || !current) return;
    autoSent.current = true;
    void ask(current, {}, props.prompt);
  }, [data, current]);

  if (sent) return <AnswerView sent={sent} />;

  const chooseAgent = async (a: Agent) => {
    setAgent(a);
    await LocalStorage.setItem(AGENT_KEY, JSON.stringify(a));
  };
  const chooseProject = (r: Row) => {
    setKey(rowKey(r));
    void LocalStorage.setItem(PROJECT_KEY, rowKey(r));
  };

  const actionsFor = (row: Row) => (
    <ActionPanel>
      <ActionPanel.Section>
        <Action title="Ask" icon={Icon.ArrowRight} onAction={() => void ask(row)} />
        <Action
          title="Ask and Open in Browser"
          icon={Icon.Globe}
          shortcut={{ modifiers: ["cmd"], key: "return" }}
          onAction={() => void ask(row, { browser: true })}
        />
        <Action
          title="Ask in a Separate Worktree"
          icon={Icon.Tree}
          shortcut={{ modifiers: ["cmd", "shift"], key: "return" }}
          onAction={() => void ask(row, { worktree: true })}
        />
        <Action.Push
          title="Write a Longer Prompt"
          icon={Icon.Text}
          shortcut={{ modifiers: ["cmd"], key: "l" }}
          target={<LongPrompt row={row} text={text} onSend={(t, o) => ask(row, o, t)} />}
        />
      </ActionPanel.Section>
      <ActionPanel.Section>
        <ActionPanel.Submenu
          title="Choose Model"
          icon={Icon.Stars}
          shortcut={{ modifiers: ["cmd", "shift"], key: "m" }}
          isLoading={agents.isLoading}
        >
          {menu.map((a) => (
            <ActionPanel.Section key={a.provider} title={a.provider}>
              {[{ id: "", label: "Default model", hint: "" }, ...a.models].map((m) => (
                <Action
                  key={`${a.provider}:${m.id}`}
                  title={m.id ? m.label || m.id : `${a.provider} · default`}
                  icon={effective.provider === a.provider && effective.model === m.id ? Icon.CheckCircle : Icon.Circle}
                  onAction={() => void chooseAgent({ provider: a.provider, model: m.id })}
                />
              ))}
            </ActionPanel.Section>
          ))}
        </ActionPanel.Submenu>
        <ActionPanel.Submenu
          title="Choose Machine"
          icon={Icon.Desktop}
          shortcut={{ modifiers: ["cmd", "shift"], key: "t" }}
        >
          {machines.map((m) => (
            <Action
              key={machineKey(m)}
              title={`${multi ? `${m.instance.name} / ` : ""}${m.project.machineName}`}
              icon={
                current && machineKey(current) === machineKey(m)
                  ? Icon.CheckCircle
                  : m.project.online
                    ? Icon.Desktop
                    : Icon.WifiDisabled
              }
              onAction={() => chooseProject(onMachine(m))}
            />
          ))}
        </ActionPanel.Submenu>
      </ActionPanel.Section>
    </ActionPanel>
  );

  const where = (r: Row) =>
    `${multi ? `${r.instance.name} · ` : ""}${r.project.machineName}${r.project.online ? "" : " · offline, will queue"}`;

  return (
    <List
      isLoading={isLoading || sending}
      filtering={false}
      searchText={text}
      onSearchTextChange={setText}
      searchBarPlaceholder={
        current
          ? `Ask ${modelLabel(effective)} in ${projectName(current)} on ${current.project.machineName}...`
          : "Ask the agent..."
      }
      searchBarAccessory={
        <List.Dropdown
          tooltip="Project"
          value={current ? rowKey(current) : undefined}
          onChange={(v) => {
            setKey(v);
            void LocalStorage.setItem(PROJECT_KEY, v);
          }}
        >
          {machines.map((m) => (
            <List.Dropdown.Section
              key={machineKey(m)}
              title={`${m.project.online ? "🟢" : "⚪️"} ${multi ? `${m.instance.name} / ` : ""}${m.project.machineName}`}
            >
              {rows
                .filter((r) => machineKey(r) === machineKey(m))
                .map((r) => (
                  <List.Dropdown.Item
                    key={rowKey(r)}
                    value={rowKey(r)}
                    title={projectName(r)}
                    icon={Icon.Folder}
                    keywords={[r.project.machineName, r.project.cwd]}
                  />
                ))}
            </List.Dropdown.Section>
          ))}
        </List.Dropdown>
      }
    >
      {data && data.instances.length === 0 ? (
        <List.EmptyView icon={Icon.Plug} title="No hub connected" description="Run Connect Hub first." />
      ) : !text.trim() ? (
        <List.EmptyView
          icon="harnessly.png"
          title={
            current
              ? `${modelLabel(effective)} · ${projectName(current)} · ${current.project.machineName}`
              : "Ask the agent"
          }
          description={
            (data?.errors.length ? `${data.errors.join("\n")}\n\n` : "") +
            "Type the prompt and press ↵. ⌘↵ opens the chat in the browser, ⌘⇧↵ works in a separate worktree, " +
            "⌘L for a longer prompt, ⌘⇧M to choose the model, ⌘⇧T to choose the machine."
          }
        />
      ) : (
        <>
          {current ? (
            <List.Item
              icon={{ source: Icon.ArrowRight, tintColor: Color.Blue }}
              title={text.trim()}
              subtitle={`→ ${projectName(current)}`}
              accessories={[{ text: where(current), icon: Icon.Desktop }, { tag: modelLabel(effective) }]}
              actions={actionsFor(current)}
            />
          ) : null}
          {others.length > 0 ? (
            <List.Section title="Ask in another project">
              {others.map((r) => (
                <List.Item
                  key={rowKey(r)}
                  icon={r.project.online ? Icon.Folder : { source: Icon.Folder, tintColor: Color.SecondaryText }}
                  title={projectName(r)}
                  accessories={[{ text: where(r) }]}
                  actions={actionsFor(r)}
                />
              ))}
            </List.Section>
          ) : null}
        </>
      )}
    </List>
  );
}

function LongPrompt(props: { row: Row; text: string; onSend: (text: string, opts: Options) => Promise<void> }) {
  const nav = useNavigation();
  const submit = async (v: { text: string; worktree: boolean }) => {
    if (!v.text.trim()) return;
    nav.pop();
    await props.onSend(v.text, { worktree: v.worktree });
  };
  return (
    <Form
      navigationTitle={`Ask ${projectName(props.row)}`}
      actions={
        <ActionPanel>
          <Action.SubmitForm title="Ask" icon={Icon.ArrowRight} onSubmit={submit} />
        </ActionPanel>
      }
    >
      <Form.TextArea id="text" title="Prompt" defaultValue={props.text} autoFocus />
      <Form.Checkbox id="worktree" label="Work in a separate git worktree" defaultValue={false} />
    </Form>
  );
}

const busy = (state: string) => state === "starting" || state === "working";

/** The chat just started, refreshed every two seconds while the agent works. */
function AnswerView(props: { sent: Sent }) {
  const { row, prompt, chatId, url, queued } = props.sent;
  const conn = connOf(row.instance);
  const { data, isLoading, revalidate } = useCachedPromise(
    async (id: string | undefined) => (id ? getChat(conn, id, 1) : undefined),
    [chatId],
    { execute: !!chatId, keepPreviousData: true, onError: () => undefined },
  );
  const chat = data?.chat;
  const turn = data?.turns[data.turns.length - 1];
  const working = !chat || busy(chat.state);

  useEffect(() => {
    if (!chatId || (chat && !busy(chat.state))) return;
    const t = setInterval(revalidate, 2000);
    return () => clearInterval(t);
  }, [chatId, chat?.state]);

  const parts = [`> ${prompt.split("\n").join("\n> ")}`];
  if (queued) parts.push("_The machine is off. The chat starts when it connects._");
  else if (!chatId) parts.push("_Sent. Open the chat in the browser to follow it._");
  else if (chat?.attention === "question" || chat?.attention === "permission" || chat?.attention === "plan")
    parts.push(`**The agent waits for you.** Open the chat in the browser (⌘↵) to answer.`);
  if (turn?.answer?.trim()) parts.push(turn.answer.trim());
  else if (chatId && working)
    parts.push(
      `_Working${turn?.toolCount ? ` · ${turn.toolCount} tool calls` : ""}..._` +
        (chat?.snippet?.trim() ? `\n\n${chat.snippet.trim()}` : ""),
    );

  return (
    <Detail
      isLoading={isLoading || (!!chatId && working)}
      navigationTitle={projectName(row)}
      markdown={parts.join("\n\n")}
      metadata={
        <Detail.Metadata>
          <Detail.Metadata.Label title="Project" text={projectName(row)} />
          <Detail.Metadata.Label
            title="Machine"
            text={row.project.machineName}
            icon={row.project.online ? Icon.Desktop : Icon.WifiDisabled}
          />
          {chat ? (
            <Detail.Metadata.Label title="Agent" text={[chat.provider, chat.model].filter(Boolean).join(" / ")} />
          ) : null}
          <Detail.Metadata.Label title="State" text={queued ? "queued" : (chat?.state ?? "starting")} />
        </Detail.Metadata>
      }
      actions={
        <ActionPanel>
          {chat ? (
            <Action.Push
              title="Reply"
              icon={Icon.Reply}
              target={<ReplyForm instance={row.instance} chat={chat} onSent={revalidate} />}
            />
          ) : null}
          {url ? (
            <Action.OpenInBrowser title="Open in Browser" url={url} shortcut={{ modifiers: ["cmd"], key: "return" }} />
          ) : null}
          {chatId && working ? (
            <Action
              title="Stop"
              icon={Icon.Stop}
              style={Action.Style.Destructive}
              shortcut={{ modifiers: ["opt"], key: "x" }}
              onAction={async () => {
                try {
                  await stopChat(conn, chatId);
                  revalidate();
                } catch (e) {
                  await showToast({ style: Toast.Style.Failure, title: "Could not stop", message: describeError(e) });
                }
              }}
            />
          ) : null}
          {turn?.answer ? (
            <Action.CopyToClipboard
              title="Copy Answer"
              content={turn.answer}
              shortcut={Keyboard.Shortcut.Common.Copy}
            />
          ) : null}
          <Action
            title="Refresh"
            icon={Icon.ArrowClockwise}
            shortcut={Keyboard.Shortcut.Common.Refresh}
            onAction={revalidate}
          />
        </ActionPanel>
      }
    />
  );
}

export default function Command(props: LaunchProps<{ arguments: Arguments.NewPrompt }>) {
  return <AskView prompt={props.arguments?.prompt} />;
}
