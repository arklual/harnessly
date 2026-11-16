"""Partner library (vendored, do not modify): pushes bookings to the partner's CRM."""


def payload(booking):
    # The partner's CRM reads the owner from the attribute user_id of what it is given.
    return {"external_id": booking.id, "owner": booking.user_id, "title": booking.event, "qty": booking.seats}
