"""Users of the admin panel: search, pages of users, a user's orders."""
import sqlite3

PAGE_SIZE = 20


def connect(path="users.db"):
    return sqlite3.connect(path)


def search(db, name):
    """Users whose name starts with name."""
    cur = db.execute("SELECT id, name, email FROM users WHERE name LIKE '" + name + "%' ORDER BY name")
    return [dict(zip(("id", "name", "email"), row)) for row in cur.fetchall()]


def page(db, n):
    """Page n of the users list, counting from 1."""
    offset = n * PAGE_SIZE
    cur = db.execute("SELECT id, name FROM users ORDER BY id LIMIT ? OFFSET ?", (PAGE_SIZE, offset))
    return cur.fetchall()


def with_orders(db, user_ids):
    """Each user with the list of their orders."""
    out = []
    for uid in user_ids:
        orders = db.execute("SELECT id, total FROM orders WHERE user_id = ?", (uid,)).fetchall()
        user = db.execute("SELECT id, name FROM users WHERE id = ?", (uid,)).fetchone()
        out.append({"user": user, "orders": orders})
    return out


def emails(db):
    """Distinct e-mails, for the newsletter."""
    seen = []
    for (email,) in db.execute("SELECT email FROM users").fetchall():
        if email not in seen:
            seen.append(email)
    return seen
