import { Color, Icon, launchCommand, LaunchType, MenuBarExtra, open } from "@raycast/api";
import { useCachedPromise } from "@raycast/utils";
import { attentionText, loadAllChats } from "./instances";

export default function Command() {
  const { data, isLoading } = useCachedPromise(async () => {
    const all = await loadAllChats("all");
    return all;
  });

  const attention = (data?.items ?? []).filter((i) => i.chat.attention !== "");
  const working = (data?.items ?? []).filter((i) => i.chat.attention === "" && i.chat.state === "working");
  const count = data?.counts.attention ?? 0;
  const multi = (data?.instances.length ?? 0) > 1;
  const tag = (name: string) => (multi ? ` (${name})` : "");

  return (
    <MenuBarExtra
      isLoading={isLoading}
      icon={{ source: { light: "menubar.png", dark: "menubar@dark.png" } }}
      title={count > 0 ? String(count) : undefined}
      tooltip="Harnessly chats"
    >
      {data && data.instances.length === 0 ? (
        <MenuBarExtra.Item
          title="Connect a Hub"
          icon={Icon.Plug}
          onAction={() => launchCommand({ name: "add-instance", type: LaunchType.UserInitiated })}
        />
      ) : null}
      {(data?.errors ?? []).map((e) => (
        <MenuBarExtra.Item
          key={e.instance.id}
          icon={{ source: Icon.Warning, tintColor: Color.Red }}
          title={`${e.instance.name}: ${e.message}`}
          onAction={() =>
            e.tokenInvalid
              ? launchCommand({ name: "manage-instances", type: LaunchType.UserInitiated })
              : open(e.instance.url)
          }
        />
      ))}
      <MenuBarExtra.Section title={`Waiting for you (${attention.length})`}>
        {attention.length === 0 ? <MenuBarExtra.Item title="Nothing waits for you" /> : null}
        {attention.map((t) => (
          <MenuBarExtra.Item
            key={`${t.instance.id}:${t.chat.id}`}
            title={`${t.chat.title || "Untitled chat"}${tag(t.instance.name)}`}
            subtitle={`${attentionText(t.chat.attention)} · ${t.chat.project}`}
            icon={Icon.ExclamationMark}
            onAction={() => open(t.chat.url)}
          />
        ))}
      </MenuBarExtra.Section>
      {working.length > 0 ? (
        <MenuBarExtra.Section title="Working">
          {working.map((t) => (
            <MenuBarExtra.Item
              key={`${t.instance.id}:${t.chat.id}`}
              title={`${t.chat.title || "Untitled chat"}${tag(t.instance.name)}`}
              subtitle={t.chat.project}
              icon={Icon.CircleProgress50}
              onAction={() => open(t.chat.url)}
            />
          ))}
        </MenuBarExtra.Section>
      ) : null}
      <MenuBarExtra.Section>
        <MenuBarExtra.Item
          title="Open Chats"
          icon={Icon.List}
          onAction={() => launchCommand({ name: "chats", type: LaunchType.UserInitiated })}
        />
        <MenuBarExtra.Item
          title="Manage Hubs"
          icon={Icon.Gear}
          onAction={() => launchCommand({ name: "manage-instances", type: LaunchType.UserInitiated })}
        />
      </MenuBarExtra.Section>
    </MenuBarExtra>
  );
}
