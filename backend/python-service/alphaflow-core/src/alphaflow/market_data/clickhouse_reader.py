from __future__ import annotations

import asyncio
import json
import urllib.parse
import urllib.request
from typing import Any

from alphaflow.strategy.models import IndicatorSnapshot


class AsyncClickHouseIndicatorReader:
    def __init__(
        self,
        url: str,
        username: str = "alphaflow",
        password: str = "alphaflow",
        database: str = "alphaflow",
        limit: int = 200,
    ) -> None:
        self._url = url.rstrip("/")
        self._username = username
        self._password = password
        self._database = database
        self._limit = limit

    async def read_indicator_history(
        self,
        exchange: str,
        market: str,
        symbol: str,
        interval: str,
    ) -> tuple[IndicatorSnapshot, ...]:
        query = indicator_history_query(
            exchange=exchange,
            market=market,
            symbol=symbol,
            interval=interval,
            limit=self._limit,
        )
        payload = await asyncio.to_thread(self._post_query, query)
        snapshots = tuple(
            decode_indicator_row(line) for line in payload.splitlines() if line.strip()
        )
        return tuple(sorted(snapshots, key=lambda snapshot: snapshot.open_time))

    def _post_query(self, query: str) -> str:
        params = urllib.parse.urlencode({"database": self._database})
        request = urllib.request.Request(
            f"{self._url}/?{params}",
            data=query.encode("utf-8"),
            method="POST",
        )
        credentials = f"{self._username}:{self._password}"
        request.add_header("Authorization", "Basic " + basic_auth_token(credentials))
        with urllib.request.urlopen(request, timeout=10) as response:
            return response.read().decode("utf-8")


def indicator_history_query(
    exchange: str,
    market: str,
    symbol: str,
    interval: str,
    limit: int,
) -> str:
    return f"""
SELECT
    exchange,
    market,
    symbol,
    interval,
    open_time,
    close_time,
    values,
    signals,
    updated_at
FROM indicator_snapshots
WHERE exchange = {sql_string(exchange)}
  AND market = {sql_string(market)}
  AND symbol = {sql_string(symbol)}
  AND interval = {sql_string(interval)}
ORDER BY open_time DESC
LIMIT {int(limit)}
FORMAT JSONEachRow
"""


def decode_indicator_row(payload: str) -> IndicatorSnapshot:
    data = json.loads(payload)
    return IndicatorSnapshot(
        exchange=str(data["exchange"]),
        market=str(data["market"]),
        symbol=str(data["symbol"]),
        interval=str(data["interval"]),
        open_time=int(data["open_time"]),
        close_time=int(data["close_time"]),
        values=decode_mapping(data.get("values", {})),
        signals=decode_mapping(data.get("signals", {})),
        updated_at=int(data.get("updated_at", 0)),
    )


def decode_mapping(value: Any) -> dict[str, str]:
    if isinstance(value, str):
        decoded = json.loads(value) if value else {}
    else:
        decoded = value
    if not isinstance(decoded, dict):
        return {}
    return {str(key): str(item) for key, item in decoded.items()}


def sql_string(value: str) -> str:
    return "'" + value.replace("\\", "\\\\").replace("'", "\\'") + "'"


def basic_auth_token(credentials: str) -> str:
    import base64

    return base64.b64encode(credentials.encode("utf-8")).decode("ascii")
