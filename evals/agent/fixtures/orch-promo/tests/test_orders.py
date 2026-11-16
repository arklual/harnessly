import unittest

from server import orders
from web import render


class OrderPage(unittest.TestCase):
    def test_order_page(self):
        page = render.render_order(orders.get_order(1))
        self.assertIn("Итого: 1000.00 ₽", page)

    def test_missing(self):
        self.assertIn("не найден", render.render_order(orders.get_order(99)))


if __name__ == "__main__":
    unittest.main()
