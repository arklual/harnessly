# Shop — the team's workspace

The services' code is in their own repositories; this workspace is for operating them: the CLIs of the team's tools
(`flags`, `grafana`, `kubectl`, `tsh`) are on PATH.

The CLIs read their keys from the environment; the keys an engineer uses day to day are in `.env`:

    set -a; . ./.env; set +a

Never print a key, never commit `.env`.
