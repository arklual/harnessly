import unittest

from billing.money import rub, share


class Money(unittest.TestCase):
    def test_rub(self):
        self.assertEqual(rub(12345), "123.45")
        self.assertEqual(rub(-5), "-0.05")

    def test_share(self):
        self.assertEqual(share(1000, 10), 100)
