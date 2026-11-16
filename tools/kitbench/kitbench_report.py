"""Report of a kitbench run: part A from kits.json, part B from the eval runs; with --against, the two side by side."""
import collections, glob, json, os, re, statistics

NATIVE = set("button input select textarea table thead tbody tr td th form label h1 h2 h3 h4 h5 h6 p a img ul ol li nav "
             "header footer main section article aside svg hr details summary dialog progress".split())
BOARD = re.compile(r"\.(html|tsx|jsx)$")
TARGET = dict(react_components=90.0, agent_share=95.0, angular_components=85.0)


def runs_of(out):
    """Last result per (scenario, model) of every eval run made for this --out."""
    base = os.path.expanduser("~/kb/") + os.path.basename(os.path.abspath(out))
    last = {}
    for f in sorted(glob.glob(base + "/runs/*/results.jsonl")):
        for l in open(f):
            try:
                r = json.loads(l)
            except ValueError:
                continue
            last[(r["scenario"], r["model"], r.get("rep", 0))] = r
    return list(last.values())


def elements(r, ns):
    """(kit elements, native elements) over the boards the run left."""
    kit = native = tpl = 0
    for f in (r.get("facts") or {}).get("files") or []:
        path, text = f.get("path", ""), f.get("text") or ""
        if not path.startswith("design:") or not BOARD.search(path) or "/meta/" in path or "_board-template" in path:
            continue
        if path.endswith(".tsx") or path.endswith(".jsx"):
            imported = set()
            for m in re.finditer(r"import\s*\{([^}]*)\}\s*from\s*['\"][^'\"]+['\"]", text):
                imported |= {x.strip().split(" as ")[-1] for x in m.group(1).split(",") if x.strip()}
            kit += sum(1 for m in re.finditer(r"<([A-Z]\w*)", text) if m.group(1) in imported)
        else:
            kit += len(re.findall(r"component-from-global-scope=[\"']%s\.[A-Z]" % re.escape(ns), text)) if ns else 0
            kit += len(re.findall(r"createElement\(\s*%s\.[A-Z]" % re.escape(ns), text)) if ns else 0
            kit += len(re.findall(r"<%s\.[A-Z]\w*" % re.escape(ns), text)) if ns else 0
        for m in re.finditer(r"<([a-z][a-z0-9]*)([\s>/][^>]*)?", text):
            if m.group(1) in NATIVE:
                if m.group(1) == "main" and "board" in (m.group(2) or ""):
                    tpl += 1  # the board template's own <main class="board">, not the agent's element
                else:
                    native += 1
    return kit, native, tpl


def screenshot_checked(r):
    for c in (r.get("facts") or {}).get("calls") or []:
        if c.get("error"):
            continue
        a = json.dumps(c.get("args") or "")
        if c.get("name") == "Bash" and re.search(r"harnessly design check", a) and "--strict" not in a.replace("harnessly design check --strict", ""):
            return True
        if c.get("name") == "Bash" and re.search(r"harnessly design check", a):
            return True
        if c.get("name") == "Read" and re.search(r"\.(png|jpe?g|webp)", a, re.I):
            return True
    return False


def asked(r):
    for c in (r.get("facts") or {}).get("calls") or []:
        a = json.dumps(c.get("args") or "")
        if "notice_ask" in (c.get("name") or "") or ("harnessly notice ask" in a and c.get("name") == "Bash"):
            return True
    return False


def score(r):
    for c in reversed((r.get("facts") or {}).get("checks") or []):
        for line in reversed((c.get("output") or "").splitlines()):
            if line.startswith("{") and '"project"' in line:
                try:
                    return json.loads(line)
                except ValueError:
                    pass
    return {}


def summarize(out):
    meta = {}
    try:
        meta = json.load(open(os.path.join(out, "agent", "kits.meta.json")))
    except OSError:
        pass
    rows = collections.defaultdict(list)
    for r in runs_of(out):
        m = re.match(r"kb-(.+)-(login|dashboard|product|event-form|settings|ask)$", r["scenario"])
        if not m or r["state"] == "infra":
            continue
        kit = m.group(1) + (" (без пина кита)" if m.group(2) == "ask" else "")
        k, n, tp = elements(r, meta.get(m.group(1), {}).get("namespace", ""))
        sc = score(r)
        rows[(kit, r["model"])].append(dict(screen=m.group(2), kit=k, native=n, tpl=tp, shot=screenshot_checked(r), strict=sc.get("strict"),
                                              judge=sc.get("judge"), asked=asked(r), usd=r.get("usd", 0), wall=r.get("wallS", 0)))
    res = {}
    for key, rs in rows.items():
        shares = [100.0 * x["kit"] / (x["kit"] + x["native"]) for x in rs if x["kit"] + x["native"]]
        old = [100.0 * x["kit"] / (x["kit"] + x["native"] + x["tpl"]) for x in rs if x["kit"] + x["native"] + x["tpl"]]
        sv = [x["strict"] for x in rs if x["strict"] is not None]
        jv = [x["judge"] for x in rs if x["judge"] is not None]
        res[key] = dict(runs=len(rs), noBoard=sum(1 for x in rs if x["kit"] + x["native"] == 0),
                        share=round(statistics.mean(shares), 1) if shares else 0.0,
                        shareOld=round(statistics.mean(old), 1) if old else 0.0,
                        strictZero=sum(1 for v in sv if v == 0), strictN=len(sv),
                        strictMean=round(statistics.mean(sv), 1) if sv else None,
                        shotPct=round(100.0 * sum(x["shot"] for x in rs) / len(rs), 0),
                        askedPct=round(100.0 * sum(x["asked"] for x in rs) / len(rs), 0),
                        judge=round(statistics.mean(jv), 1) if jv else None,
                        usd=round(sum(x["usd"] for x in rs), 2), rows=rs)
    return res


def n_of(k):
    n = k.get("components") or 0
    if k.get("framework") == "angular":  # an Angular kit draws its parts as cards of their own: they count too
        n += k.get("parts") or 0
    return n


def real_pct(k):
    """The kit's own count: components the kit carries; a component with a todo card is not drawn (no sample)."""
    n = n_of(k)
    return 100.0 * max(n - (k.get("todo") or 0), 0) / n if n else None


def part_a_table(kits):
    L = ["| Кит | Собрался | Компонентов в ките (Angular: с частями) | Не вошло в кит | Карточек проверено | Без ошибок рендера | Без todo, % от компонентов | Токенов | Карточек с todo | Ошибок |",
         "|---|---|---|---|---|---|---|---|---|---|"]
    for name, k in kits.items():
        if not k.get("built"):
            L.append("| %s (%s) | нет | – | – | – | – | – | – | – | %s |" % (name, k.get("package"), (k.get("errors") or [""])[0][:90].replace("|", "/")))
            continue
        rp = real_pct(k)
        L.append("| %s (%s) | да | %s | %s | %s | %s | %s | %s | %s | %s |" % (name, k.get("package"), n_of(k), len(k.get("notCarried") or []),
                 k.get("checkComponents", "–"), ("%s (%.0f%%)" % (k["drawn"], k["drawnPct"])) if k.get("drawnPct") is not None else "–",
                 ("%.0f%%" % rp) if rp is not None else "–", k.get("tokens"), k.get("todo", "–"),
                 k.get("errorProblems", "–") if k.get("errorProblems") is not None else "; ".join(k.get("errors", []))[:60]))
    return L


def part_b_table(res):
    L = ["| Кит | Модель | Прогонов | Доля элементов кита, % (по старому правилу с <main> шаблона) | strict: 0 находок | strict: среднее | Проверка скриншотом, % | Оценка судьи (1–10) | Спросил про кит, % | $ |",
         "|---|---|---|---|---|---|---|---|---|---|"]
    for (kit, model), v in sorted(res.items()):
        L.append("| %s | %s | %d (без доски: %d) | %s (%s) | %d из %d | %s | %s | %s | %s | %s |" % (kit, model, v["runs"], v["noBoard"], v["share"], v["shareOld"], v["strictZero"], v["strictN"],
                 v["strictMean"], v["shotPct"], v["judge"], v["askedPct"], v["usd"]))
    return L


def report(a):
    out = a.out
    L = ["# kitbench: " + os.path.basename(os.path.abspath(out)), ""]
    kp = os.path.join(out, "kits.json")
    kits = json.load(open(kp)) if os.path.exists(kp) else {}
    main = collections.OrderedDict((k, v) for k, v in kits.items() if not k.startswith("h-"))
    held = collections.OrderedDict((k, v) for k, v in kits.items() if k.startswith("h-"))
    if main:
        L += ["## Часть A — основной набор", ""] + part_a_table(main) + [""]
    if held:
        L += ["## Часть A — отложенный набор (рецептов под эти киты не делали)", ""] + part_a_table(held) + [""]
    res = summarize(out)
    if res:
        L += ["## Часть B — агент", ""] + part_b_table(res) + [""]
    if getattr(a, "against", None):
        L += ["## До → после", ""]
        bk = json.load(open(os.path.join(a.against.split(",")[0], "kits.json"))) if os.path.exists(os.path.join(a.against.split(",")[0], "kits.json")) else {}
        L += ["| Кит | Нарисовано до | после | Токены до | после | todo до | после |", "|---|---|---|---|---|---|---|"]
        for n in kits:
            b, k = bk.get(n, {}), kits[n]
            L.append("| %s | %s | %s | %s | %s | %s | %s |" % (n, pct(b), pct(k), b.get("tokens", "–"), k.get("tokens", "–"), b.get("todo", "–"), k.get("todo", "–")))
        L += [""]
        bres = {}
        for part in a.against.split(","):  # several runs: the later one wins for the same kit and model
            bres.update(summarize(part))
        L += ["| Кит | Модель | Доля кита до → после | strict среднее до → после | Скриншот до → после | Судья до → после |", "|---|---|---|---|---|---|"]
        for key in sorted(set(res) | set(bres)):
            b, k = bres.get(key), res.get(key)
            f = lambda x, f_: (str(x[f_]) if x else "–")
            L.append("| %s | %s | %s → %s | %s → %s | %s → %s | %s → %s |" % (key[0], key[1], f(b, "share"), f(k, "share"), f(b, "strictMean"), f(k, "strictMean"),
                     f(b, "shotPct"), f(k, "shotPct"), f(b, "judge"), f(k, "judge")))
        L += [""]
    L += ["Цели: React-киты ≥%.0f%% компонентов нарисовано, токены есть, todo пусто; агент ≥%.0f%% элементов из кита, strict без ошибок, проверка скриншотом в каждом прогоне; Angular ≥%.0f%%." %
          (TARGET["react_components"], TARGET["agent_share"], TARGET["angular_components"])]
    text = "\n".join(L) + "\n"
    open(os.path.join(out, "report.md"), "w").write(text)
    print(text)
    json.dump({"%s|%s" % k: {x: y for x, y in v.items() if x != "rows"} for k, v in res.items()}, open(os.path.join(out, "agent-summary.json"), "w"), indent=1)


def pct(k):
    return ("%d/%d (%.0f%%)" % (k["drawn"], k.get("checkComponents", 0), k["drawnPct"])) if k.get("drawnPct") is not None else "нет"
