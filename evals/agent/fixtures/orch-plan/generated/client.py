# GENERATED from api.yaml by gen.py — do not edit
from dataclasses import dataclass


@dataclass
class Booking:
    id: int
    user_id: int
    event: str
    seats: int


@dataclass
class Refund:
    id: int
    booking_id: int
    user_id: int
    amount: int
