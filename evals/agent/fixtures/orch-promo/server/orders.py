"""Orders API of the shop: every function returns what the HTTP layer sends as JSON."""
import json
import os

_DATA = os.path.join(os.path.dirname(__file__), "data.json")


def _load():
    with open(_DATA, encoding="utf-8") as f:
        return json.load(f)


def get_order(order_id):
    """GET /orders/<id>: {"id", "items": [{"title", "price", "qty"}], "total"}; {"error": "not_found"} when there is none."""
    order = _load()["orders"].get(str(order_id))
    if order is None:
        return {"error": "not_found"}
    total = round(sum(i["price"] * i["qty"] for i in order["items"]), 2)
    return {"id": order["id"], "items": order["items"], "total": total}
