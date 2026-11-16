"""Part B of kitbench: a clean agent draws five ordinary screens in a Sinopia project that has a UI kit attached.

The scenarios are generated into <out>/agent/scenarios and run with `harnessly eval run` of the build under test, so
the product is exactly the --bin build (stand, sandbox, no memory, no CLAUDE.md: docs/agent-evals.md). The prompts do
not name the kit."""
import base64, glob, json, os, shutil, subprocess, sys, tarfile

HERE = os.path.dirname(os.path.abspath(__file__))
PROJECT = "cabinet"
# Kits of the agent part: name -> where the installable archive comes from. "folder": a `kit add` result of part A;
# "export": a ready `kit export` archive (a kit that is not public, kept outside the repository).
# Private kits come from the environment: KITBENCH_EXPORT_KITS="name=archive.tar.gz,name2=other.tar.gz" (archives in the --out dir's parent).
AGENT_KITS = [(n, "folder", None) for n in ("antd", "mui", "mantine9", "angular-material", "primeng")] + [
    (kv.split("=", 1)[0], "export", kv.split("=", 1)[1]) for kv in os.environ.get("KITBENCH_EXPORT_KITS", "").split(",") if "=" in kv]

PROMPTS = [
    ("login", "В проекте Sinopia Design «cabinet» нарисуй экран входа в личный кабинет: почта, пароль, «Запомнить меня», кнопка «Войти», ссылка «Забыли пароль?»."),
    ("dashboard", "В проекте Sinopia Design «cabinet» нарисуй дашборд продаж: четыре метрики сверху, график выручки по дням и таблица последних заказов."),
    ("product", "В проекте Sinopia Design «cabinet» нарисуй карточку товара: фото, название, цена, выбор размера, кнопка «В корзину» и вкладки с описанием и отзывами."),
    ("event-form", "В проекте Sinopia Design «cabinet» нарисуй форму создания мероприятия: название, даты, площадка, описание, тип билета, переключатель «Публичное» и кнопки «Сохранить» и «Отмена»."),
    ("settings", "В проекте Sinopia Design «cabinet» нарисуй настройки профиля с вкладками «Профиль», «Безопасность», «Уведомления»: аватар, имя, почта, переключатели уведомлений, кнопка сохранения."),
]


def kit_record(folder):
    d = json.load(open(os.path.join(folder, "design-system.json")))
    return d["kit"]["id"], d["kit"]["version"]


def kit_namespace(arc, a, name):
    folder = os.path.join(a.out, "kits", name)
    try:
        if os.path.exists(os.path.join(folder, "design-system.json")):
            return json.load(open(os.path.join(folder, "design-system.json")))["namespace"]
        with tarfile.open(arc) as t:
            m = sorted([x for x in t.getnames() if x.endswith("design-system.json")], key=len)[0]
            return json.load(t.extractfile(m))["namespace"]
    except Exception:
        return ""


def yq(s):
    return json.dumps(s, ensure_ascii=False)


def make_archive(name, how, extra, a, out, h, env, run):
    """-> (archive path, id, version, install command) or None."""
    d = os.path.join(out, "kits", name)
    os.makedirs(d, exist_ok=True)
    if how == "export":
        src = os.path.join(os.path.dirname(os.path.abspath(a.out)) if False else os.path.expanduser("~/hrn1561"), extra)
        if not os.path.exists(src):
            src = os.path.join(a.out, "..", extra)
        if not os.path.exists(src):
            print("kit", name, "skipped: no archive", extra, flush=True)
            return None
        dst = os.path.join(d, os.path.basename(src))
        shutil.copy(src, dst)
        with tarfile.open(dst) as t:
            m = [x for x in t.getnames() if x.endswith("design-system.json")]
            ds = json.load(t.extractfile(sorted(m, key=len)[0]))
        return dst, ds["kit"]["id"], ds["kit"]["version"], "import"
    folder = os.path.join(a.out, "kits", name)
    if not os.path.exists(os.path.join(folder, "design-system.json")):
        print("kit", name, "skipped: part A made no folder", flush=True)
        return None
    kid, ver = kit_record(folder)
    dst = os.path.join(d, "%s-%s.tar.gz" % (kid, ver))
    with tarfile.open(dst, "w:gz") as t:
        t.add(folder, arcname=".")
    return dst, kid, ver, "install"


def engine_file(a):
    # the board engine a new project gets (support.js): a fixture project without it renders blank and 404s on ./support.js
    for c in (os.environ.get("KITBENCH_ENGINE"), os.path.join(a.bin, "support.js")):
        if c and os.path.exists(c):
            return c
    raise SystemExit("kitbench: no board engine: put support.js beside the binaries or set KITBENCH_ENGINE")


def write_scenarios(a, KITS, env, run, h):
    out = os.path.join(a.out, "agent")
    sc = os.path.join(out, "scenarios")
    shutil.rmtree(sc, ignore_errors=True)
    os.makedirs(sc)
    score = base64.b64encode(open(os.path.join(HERE, "score_inside.py"), "rb").read()).decode()
    ids = []
    meta = {}
    only = a.only.split(",") if a.only else None
    for name, how, extra in AGENT_KITS:
        if only and name not in only:
            continue
        got = make_archive(name, how, extra, a, out, h, env, run)
        if not got:
            continue
        arc, kid, ver, mode = got
        meta[name] = {"id": kid, "version": ver, "namespace": kit_namespace(arc, a, name)}
        kd = os.path.join(sc, "kitpkg-" + name)
        os.makedirs(kd, exist_ok=True)
        shutil.copy(arc, os.path.join(kd, "kit.tar.gz"))
        engine = engine_file(a)
        proj = os.path.join(sc, "fx-" + name, PROJECT)
        os.makedirs(os.path.join(proj, "meta"))
        os.makedirs(os.path.join(proj, "project"))
        shutil.copy(engine, os.path.join(proj, "project", "support.js"))
        json.dump({"title": "Кабинет", "boards": []}, open(os.path.join(proj, "artifact.json"), "w"), ensure_ascii=False)
        json.dump({"kit": {"id": kid, "version": ver}}, open(os.path.join(proj, "meta", "project.json"), "w"))
        if mode == "install":
            inst = "harnessly design kit install %s %s kit.tar.gz --dir ../../data/design-kits" % (kid, ver)
        else:
            inst = "harnessly design kit import kit.tar.gz --dir ../../data/design-kits"
        # the "ask" variant: the project does not pin the kit, the kit is installed on the machine; does the agent ask and take it
        proj2 = os.path.join(sc, "fx-" + name + "-ask", PROJECT)
        os.makedirs(os.path.join(proj2, "meta"))
        os.makedirs(os.path.join(proj2, "project"))
        shutil.copy(engine, os.path.join(proj2, "project", "support.js"))
        json.dump({"title": "Кабинет", "boards": []}, open(os.path.join(proj2, "artifact.json"), "w"), ensure_ascii=False)
        json.dump({}, open(os.path.join(proj2, "meta", "project.json"), "w"))
        for pid, prompt in PROMPTS + [("ask", dict(PROMPTS)["dashboard"])]:
            if os.environ.get("KITBENCH_ONLY_ASK") == "1" and pid != "ask":
                continue
            fx = "fx-%s%s" % (name, "-ask" if pid == "ask" else "")
            sid = "kb-%s-%s" % (name, pid)
            ids.append(sid)
            chk = "echo %s | base64 -d > ../../score.py && python3 ../../score.py %s %s" % (score, PROJECT, os.path.join(os.path.abspath(out), "boards", sid))
            lines = [
                "id: " + sid,
                "title: " + yq("kitbench: %s, %s" % (name, pid)),
                "set: dev",
                "tags: [kitbench]",
                "timeout_s: 900",
                "setup:",
                "  workspace: [kitpkg-%s]" % name,
                "  run: " + yq(inst + " && rm -f kit.tar.gz"),
                "  design: [%s/%s]" % (fx, PROJECT),
            ] + (["  runtime_env: {RUNTIME_EVAL_NOTICE_FIRST: \"1\"}"] if pid == "ask" else []) + [
                "prompt: " + yq(prompt),
                "expect:",
                "  - name: did a screenshot check of the screen",
                "    any_of:",
                "      - {called: Bash, args: {command: '(?s).*harnessly design check.*'}}",
                "      - {called: Read, args: {file_path: '(?i).*\\.(png|jpe?g|webp)$'}}",
                "  - name: score (informational, always exits 0)",
                "    check: " + yq(chk + "; true"),
            ]
            open(os.path.join(sc, sid + ".yaml"), "w").write("\n".join(lines) + "\n")
    json.dump(meta, open(os.path.join(out, "kits.meta.json"), "w"), indent=1)
    return out, sc, ids


def part_b(a, KITS, env_for, run, jload):
    h = os.path.join(a.bin, "harnessly")
    env = env_for(a.bin)
    out, sc, ids = write_scenarios(a, KITS, env, run, h)
    # a stand's tmux socket path must stay under 107 bytes: the runs live in a short folder, not under --out
    root = os.path.expanduser("~/kb/") + os.path.basename(os.path.abspath(a.out))
    os.makedirs(root, exist_ok=True)
    tools = os.path.expanduser("~/agent-evals/tools/node_modules/.bin") + ":" + os.path.expanduser("~/agent-evals/tools/chrome")
    cmd = [h, "eval", "run", "--dir", sc, "--scenarios", ",".join(ids), "--models", a.models, "--repeats", str(a.repeats),
           "--parallel", "3", "--runtime", os.path.join(a.bin, "runtime"), "--tools", tools, "--login",
           os.path.expanduser("~/agent-evals/login"), "--root", root, "--setup-file",
           os.path.expanduser("~/agent-evals/setups/chrome.yaml"), "--port-base", str(getattr(a, "port_base", 19700)),
           "--retries", "1", "--max-runs", "200"]
    print(" ".join(cmd), flush=True)
    rc = subprocess.call(cmd, env=env, cwd=out)
    print("eval run exit", rc, flush=True)
    return rc

