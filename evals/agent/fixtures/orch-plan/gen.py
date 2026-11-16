"""python3 gen.py — builds generated/client.py from api.yaml. Do not edit generated/ by hand."""
import os

HERE = os.path.dirname(os.path.abspath(__file__))


def build():
    out = ["# GENERATED from api.yaml by gen.py — do not edit", "from dataclasses import dataclass", ""]
    for line in open(os.path.join(HERE, "api.yaml"), encoding="utf-8"):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        name, fields = line.split(":", 1)
        out += ["", "@dataclass", f"class {name.strip()}:"]
        for f in fields.split(","):
            fname, ftype = f.strip().split(":")
            out.append(f"    {fname}: {ftype}")
        out.append("")
    return "\n".join(out)


if __name__ == "__main__":
    with open(os.path.join(HERE, "generated", "client.py"), "w", encoding="utf-8") as f:
        f.write(build())
