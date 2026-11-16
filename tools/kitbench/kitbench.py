#!/usr/bin/env python3
"""kitbench (HRN-1561): a repeatable before/after bench of UI kits in Sinopia Design. Run on the build machine.

  kitbench.py kits  --bin DIR --out DIR [--only a,b]     part A: `kit add` + `kit check --render --levels` per package
  kitbench.py agent --bin DIR --out DIR [--models haiku,sonnet] [--repeats 1]   part B: a clean agent draws 5 screens
  kitbench.py all   ...                                  both, then report
  kitbench.py report --out DIR [--against DIR]           markdown of one run, or a before/after table

--bin is a folder with `harnessly` and `runtime` built from the commit under test. Docs: tools/kitbench/README.md.
"""
import argparse, collections, json, os, re, shutil, subprocess, sys, time

HOME = os.path.expanduser("~")
CHROME = os.environ.get("KITBENCH_CHROME", HOME + "/agent-evals/tools/chrome/chrome")

# name -> (npm package, framework). A React 19 kit has a peer dependency on react ^19 only.
KITS = collections.OrderedDict([
    ("antd", ("antd", "react")),
    ("mui", ("@mui/material", "react")),
    ("chakra", ("@chakra-ui/react", "react")),
    ("mantine9", ("@mantine/core", "react19")),
    ("angular-material", ("@angular/material", "angular")),
    ("primeng", ("primeng", "angular")),
    ("heroui", ("@heroui/react", "react19")),
    ("taiga", ("@taiga-ui/core", "angular")),
    # the held-out set: kits nobody made recipes for; the honest number for an enterprise
    ("h-mantine", ("@mantine/core", "react19")),
    ("h-react-bootstrap", ("react-bootstrap", "react")),
    ("h-fluentui", ("@fluentui/react-components", "react19")),
    ("h-blueprint", ("@blueprintjs/core", "react")),
    ("h-radix-themes", ("@radix-ui/themes", "react")),
])
HOLDOUT = [k for k in KITS if k.startswith("h-")]


def env_for(binDir):
    shim = os.path.join(binDir, "..", "kitbench-shim")
    os.makedirs(shim, exist_ok=True)
    link = os.path.join(shim, "google-chrome")
    if not os.path.exists(link):
        os.symlink(CHROME, link)
    e = dict(os.environ)
    e["PATH"] = shim + ":" + e["PATH"]
    e["RUNTIME_DESIGN_CHROME"] = CHROME
    e["RUNTIME_DESIGN_CHROME_ARGS"] = "--no-sandbox"
    return e


def run(cmd, env, cwd, timeout):
    t = time.time()
    try:
        p = subprocess.run(cmd, env=env, cwd=cwd, capture_output=True, text=True, timeout=timeout)
        return p.returncode, p.stdout, p.stderr, time.time() - t
    except subprocess.TimeoutExpired as e:
        return -9, (e.stdout or b"").decode() if isinstance(e.stdout, bytes) else (e.stdout or ""), "timeout", time.time() - t


def jload(s):
    try:
        return json.loads(s)
    except Exception:
        return None


def part_a(a):
    h = os.path.join(a.bin, "harnessly")
    env = env_for(a.bin)
    kd = os.path.join(a.out, "kits")
    os.makedirs(kd, exist_ok=True)
    res = {}
    for name, (pkg, fw) in KITS.items():
        if a.only and name not in a.only.split(","):
            continue
        folder = os.path.join(kd, name)
        shutil.rmtree(folder, ignore_errors=True)
        r = dict(name=name, package=pkg, framework=fw, built=False, errors=[])
        rc, out, err, dt = run([h, "design", "kit", "add", "npm:" + pkg, folder, "--id", name, "--json"] + (["--namespace", "Tui"] if "taiga" in pkg else []), env, kd, 900)
        r["addSeconds"] = round(dt, 1)
        d = jload(out)
        if rc != 0 or not d or not os.path.exists(os.path.join(folder, "design-system.json")):
            r["errors"].append(("add exit %d: " % rc) + (err.strip().splitlines() or [""])[-1][:300] if err.strip() else "add exit %d" % rc)
            if out.strip() and not d:
                r["errors"].append(out.strip().splitlines()[-1][:300])
            res[name] = r
            print(name, "FAILED", r["errors"], flush=True)
            continue
        rep = d.get("report", {})
        r.update(built=True, version=rep.get("version"), components=rep.get("components", 0), parts=rep.get("parts", 0),
                 providers=rep.get("providers", 0), tokens=rep.get("tokens", 0), notCarried=rep.get("notCarried", []))
        rc, out, err, dt = run([h, "design", "kit", "check", folder, "--render", "--levels", "--json"], env, kd, 1500)
        r["checkSeconds"] = round(dt, 1)
        c = jload(out)
        if not c:
            r["errors"].append("check exit %d: %s" % (rc, (err.strip().splitlines() or [""])[-1][:300]))
            res[name] = r
            print(name, "built, check failed", r["errors"], flush=True)
            continue
        probs = c.get("problems", [])
        rules = collections.Counter(p["rule"] for p in probs)
        bad = {p["file"] for p in probs if p["rule"].startswith("render.") and p["level"] == "error"}
        total = c.get("components", 0)
        r.update(checkComponents=total, drawnBad=len(bad), drawn=max(total - len(bad), 0), level=c.get("level"),
                 todo=rules.get("component.todo", 0), errorProblems=sum(1 for p in probs if p["level"] == "error"),
                 rules=dict(rules))
        r["drawnPct"] = round(100.0 * r["drawn"] / total, 1) if total else 0.0
        r["errors"] += [p["file"].split(":")[-1] + ": " + p["message"][:160] for p in probs if p["level"] == "error"][:5]
        res[name] = r
        print(name, "drawn %d/%d tokens %s todo %d" % (r["drawn"], total, r["tokens"], r["todo"]), flush=True)
    path = os.path.join(a.out, "kits.json")
    old = jload(open(path).read()) if os.path.exists(path) else {}
    old.update(res)
    json.dump(old, open(path, "w"), indent=1, ensure_ascii=False)
    return old


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("cmd", choices=["kits", "agent", "all", "report"])
    ap.add_argument("--bin")
    ap.add_argument("--out", required=True)
    ap.add_argument("--only")
    ap.add_argument("--against")
    ap.add_argument("--models", default="haiku,sonnet")
    ap.add_argument("--repeats", type=int, default=1)
    ap.add_argument("--port-base", dest="port_base", type=int, default=19700)
    a = ap.parse_args()
    os.makedirs(a.out, exist_ok=True)
    if a.cmd in ("kits", "all"):
        part_a(a)
    if a.cmd in ("agent", "all"):
        import kitbench_agent
        kitbench_agent.part_b(a, KITS, env_for, run, jload)
    if a.cmd in ("report", "all"):
        import kitbench_report
        kitbench_report.report(a)


if __name__ == "__main__":
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    main()
