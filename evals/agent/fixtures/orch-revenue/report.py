"""python3 report.py 2026-10 — the month's revenue for the accountant."""
import sys

from billing.money import rub
from billing.revenue import revenue

if __name__ == "__main__":
    print(rub(revenue(sys.argv[1] if len(sys.argv) > 1 else "2026-10")))
