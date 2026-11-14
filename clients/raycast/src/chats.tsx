import { Action, ActionPanel, Color, Icon, Keyboard, List, showToast, Toast } from "@raycast/api";
import { useCachedPromise } from "@raycast/utils";
import { useState } from "react";
import { Chat, ChatFilter, ChatResponse, describeError, getChat, markChatRead, stopChat } from "./api";
import { ConnectForm } from "./connect";
import { attentionText, connOf, Instance, loadAllChats, TaggedChat } from "./instances";
import { AskView } from "./new-prompt";
import { ReplyForm } from "./reply";

const stateIcon = (c: Chat): { source: Icon; tintColor: Color } => {
  if (c.attention === "question" || c.attention === "permission" || c.attention === "plan")
    return { source: Icon.ExclamationMark, tintColor: Color.Orange };
  switch (c.state) {
    case "working":
    case "starting":
      return { source: Icon.CircleProgress50, tintColor: Color.Blue };
    case "stopped":
      return { source: Icon.Stop, tintColor: Color.SecondaryText };
    case "ready":
      return { source: Icon.CheckCircle, tintColor: Color.Green };
    default:
      return { source: Icon.Circle, tintColor: Color.SecondaryText };
  }
};

function turnsMarkdown(chat: Chat, data: ChatResponse | undefined, loading: boolean): string {
  const parts: string[] = [];
  const att = attentionText(chat.attention);
  if (att) parts.push(`**${att}.** Open the chat in the browser to answer.`);
  parts.push(`## Summary\n\n${chat.recap?.trim() || chat.snippet?.trim() || "_No summary yet._"}`);
  if (data?.turns.length) {
    parts.push("## Last turns");
    for (const t of data.turns) {
      parts.push(`> ${t.prompt.trim().split("\n").join("\n> ")}\n\n${t.answer?.trim() || "_No answer yet._"}`);
    }
  } else if (loading) {
    parts.push("_Loading turns..._");
  }
  return parts.join("\n\n");
}

export default function Command() {
  const [filter, setFilter] = useState<ChatFilter>("attention");
  const [selected, setSelected] = useState<string | null>(null);
  const { data, isLoading, revalidate } = useCachedPromise(loadAllChats, [filter], { keepPreviousData: true });

  const sel = data?.items.find((i) => i.chat.id === selected);
  const detail = useCachedPromise(
    async (inst: Instance | undefined, id: string | undefined) =>
      inst && id ? getChat(connOf(inst), id, 5) : undefined,
    [sel?.instance, sel?.chat.id],
    { execute: !!sel, keepPreviousData: false },
  );

  const multi = (data?.instances.length ?? 0) > 1;
  const items = data?.items ?? [];
  const waiting = items.filter((i) => i.chat.attention !== "");
  const rest = filter === "all" ? items.filter((i) => i.chat.attention === "") : filter === "working" ? items : [];

  const render = (t: TaggedChat) => {
    const c = t.chat;
    const isSel = c.id === selected;
    const accessories: List.Item.Accessory[] = [];
    if (multi) accessories.push({ tag: t.instance.name });
    if (c.unread) accessories.push({ icon: { source: Icon.Dot, tintColor: Color.Blue }, tooltip: "Unread" });
    accessories.push({ icon: stateIcon(c), tooltip: c.state });
    if (!isSel) accessories.push({ text: c.machineName, icon: c.online ? Icon.Desktop : Icon.WifiDisabled });
    accessories.push({ date: new Date(c.activityAt || c.lastPromptAt) });
    return (
      <List.Item
        key={`${t.instance.id}:${c.id}`}
        id={c.id}
        title={c.title || "Untitled chat"}
        subtitle={isSel ? undefined : c.project}
        accessories={isSel ? accessories.filter((a) => !("text" in a)) : accessories}
        detail={
          <List.Item.Detail
            isLoading={isSel && detail.isLoading}
            markdown={turnsMarkdown(
              isSel && detail.data ? detail.data.chat : c,
              isSel ? detail.data : undefined,
              isSel && detail.isLoading,
            )}
            metadata={
              <List.Item.Detail.Metadata>
                <List.Item.Detail.Metadata.Label title="Project" text={c.project} />
                <List.Item.Detail.Metadata.Label
                  title="Machine"
                  text={c.machineName}
                  icon={c.online ? Icon.Desktop : Icon.WifiDisabled}
                />
                <List.Item.Detail.Metadata.Label
                  title="Agent"
                  text={[c.provider, c.model].filter(Boolean).join(" / ")}
                />
                <List.Item.Detail.Metadata.Label title="State" text={c.state} />
                {multi ? <List.Item.Detail.Metadata.Label title="Hub" text={t.instance.name} /> : null}
                {c.queue > 0 ? (
                  <List.Item.Detail.Metadata.Label title="Queued messages" text={String(c.queue)} />
                ) : null}
              </List.Item.Detail.Metadata>
            }
          />
        }
        actions={<ChatActions t={t} revalidate={revalidate} />}
      />
    );
  };

  return (
    <List
      isLoading={isLoading}
      isShowingDetail={items.length > 0}
      onSelectionChange={setSelected}
      searchBarPlaceholder="Search chats"
      searchBarAccessory={
        <List.Dropdown tooltip="Filter" value={filter} onChange={(v) => setFilter(v as ChatFilter)}>
          <List.Dropdown.Item title={`Attention${data ? ` (${data.counts.attention})` : ""}`} value="attention" />
          <List.Dropdown.Item title={`Working${data ? ` (${data.counts.working})` : ""}`} value="working" />
          <List.Dropdown.Item title={`All${data ? ` (${data.counts.all})` : ""}`} value="all" />
        </List.Dropdown>
      }
    >
      {data && data.instances.length === 0 ? (
        <List.EmptyView
          icon={Icon.Plug}
          title="No hub connected"
          description="Connect a Harnessly hub to see your chats."
          actions={
            <ActionPanel>
              <Action.Push title="Connect Hub" icon={Icon.Plus} target={<ConnectForm onDone={revalidate} />} />
            </ActionPanel>
          }
        />
      ) : (
        <List.EmptyView icon={Icon.CheckCircle} title="Nothing here" description="No chats match this filter." />
      )}
      {(data?.errors ?? []).map((e) => (
        <List.Section key={e.instance.id} title="Problems">
          <List.Item
            icon={{ source: Icon.Warning, tintColor: Color.Red }}
            title={e.instance.name}
            subtitle={e.message}
            actions={
              <ActionPanel>
                {e.tokenInvalid ? (
                  <Action.Push
                    title="Reconnect Instance"
                    icon={Icon.ArrowClockwise}
                    target={<ConnectForm url={e.instance.url} name={e.instance.name} onDone={revalidate} />}
                  />
                ) : null}
                <Action title="Refresh" icon={Icon.ArrowClockwise} onAction={revalidate} />
              </ActionPanel>
            }
          />
        </List.Section>
      ))}
      {waiting.length > 0 ? (
        <List.Section title="Waiting for you" subtitle={String(waiting.length)}>
          {waiting.map(render)}
        </List.Section>
      ) : null}
      {rest.length > 0 ? (
        <List.Section title={filter === "working" ? "Working" : "Other chats"} subtitle={String(rest.length)}>
          {rest.map(render)}
        </List.Section>
      ) : null}
    </List>
  );
}

function ChatActions(props: { t: TaggedChat; revalidate: () => void }) {
  const { instance, chat } = props.t;
  const conn = connOf(instance);

  async function run(title: string, fn: () => Promise<void>) {
    const toast = await showToast({ style: Toast.Style.Animated, title });
    try {
      await fn();
      toast.style = Toast.Style.Success;
      toast.title = "Done";
      props.revalidate();
    } catch (e) {
      toast.style = Toast.Style.Failure;
      toast.title = "Failed";
      toast.message = describeError(e);
    }
  }

  return (
    <ActionPanel>
      <Action.Push
        title="Reply"
        icon={Icon.Reply}
        target={<ReplyForm instance={instance} chat={chat} onSent={props.revalidate} />}
      />
      <Action.OpenInBrowser title="Open in Browser" url={chat.url} />
      <Action
        title="Mark Read"
        icon={Icon.Eye}
        shortcut={{ modifiers: ["opt"], key: "m" }}
        onAction={() => run("Marking read...", () => markChatRead(conn, chat.id))}
      />
      <Action
        title="Stop"
        icon={Icon.Stop}
        style={Action.Style.Destructive}
        shortcut={{ modifiers: ["opt"], key: "x" }}
        onAction={() => run("Stopping...", () => stopChat(conn, chat.id))}
      />
      <Action.CopyToClipboard title="Copy URL" content={chat.url} shortcut={Keyboard.Shortcut.Common.Copy} />
      <Action.Push
        title="Ask in This Project"
        icon={Icon.Plus}
        shortcut={Keyboard.Shortcut.Common.New}
        target={<AskView instanceId={instance.id} machineId={chat.machineId} projectId={chat.projectId ?? undefined} />}
      />
      <Action.Push
        title="Reconnect Instance"
        icon={Icon.Plug}
        target={<ConnectForm url={instance.url} name={instance.name} onDone={props.revalidate} />}
      />
      <Action
        title="Refresh"
        icon={Icon.ArrowClockwise}
        shortcut={Keyboard.Shortcut.Common.Refresh}
        onAction={props.revalidate}
      />
    </ActionPanel>
  );
}
