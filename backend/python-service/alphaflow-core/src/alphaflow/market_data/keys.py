def indicator_key(exchange: str, market: str, symbol: str, interval: str) -> str:
    return f"{exchange_code(exchange)}:{market}:ind:{symbol}:{interval}"


def indicator_window_key(exchange: str, market: str, symbol: str, interval: str) -> str:
    return f"{exchange_code(exchange)}:{market}:indwin:{symbol}:{interval}"


def indicator_realtime_key(exchange: str, market: str, symbol: str, interval: str) -> str:
    return f"{exchange_code(exchange)}:{market}:indrt:{symbol}:{interval}"


def data_health_key(exchange: str, market: str, symbol: str, interval: str) -> str:
    return f"{exchange_code(exchange)}:{market}:health:{symbol}:{interval}"


def last_price_key(exchange: str, market: str, symbol: str) -> str:
    return f"{exchange_code(exchange)}:{market}:lp:{symbol}"


def mark_price_key(exchange: str, market: str, symbol: str) -> str:
    return f"{exchange_code(exchange)}:{market}:mp:{symbol}"


def kline_base_key(exchange: str, market: str, symbol: str, interval: str) -> str:
    return f"{exchange_code(exchange)}:{market}:k:{symbol}:{interval}"


def kline_index_key(exchange: str, market: str, symbol: str, interval: str) -> str:
    return kline_base_key(exchange, market, symbol, interval) + ":idx"


def kline_data_key(exchange: str, market: str, symbol: str, interval: str) -> str:
    return kline_base_key(exchange, market, symbol, interval) + ":data"


def exchange_code(exchange: str) -> str:
    if exchange == "binance":
        return "bn"
    return exchange
