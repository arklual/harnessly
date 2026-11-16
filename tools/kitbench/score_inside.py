#!/usr/bin/env python3
"""Runs inside a bench stand after the agent's turn (a `check:` of the scenario). Prints one JSON line:
strict findings of the project, whether the screenshot check can render it, and a model's grade of the picture.
The stand is deleted afterwards, so everything the report needs is taken here or from the run's facts."""
import glob, json, os, subprocess, sys

PROJECT = sys.argv[1]
work = os.getcwd()
stand = os.path.dirname(os.path.dirname(work))
env = dict(os.environ, HARNESSLY_DATA_DIR=stand + "/data", AGENT_DATA_DIR=stand + "/data",
           RUNTIME_DESIGN_LIBRARY_DIR=stand + "/design")
port = ""
try:
    import re
    m = re.search(r"127\.0\.0\.1:(\d{4,5})", open(stand + "/runtime.log", errors="replace").read())
    port = m.group(1) if m else ""
except OSError:
    pass
if port:
    env["HARNESSLY_URL"] = "http://127.0.0.1:" + port
out = {"project": PROJECT}
if len(sys.argv) > 2:  # keep the boards and the screenshots: the stand is deleted after the run
    import shutil
    dst = os.path.join(sys.argv[2], os.environ.get("EVAL_STARTED_AT", "0"))
    try:
        shutil.copytree(stand + "/design/" + PROJECT, dst, symlinks=True, ignore=shutil.ignore_patterns("node_modules"))
        out["kept"] = dst
    except OSError as e:
        out["kept"] = "failed: %s" % e


def sh(cmd, timeout, stdin=None):
    try:
        p = subprocess.run(cmd, env=env, cwd=work, capture_output=True, text=True, timeout=timeout, input=stdin)
        return p.returncode, p.stdout, p.stderr
    except subprocess.TimeoutExpired:
        return -9, "", "timeout"


rc, o, e = sh(["harnessly", "design", "check", PROJECT, "--strict", "--json"], 120)
out["strictExit"] = rc
try:
    d = json.loads(o)
    f = d.get("findings") or []
    out["strict"] = len(f)
    rules = {}
    for x in f:
        rules[x.get("rule", "?")] = rules.get(x.get("rule", "?"), 0) + 1
    out["strictRules"] = rules
except Exception:
    out["strict"] = None
    out["strictErr"] = (o + e)[-300:]
rc, o, e = sh(["harnessly", "design", "check", PROJECT], 300)
out["checkExit"] = rc
out["checkTail"] = (o + e)[-300:]
allshots = sorted(glob.glob(stand + "/design/" + PROJECT + "/.check/*.jp*g") + glob.glob(stand + "/design/" + PROJECT + "/.check/*.png"))
shots = [x for x in allshots if "phone" not in os.path.basename(x)] or allshots
out["shots"] = len(shots)
if shots:
    q = ("Open and look at the image file " + shots[-1] + " (use the Read tool). It is a mockup of a screen made with a UI kit. "
         "Grade it from 1 to 10 for visual quality: layout, spacing, hierarchy, no broken or unstyled parts, looks like a "
         "finished product. Reply with only the integer.")
    rc, o, e = sh(["claude", "-p", "--model", "haiku", "--allowedTools", "Read"], 180, q)
    digits = [t for t in o.replace("/", " ").split() if t.isdigit()]
    out["judge"] = int(digits[0]) if digits else None
    if out["judge"] is None:
        out["judgeErr"] = (o + e)[-200:]
print(json.dumps(out, ensure_ascii=False))
