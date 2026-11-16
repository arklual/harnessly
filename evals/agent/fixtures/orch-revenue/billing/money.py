"""Money is kept in kopecks (int) end to end; only the report formats it."""


def rub(kop):
    sign = "-" if kop < 0 else ""
    kop = abs(kop)
    return f"{sign}{kop // 100}.{kop % 100:02d}"


def share(kop, percent):
    # Banker's rounding is what the bank does with fractional kopecks.
    q, r = divmod(kop * percent, 100)
    if r * 2 > 100 or (r * 2 == 100 and q % 2 == 1):
        q += 1
    return q
