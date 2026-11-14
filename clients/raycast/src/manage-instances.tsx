import { Action, ActionPanel, Alert, confirmAlert, Icon, List, showToast, Toast } from "@raycast/api";
import { usePromise } from "@raycast/utils";
import { ConnectForm } from "./connect";
import { loadInstances, removeInstance } from "./instances";

export default function Command() {
  const { data, isLoading, revalidate } = usePromise(loadInstances);

  return (
    <List isLoading={isLoading} searchBarPlaceholder="Connected hubs">
      <List.EmptyView
        icon={Icon.Plug}
        title="No hubs connected"
        description="Connect a hub to see your chats."
        actions={
          <ActionPanel>
            <Action.Push title="Connect Hub" icon={Icon.Plus} target={<ConnectForm onDone={revalidate} />} />
          </ActionPanel>
        }
      />
      {(data ?? []).map((i) => (
        <List.Item
          key={i.id}
          icon={Icon.Globe}
          title={i.name}
          subtitle={i.url}
          accessories={[{ text: i.email }]}
          actions={
            <ActionPanel>
              <Action.Push title="Connect Another Hub" icon={Icon.Plus} target={<ConnectForm onDone={revalidate} />} />
              <Action.Push
                title="Reconnect Hub"
                icon={Icon.ArrowClockwise}
                target={<ConnectForm url={i.url} name={i.name} onDone={revalidate} />}
              />
              <Action.OpenInBrowser title="Open Hub" url={i.url} />
              <Action
                title="Disconnect Hub"
                icon={Icon.Trash}
                style={Action.Style.Destructive}
                onAction={async () => {
                  if (
                    await confirmAlert({
                      title: `Disconnect ${i.name}?`,
                      message: "The token is forgotten here. Revoke it in the hub's account settings too.",
                      primaryAction: { title: "Disconnect", style: Alert.ActionStyle.Destructive },
                    })
                  ) {
                    await removeInstance(i.id);
                    await showToast({ style: Toast.Style.Success, title: "Hub disconnected" });
                    revalidate();
                  }
                }}
              />
            </ActionPanel>
          }
        />
      ))}
    </List>
  );
}
