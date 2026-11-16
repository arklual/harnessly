"""Monthly revenue as the bank's statement shows it: payments of the month minus their refunds."""
import csv
import datetime as dt
import os

from billing import refunds as rf

DATA = os.path.join(os.path.dirname(__file__), "..", "data")


def _payments():
    with open(os.path.join(DATA, "payments.csv"), newline="", encoding="utf-8") as f:
        return [{"id": r["id"], "ts": int(r["paid_at_unix"]), "amount": int(r["amount_kop"])} for r in csv.DictReader(f)]


def month_of(ts):
    return dt.datetime.utcfromtimestamp(ts).strftime("%Y-%m")


def revenue(month):
    """Revenue of a month ("2026-10") in kopecks."""
    pays = [p for p in _payments() if month_of(p["ts"]) == month]
    back = rf.by_payment(rf.load(os.path.join(DATA, "refunds.csv")))
    return sum(p["amount"] for p in pays) - sum(back.get(p["id"], 0) for p in pays)
