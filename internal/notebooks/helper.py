"""Private line-JSON bridge to Jupyter; never imports from the notebook's cwd.

Invoked by the embedded Go launcher with Python -I. The kernel is deliberately a
normal local Python process, not an untrusted-code sandbox.
"""
import contextlib
import json
import os
import queue
import signal
import sys
import threading
import time

DEPENDENCIES = (
    "Notebook execution needs Python with jupyter_client and ipykernel. "
    "Install them with python3 -m pip install jupyter_client ipykernel, or create "
    "a virtualenv, install them there, and set RUNTIME_NOTEBOOK_PYTHON to its "
    "absolute bin/python path."
)


class OutputLimit(Exception):
    pass


class Outputs:
    def __init__(self, max_bytes, max_outputs):
        self.items = []
        self.displays = {}
        self.clear_wait = False
        self.max_bytes = max_bytes
        self.max_outputs = max_outputs
        self.sizes = []
        self.bytes = 0

    @staticmethod
    def size(item):
        return len(json.dumps(item, ensure_ascii=True, separators=(",", ":")).encode())

    def check(self):
        # Leave room for the result envelope, execution count and JSON newlines.
        if self.bytes > self.max_bytes - 4096 or len(self.items) > self.max_outputs:
            raise OutputLimit()

    def clear(self):
        self.items.clear()
        self.sizes.clear()
        self.displays.clear()
        self.bytes = 0
        self.clear_wait = False

    def accept(self, kind, content):
        if kind == "clear_output":
            if content.get("wait", False):
                self.clear_wait = True
            else:
                self.clear()
            return
        if kind not in ("stream", "display_data", "execute_result", "error", "update_display_data"):
            return
        if self.clear_wait:
            self.clear()
        display_id = content.get("transient", {}).get("display_id")
        if kind == "stream":
            item = {"output_type": kind, "name": content.get("name", "stdout"), "text": content.get("text", "")}
        elif kind == "error":
            item = {"output_type": kind, "ename": content.get("ename", "Error"), "evalue": content.get("evalue", ""), "traceback": content.get("traceback", [])}
        else:
            item = {"output_type": "display_data" if kind == "update_display_data" else kind, "data": content.get("data", {}), "metadata": content.get("metadata", {})}
            if kind == "execute_result":
                item["execution_count"] = content.get("execution_count")
        if kind == "update_display_data":
            for index in self.displays.get(display_id, []):
                updated = dict(self.items[index], data=item["data"], metadata=item["metadata"])
                size = self.size(updated)
                self.bytes += size - self.sizes[index]
                self.items[index], self.sizes[index] = updated, size
                self.check()
            return
        if kind == "stream" and self.items and self.items[-1].get("output_type") == "stream" and self.items[-1]["name"] == item["name"]:
            # Check the increment before concatenation to keep repeated prints linear.
            extra = self.size(item["text"]) - 2
            self.bytes += extra
            self.check()
            self.items[-1]["text"] += item["text"]
            self.sizes[-1] += extra
            return
        size = self.size(item)
        self.bytes += size
        self.check()
        if display_id:
            self.displays.setdefault(display_id, []).append(len(self.items))
        self.items.append(item)
        self.sizes.append(size)
        self.check()


def execute(km, client, code, max_bytes, max_outputs):
    msg_id = client.execute(code, allow_stdin=False, stop_on_error=True)
    outputs = Outputs(max_bytes, max_outputs)
    idle = False
    reply = None
    deadline = time.monotonic() + 120
    while not idle or reply is None:
        if time.monotonic() > deadline:
            raise TimeoutError("Notebook execution timed out; the kernel was stopped.")
        if not km.is_alive():
            raise RuntimeError("The notebook kernel stopped. Run the cell again to start a fresh kernel.")
        try:
            msg = client.get_iopub_msg(timeout=0.05)
            if msg.get("parent_header", {}).get("msg_id") == msg_id:
                kind, content = msg["header"]["msg_type"], msg["content"]
                if kind == "status" and content.get("execution_state") == "idle":
                    idle = True
                else:
                    outputs.accept(kind, content)
        except queue.Empty:
            pass
        if reply is None:
            try:
                msg = client.get_shell_msg(timeout=0.01)
                if msg.get("parent_header", {}).get("msg_id") == msg_id:
                    reply = msg["content"]
            except queue.Empty:
                pass
    status = "ok" if reply.get("status") == "ok" else "error"
    # An exceptional kernel may report an error only on the shell channel.
    if status == "error" and not any(item["output_type"] == "error" for item in outputs.items):
        outputs.accept("error", reply)
    return {"outputs": outputs.items, "execution_count": reply.get("execution_count"), "status": status}


def main():
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
    protocol = sys.stdout
    lock = threading.Lock()

    def send(id_, **fields):
        with lock:
            protocol.write(json.dumps(dict(id=id_, **fields), ensure_ascii=True, separators=(",", ":")) + "\n")
            protocol.flush()

    try:
        with contextlib.redirect_stdout(sys.stderr):
            import ipykernel  # noqa: F401: fail actionably before attempting startup
            from jupyter_client import KernelManager
            from jupyter_client.kernelspec import KernelSpec
    except ImportError:
        send(0, error="dependencies", message=DEPENDENCIES)
        return
    notebook, max_bytes, max_outputs = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
    # Override the spec getter: no metadata or installed kernelspec executable is
    # trusted. -I also prevents a local json.py/ipykernel_launcher.py from loading
    # before the first explicitly executed cell.
    class PythonKernelManager(KernelManager):
        @property
        def kernel_spec(self):
            return KernelSpec(argv=[sys.executable, "-I", "-m", "ipykernel_launcher", "-f", "{connection_file}"], display_name="Python 3", language="python")

    km = PythonKernelManager(kernel_name="python3")
    client = None
    busy = threading.Lock()
    try:
        with contextlib.redirect_stdout(sys.stderr), open(os.devnull, "wb") as null:
            km.start_kernel(cwd=os.path.dirname(notebook), env=dict(os.environ), stdout=null, stderr=null)
            child = getattr(km, "provisioner", None) or getattr(km, "kernel", None)
            send(0, pid=getattr(child, "pid", 0))
            client = km.blocking_client()
            client.start_channels()
            client.wait_for_ready(timeout=25)
        send(0, ready=True)

        def run(request):
            try:
                result = execute(km, client, request.get("code", ""), max_bytes, max_outputs)
                response = {"result": result}
            except OutputLimit:
                response = {"error": "output-limit", "message": "Notebook output exceeded its limit; the kernel was stopped."}
            except TimeoutError as exc:
                response = {"error": "timeout", "message": str(exc)}
            except Exception:
                response = {"error": "kernel", "message": "The notebook kernel stopped. Run the cell again to start a fresh kernel."}
            finally:
                busy.release()
            send(request["id"], **response)

        for line in sys.stdin:
            request = json.loads(line)
            action = request.get("action")
            if action == "interrupt":
                km.interrupt_kernel()
                send(request["id"])
            elif action == "execute":
                if not busy.acquire(blocking=False):
                    send(request["id"], error="busy", message="This notebook is busy.")
                else:
                    threading.Thread(target=run, args=(request,), daemon=True).start()
            else:
                send(request["id"], error="kernel", message="Unknown notebook operation.")
    except Exception:
        send(0, error="kernel", message="The Python kernel could not start. Check the configured Python environment has working jupyter_client and ipykernel installations.")
    finally:
        if client is not None:
            client.stop_channels()
        with contextlib.suppress(Exception):
            km.shutdown_kernel(now=True)


if __name__ == "__main__":
    main()
