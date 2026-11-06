#!/usr/bin/env python3
"""The data of a task's card: the task from the person's tracker (harnessly issue, through Connections) and, when
GITLAB_TOKEN is in the environment (harnessly secret run), the merge requests that name the task and their pipelines.
Prints one JSON object; never prints the token."""

import json
import os
import re
import subprocess
import sys
import time
import urllib.parse
import urllib.request

KEY = re.compile(r"^[A-Z][A-Z0-9]{0,15}-\d{1,7}$")
DONE_WORDS = ("готово когда", "done when", "definition of done")


def run(*args):
    out = subprocess.run(args, capture_output=True, text=True, timeout=30)
    return out.returncode, out.stdout, out.stderr.strip()


def plain(text):
    """A line of the tracker's markdown as plain words: no code ticks, emphasis or link targets."""
    text = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", text)
    return re.sub(r"[`*_]{1,3}", "", text).strip()


def checklist(description):
    """The «Готово когда» items: - [ ] / - [x] lines after the heading, or every such line when there is none."""
    lines = (description or "").splitlines()
    start = 0
    for i, line in enumerate(lines):
        if any(w in line.lower() for w in DONE_WORDS):
            start = i + 1
            break
    items = []
    for line in lines[start:]:
        m = re.match(r"^\s*[-*]\s+\[( |x|X)\]\s+(.*)$", line)
        if m:
            items.append({"text": plain(m.group(2))[:200], "done": m.group(1).lower() == "x"})
        elif items and line.startswith("**"):
            break
    return {"done": sum(1 for i in items if i["done"]), "total": len(items), "items": items[:12]}


def gitlab_host():
    code, out, _ = run("harnessly", "connections")
    if code != 0:
        return None
    for line in out.splitlines():
        cols = line.split("\t")
        if len(cols) >= 4 and cols[1] == "gitlab" and cols[3].startswith("https://"):
            return cols[3].rstrip("/")
    return None


def gitlab(base, token, path, query):
    url = f"{base}/api/v4/{path}?{urllib.parse.urlencode(query)}"
    req = urllib.request.Request(url, headers={"PRIVATE-TOKEN": token, "User-Agent": "harnessly-task-pulse"})
    with urllib.request.urlopen(req, timeout=20) as resp:
        return json.load(resp)


def merge_requests(key):
    token = os.environ.get("GITLAB_TOKEN", "").strip()
    if not token:
        return None, "no-token"
    base = gitlab_host()
    if not base:
        return None, "no-gitlab"
    try:
        found = gitlab(base, token, "merge_requests", {"scope": "all", "search": key, "in": "title,description",
                                                        "order_by": "updated_at", "per_page": 6})
    except Exception as e:  # the reason, not the token: urllib's errors carry no headers
        return None, f"gitlab: {type(e).__name__}"
    mrs = []
    for mr in found:
        # The search is by words: keep the ones that name exactly this key.
        if not re.search(rf"(?<![A-Z0-9-]){re.escape(key)}(?!\d)", f"{mr.get('title', '')} {mr.get('description') or ''}"):
            continue
        item = {"iid": mr.get("iid"), "title": (mr.get("title") or "")[:160], "url": mr.get("web_url"),
                "state": mr.get("state"), "project": (mr.get("references") or {}).get("full", "").split("!")[0],
                "pipeline": None}
        try:
            full = gitlab(base, token, f"projects/{mr['project_id']}/merge_requests/{mr['iid']}", {})
            p = full.get("head_pipeline")
            if p:
                item["pipeline"] = {"status": p.get("status"), "url": p.get("web_url")}
            item["mergeStatus"] = full.get("detailed_merge_status")
        except Exception:
            pass
        mrs.append(item)
    return mrs, "ok"


def main():
    key = (sys.argv[1] if len(sys.argv) > 1 else "").strip().upper()
    card = {"key": key, "checkedAt": int(time.time() * 1000)}
    if not KEY.match(key):
        card["error"] = "not-a-key"
        print(json.dumps(card, ensure_ascii=False))
        return
    code, out, err = run("harnessly", "issue", key, "--json")
    try:
        issue = json.loads(out)["issue"] if code == 0 else None
    except (ValueError, KeyError):
        issue = None
    if issue is None:
        card["error"] = "no-issue"
        card["reason"] = (err or out).splitlines()[0][:200] if (err or out) else ""
        print(json.dumps(card, ensure_ascii=False))
        return
    status = issue.get("status") or {}
    card.update({
        "title": issue.get("title", ""), "url": issue.get("url", ""), "type": issue.get("type", ""),
        "status": status.get("name", ""), "statusCategory": status.get("category", ""),
        "assignee": (issue.get("assignee") or {}).get("name", ""),
        "parent": {"key": (issue.get("parent") or {}).get("key", ""), "title": (issue.get("parent") or {}).get("title", "")},
        "updated": issue.get("updated", ""),
        "dod": checklist(issue.get("description", "")),
    })
    card["mrs"], card["gitlab"] = merge_requests(key)
    print(json.dumps(card, ensure_ascii=False))


if __name__ == "__main__":
    main()
