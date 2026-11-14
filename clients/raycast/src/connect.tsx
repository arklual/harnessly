import { Action, ActionPanel, Detail, Form, Icon, open, showToast, Toast, useNavigation } from "@raycast/api";
import { useEffect, useRef, useState } from "react";
import os from "node:os";
import { ApiError, deviceStart, deviceToken, DeviceStart, describeError, normalizeUrl } from "./api";
import { addInstance } from "./instances";

type Phase =
  | { kind: "starting" }
  | { kind: "waiting"; start: DeviceStart }
  | { kind: "done"; email: string }
  | { kind: "failed"; message: string };

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

function DeviceFlow(props: { url: string; name: string; onDone?: () => void }) {
  const [phase, setPhase] = useState<Phase>({ kind: "starting" });
  const [attempt, setAttempt] = useState(0);
  const cancelled = useRef(false);

  useEffect(() => {
    cancelled.current = false;
    (async () => {
      setPhase({ kind: "starting" });
      try {
        const start = await deviceStart(props.url, "Raycast", os.hostname());
        if (cancelled.current) return;
        setPhase({ kind: "waiting", start });
        await open(start.verifyUrl);
        const deadline = Date.now() + start.expiresIn * 1000;
        const interval = Math.max(start.interval, 1) * 1000;
        while (!cancelled.current && Date.now() < deadline) {
          await sleep(interval);
          if (cancelled.current) return;
          try {
            const t = await deviceToken(props.url, start.deviceCode);
            await addInstance({
              name: props.name,
              url: props.url,
              token: t.token,
              email: t.user.email,
              tokenId: t.tokenId,
            });
            setPhase({ kind: "done", email: t.user.email });
            await showToast({ style: Toast.Style.Success, title: `Connected ${props.name}`, message: t.user.email });
            props.onDone?.();
            return;
          } catch (e) {
            if (e instanceof ApiError && e.code === "authorization_pending") continue;
            if (e instanceof ApiError && e.code === "access_denied") {
              setPhase({ kind: "failed", message: "You refused the connection in the browser." });
              return;
            }
            if (e instanceof ApiError && e.code === "expired_token") {
              setPhase({ kind: "failed", message: "The code expired. Start again." });
              return;
            }
            if (e instanceof ApiError && e.code === "slow_down") {
              await sleep(interval);
              continue;
            }
            if (e instanceof ApiError && e.status >= 400 && e.status < 500) {
              setPhase({ kind: "failed", message: describeError(e) });
              return;
            }
            // network hiccup: keep polling until the code expires
          }
        }
        if (!cancelled.current) setPhase({ kind: "failed", message: "The code expired. Start again." });
      } catch (e) {
        if (!cancelled.current) setPhase({ kind: "failed", message: describeError(e) });
      }
    })();
    return () => {
      cancelled.current = true;
    };
  }, [attempt]);

  let markdown: string;
  const actions = [];
  if (phase.kind === "starting") {
    markdown = `# Connecting to ${props.name}\n\nAsking the hub for a code...`;
  } else if (phase.kind === "waiting") {
    markdown =
      `# ${phase.start.userCode}\n\n` +
      `Confirm this code in the browser window that just opened. If it did not open, use the action below.\n\n` +
      `The code is valid for ${Math.round(phase.start.expiresIn / 60)} minutes.`;
    actions.push(<Action.OpenInBrowser key="open" title="Open Verification Page" url={phase.start.verifyUrl} />);
    actions.push(<Action.CopyToClipboard key="copy" title="Copy Code" content={phase.start.userCode} />);
  } else if (phase.kind === "done") {
    markdown = `# Connected\n\nSigned in as **${phase.email}**. You can close this window.`;
  } else {
    markdown = `# Not connected\n\n${phase.message}`;
    actions.push(
      <Action key="retry" title="Try Again" icon={Icon.ArrowClockwise} onAction={() => setAttempt((n) => n + 1)} />,
    );
  }
  return (
    <Detail
      isLoading={phase.kind === "starting" || phase.kind === "waiting"}
      markdown={markdown}
      actions={<ActionPanel>{actions}</ActionPanel>}
    />
  );
}

/** The form: hub address and a name, then the device flow. */
export function ConnectForm(props: { url?: string; name?: string; onDone?: () => void }) {
  const { push } = useNavigation();
  const [urlError, setUrlError] = useState<string | undefined>();

  function submit(values: { url: string; name: string }) {
    if (!values.url.trim()) {
      setUrlError("Enter the hub address");
      return;
    }
    const url = normalizeUrl(values.url);
    let host = url;
    try {
      host = new URL(url).host;
    } catch {
      setUrlError("This is not a valid address");
      return;
    }
    push(<DeviceFlow url={url} name={values.name.trim() || host} onDone={props.onDone} />);
  }

  return (
    <Form
      actions={
        <ActionPanel>
          <Action.SubmitForm title="Connect" onSubmit={submit} />
        </ActionPanel>
      }
    >
      <Form.TextField
        id="url"
        title="Hub URL"
        placeholder="https://app.example.com"
        defaultValue={props.url}
        error={urlError}
        onChange={() => setUrlError(undefined)}
      />
      <Form.TextField id="name" title="Name" placeholder="Work" defaultValue={props.name} />
      <Form.Description text="You will confirm a short code in the browser. The extension never sees your password." />
    </Form>
  );
}
