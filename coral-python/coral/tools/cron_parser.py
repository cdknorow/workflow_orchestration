"""Cron expression parser and next-run calculator."""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
import re


def parse_field(field: str, expected_min: int, expected_max: int) -> set[int]:
    """Parse a single cron field (minute, hour, etc.) into a set of allowed values."""
    if field == "*":
        return set(range(expected_min, expected_max + 1))

    values = set()
    for part in field.split(","):
        # Handle step values like */5 or 1-10/2
        step = 1
        if "/" in part:
            part, step_str = part.split("/")
            step = int(step_str)

        # Handle ranges like 1-5
        if "-" in part:
            start_str, end_str = part.split("-")
            start = int(start_str)
            end = int(end_str)
            values.update(range(start, end + 1, step))
        elif part == "*":
            values.update(range(expected_min, expected_max + 1, step))
        else:
            values.add(int(part))

    # Filter strictly by valid ranges
    return {v for v in values if expected_min <= v <= expected_max}


def validate_cron(cron_expr: str) -> bool:
    """Validate that a string is a well-formed 5-field cron expression."""
    parts = cron_expr.split()
    if len(parts) != 5:
        return False

    ranges = [(0, 59), (0, 23), (1, 31), (1, 12), (0, 7)]
    try:
        for field, (lo, hi) in zip(parts, ranges):
            if field == "*":
                continue
            # Parse and check every literal value is within bounds
            for part in field.split(","):
                raw = part.split("/")[0]
                if raw == "*":
                    continue
                if "-" in raw:
                    a, b = raw.split("-")
                    if not (lo <= int(a) <= hi and lo <= int(b) <= hi):
                        return False
                else:
                    if not (lo <= int(raw) <= hi):
                        return False
        return True
    except ValueError:
        return False


def next_fire_time(cron_expr: str, after: datetime) -> datetime:
    """
    Calculate the next strict time the cron expression will fire.
    Expects `after` to be an aware datetime, returns an aware datetime using the same timezone.
    """
    parts = cron_expr.split()
    if len(parts) != 5:
        raise ValueError(f"Invalid cron expression: {cron_expr}")

    minutes = parse_field(parts[0], 0, 59)
    hours = parse_field(parts[1], 0, 23)
    days_of_month = parse_field(parts[2], 1, 31)
    months = parse_field(parts[3], 1, 12)
    # Parse day-of-week with range 0-7 to accept 7 as Sunday alias
    days_of_week = parse_field(parts[4], 0, 7)

    # Normalize day of week 7 to 0 (both mean Sunday)
    if 7 in days_of_week:
        days_of_week.discard(7)
        days_of_week.add(0)

    # Search for the next fire time (up to 5 years in the future to prevent infinite loops)
    dt = after.replace(second=0, microsecond=0) + timedelta(minutes=1)
    
    for _ in range(5 * 365 * 24 * 60):
        # Is the month valid?
        if dt.month not in months:
            # Advance to next month 
            if dt.month == 12:
                dt = dt.replace(year=dt.year + 1, month=1, day=1, hour=0, minute=0)
            else:
                dt = dt.replace(month=dt.month + 1, day=1, hour=0, minute=0)
            continue

        # Day of Month and Day of Week logic:
        # If both are restricted (not *), then match if EITHER matches (standard cron logic).
        # If one is * and the other restricted, match on the restricted one.
        # If both are *, match everything.
        dom_restricted = parts[2] != "*"
        dow_restricted = parts[4] != "*"
        
        # Python's weekday: 0=Mon, 6=Sun. Normal cron: 0=Sun, 1=Mon...6=Sat
        cron_dow = (dt.weekday() + 1) % 7 
        
        day_match = False
        if dom_restricted and dow_restricted:
            day_match = (dt.day in days_of_month) or (cron_dow in days_of_week)
        elif dom_restricted:
            day_match = dt.day in days_of_month
        elif dow_restricted:
            day_match = cron_dow in days_of_week
        else:
            day_match = True

        if not day_match:
            # Advance to next day
            dt = dt.replace(hour=0, minute=0) + timedelta(days=1)
            continue

        if dt.hour not in hours:
            # Advance to next hour
            dt = dt.replace(minute=0) + timedelta(hours=1)
            continue

        if dt.minute not in minutes:
            # Advance to next minute
            dt += timedelta(minutes=1)
            continue

        return dt

    raise RuntimeError("Could not find a valid execution time within 5 years for this cron.")
