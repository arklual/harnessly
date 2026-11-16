"""Bookings: create, refund, sync to the partner."""
from generated.client import Booking, Refund
from vendor import partner_sync

_BOOKINGS = {}


def book(booking_id, user_id, event, seats):
    b = Booking(id=booking_id, user_id=user_id, event=event, seats=seats)
    _BOOKINGS[booking_id] = b
    return b


def refund(refund_id, booking_id, amount):
    b = _BOOKINGS[booking_id]
    return Refund(id=refund_id, booking_id=booking_id, user_id=b.user_id, amount=amount)


def by_user(user_id):
    return [b for b in _BOOKINGS.values() if b.user_id == user_id]


def sync(booking_id):
    return partner_sync.payload(_BOOKINGS[booking_id])
