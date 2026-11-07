// Sinopia's side of the host: Sinopia is a builtin app (internal/apps/builtin/sinopia), and what it asks of
// the host — a message to the chat linked to the document, a toast, a question, the shortcut commands of its screens,
// the tools and the context it offers the chat's agent — goes through the SDK in the page's own process, under the
// rights of its manifest, not through the host's modules. This is the one place it is wired to the host's work.
import { sendMessage } from '@/next/commands/messages';
import { BridgeError, type IBridgeHandlers } from '@/next/features/app-views/bridge';
import { connectBuiltin, type IBuiltinHl } from '@/next/features/app-views/builtin-app';
import { askConfirm } from '@/next/features/app-views/confirm';
import { hostKeys } from '@/next/features/app-views/host-keys';
import { pageOffer } from '@/next/features/app-views/page-tools';
import { t } from '@/next/i18n';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';

export const SINOPIA_APP = 'harnessly.sinopia';
const SINOPIA_NAME = 'Sinopia';

export interface ISinopiaWhere {
  machine: MachineId;
  /** The chat linked to the document; null while it has none. */
  conversation: ConvId | null;
}

/**
 * What the host does for Sinopia wherever it is: Sinopia is the host's own, so its toasts read as it wrote them, and a
 * key its boards pass on runs as a press in the host's page (the boards are its own frames).
 */
const uiHandlers = (): IBridgeHandlers => ({
  toast: (text, options) => void toast(text, options),
  confirm: (text) => askConfirm(SINOPIA_NAME, text),
  ...hostKeys(() => true, true),
});

let ui: IBuiltinHl | null = null;

/** Sinopia's SDK for what needs no document: toasts, questions, keys. One for the page. */
export const sinopiaUi = (): IBuiltinHl => {
  ui ??= connectBuiltin(SINOPIA_APP, { handlers: uiHandlers() });
  return ui;
};

const noChat = () => {
  throw new BridgeError('host_unavailable', 'the document has no chat');
};

/**
 * Sinopia's SDK for a document of a machine and the chat linked to it. Its page tools and what it publishes reach the
 * agent of that chat (page_tools, page_call) while it is up; dispose() takes them back.
 */
export const connectSinopia = (where: ISinopiaWhere, document = ''): IBuiltinHl => {
  const { machine, conversation } = where;
  let hl: IBuiltinHl | null = null;
  // Made on the first tool or context: a connection that only sends to the chat offers nothing.
  let made: ReturnType<typeof pageOffer> | null = null;
  const offer = () => {
    if (conversation === null) return null;
    made ??= pageOffer(
      machine,
      conversation,
      `sinopia:${document}`,
      SINOPIA_APP,
      (id, name, args) => hl?.notifyView('harnessly/notifications/tool-call', { id, name, arguments: args }),
      (tool, shown) =>
        askConfirm(
          SINOPIA_NAME,
          t('next.appview.pageToolConfirm', {
            tool: tool.name,
            action: tool.description || tool.name,
            args: shown,
          }),
        ),
    );
    return made;
  };
  hl = connectBuiltin(SINOPIA_APP, {
    handlers: {
      ...uiHandlers(),
      chatSend: async (text) => {
        if (conversation === null) noChat();
        else sendMessage(machine, { conversationId: conversation, text });
      },
      pageTools: (tools) => offer()?.tools(tools) ?? noChat(),
      pageToolResult: (id, answer) => made?.result(id, answer),
      publish: (data) => offer()?.publish(data) ?? noChat(),
    },
    onDispose: () => made?.drop(),
  });
  return hl;
};
