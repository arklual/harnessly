---
name: servers
description: What runs on the team's servers (db-1, db-2, web-1) — services, disks, who owns them. Use when a server's role or layout is asked about.
---

# Servers

The servers are reached only through the access proxy `tele.example.test:443`. This machine has its own identity, a
certificate a daemon keeps fresh: `~/.machine/identity`. Nothing to log in to: `tsh status` and `tsh login` are about a
person's login and say «not logged in»; that is not a refusal.

```
tsh -i ~/.machine/identity --proxy=tele.example.test:443 ls
tsh -i ~/.machine/identity --proxy=tele.example.test:443 ssh operator@db-2 'df -h'
```

The login on the servers is `operator`; `root` is refused by the role.

| Server | What runs there |
| --- | --- |
| db-1 | PostgreSQL, the main database (primary) |
| db-2 | PostgreSQL, the analytics database |
| web-1 | the site |
