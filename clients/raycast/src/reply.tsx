import { Action, ActionPanel, Form, showToast, Toast, useNavigation } from "@raycast/api";
import { describeError, sendMessage } from "./api";
import { connOf, Instance } from "./instances";
import { Chat } from "./api";

export function ReplyForm(props: { instance: Instance; chat: Chat; onSent?: () => void }) {
  const { pop } = useNavigation();

  async function submit(values: { text: string; mode: string }) {
    const text = values.text.trim();
    if (!text) {
      await showToast({ style: Toast.Style.Failure, title: "Write a message first" });
      return;
    }
    const toast = await showToast({ style: Toast.Style.Animated, title: "Sending..." });
    try {
      const r = await sendMessage(connOf(props.instance), props.chat.id, text, values.mode === "now" ? "now" : "queue");
      if (r.delivery === "failed") {
        toast.style = Toast.Style.Failure;
        toast.title = "Delivery failed";
        toast.message = r.error;
        return;
      }
      toast.style = Toast.Style.Success;
      toast.title = r.delivery === "delivered" ? "Delivered" : "Queued";
      toast.message = r.delivery === "queued" ? "The machine is off; it gets the message when it connects" : undefined;
      props.onSent?.();
      pop();
    } catch (e) {
      toast.style = Toast.Style.Failure;
      toast.title = "Could not send";
      toast.message = describeError(e);
    }
  }

  return (
    <Form
      navigationTitle={`Reply: ${props.chat.title}`}
      actions={
        <ActionPanel>
          <Action.SubmitForm title="Send" onSubmit={submit} />
        </ActionPanel>
      }
    >
      <Form.TextArea id="text" title="Message" placeholder="Reply to the agent" enableMarkdown={false} autoFocus />
      <Form.Dropdown id="mode" title="When" defaultValue="queue">
        <Form.Dropdown.Item value="queue" title="After the current turn" />
        <Form.Dropdown.Item value="now" title="Right now" />
      </Form.Dropdown>
    </Form>
  );
}
