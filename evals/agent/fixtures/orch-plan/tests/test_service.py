import unittest

from app import service, views


class Service(unittest.TestCase):
    def setUp(self):
        service._BOOKINGS.clear()
        service.book(1, 77, "Концерт", 2)

    def test_refund(self):
        self.assertEqual(service.refund(10, 1, 500).user_id, 77)

    def test_card(self):
        self.assertIn("покупатель #77", views.booking_card(1))

    def test_sync(self):
        self.assertEqual(service.sync(1)["owner"], 77)
