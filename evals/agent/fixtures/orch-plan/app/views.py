"""What the admin page shows for a booking."""
from app import service


def booking_card(booking_id):
    b = service._BOOKINGS[booking_id]
    return f"Бронь {b.id}: {b.event}, мест {b.seats}, покупатель #{b.user_id}"


def user_page(user_id):
    return [booking_card(b.id) for b in service.by_user(user_id)]
