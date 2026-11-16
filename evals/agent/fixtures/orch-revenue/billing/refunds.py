"""Refunds. A payment may be refunded in several parts (several rows with the same payment_id): each part is its own
money movement and is subtracted once, in the month of the payment it refunds — that is how the bank's statement counts
them."""
import csv


def load(path):
    with open(path, newline="", encoding="utf-8") as f:
        return [{"id": r["id"], "payment_id": r["payment_id"], "amount": int(r["amount_kop"])} for r in csv.DictReader(f)]


def by_payment(refunds):
    out = {}
    for r in refunds:
        out[r["payment_id"]] = out.get(r["payment_id"], 0) + r["amount"]
    return out
