"""The order page: turns the API's JSON into HTML."""
import html


def money(x):
    return f"{x:.2f} ₽"


def render_order(resp):
    if "error" in resp:
        return "<p class=error>Заказ не найден</p>"
    rows = "".join(f"<li>{html.escape(i['title'])} × {i['qty']} — {money(i['price'] * i['qty'])}</li>" for i in resp["items"])
    return f"<h1>Заказ {resp['id']}</h1><ul>{rows}</ul><p class=total>Итого: {money(resp['total'])}</p>"
