# Jupyter notebooks in the file pane

Open a local `.ipynb` with `harnessly open example.ipynb`, a file link in the conversation, or the file list in the
right pane. Notebooks use the existing file editor's paths, downloads, revision-checked saving, unsaved drafts and
close confirmation. A notebook in nbformat 4 opens as cells; malformed files and older formats remain available in the
text editor. Opening a file never starts a kernel or runs code. Notebooks may be up to **16 MiB**, including embedded
output; the usual **1 MB** limit for other text files is unchanged.

## Reading and editing

- Markdown cells render with the file pane's Markdown renderer; switch a cell to Edit to change its source.
- Code cells use the same CodeMirror setup, language highlighting, indentation and shortcuts as text files.
- Saved output includes stdout/stderr, tracebacks, text, JSON, Markdown, raster/SVG images and static HTML tables.
- HTML output uses a scriptless, opaque-origin iframe and the existing HTML cleaner, with a restrictive content
  security policy. JavaScript, active widgets, external assets and interactive HTML visualizations are not enabled.
- Unrecognized metadata, attachments, cell IDs and output formats are preserved when saving. Markdown attachments
  and unsupported output MIME types are not interpreted by this first viewer.

## Execution

Only Python 3 is supported initially. The notebook's kernelspec is not an instruction to execute a program: notebooks
that declare another kernel remain viewable and editable, but the UI does not run them with Python by mistake.

Before the first run in an open file, choose **Allow execution**. This is a trust decision, not a sandbox: code runs
on the machine and has the same filesystem access as a terminal process under the runtime's OS account. Only run
notebooks whose code you trust. Execution is restricted to regular `.ipynb` files whose canonical paths are inside
the conversation's folder; being allowed to preview a handed-over file elsewhere does not grant execution access.

- **Run** executes one code cell; **Shift+Enter** does the same while editing its source.
- **Run all** executes code cells in document order, skipping Markdown and raw text. It stops on the first error.
- Variables survive between runs in the same notebook's kernel. The notebook's folder is its working directory.
- **Interrupt** interrupts the current execution and cancels the remaining run-all queue.
- **Restart kernel** discards variables; **Shut down kernel** releases the kernel. Previously saved output is not
  erased by either action.
- Leaving the file interrupts an active run and cancels the remaining queue. Completed results stay in the ordinary
  unsaved draft; the interrupted cell's pending result is not applied after the editor has left. Kernels can remain
  idle for later use and are cleaned up by the execution service.

Execution updates the open notebook in memory, not the disk. Use the existing **Save** button or **Ctrl/Cmd+S** to
save code and results. If the disk changed, the normal reload/overwrite conflict prompt applies; execution never
silently replaces changes another process made.

## Python dependency

Reading, editing and rendering saved results do not require Python or Jupyter. Execution requires `jupyter_client`
and `ipykernel` in the Python interpreter used by the runtime. Prefer a dedicated virtual environment on systems
whose Python installation is managed by the OS:

```sh
python3 -m venv ~/.local/share/harnessly/notebook-python
~/.local/share/harnessly/notebook-python/bin/python -m pip install jupyter_client ipykernel
```

Configure the runtime with the absolute path of that environment's `bin/python` as `RUNTIME_NOTEBOOK_PYTHON`. Without
an override the execution service uses `python3`. Missing dependencies produce an actionable execution error rather
than preventing a notebook from opening. Dependencies are not installed automatically when a file is opened or run.
