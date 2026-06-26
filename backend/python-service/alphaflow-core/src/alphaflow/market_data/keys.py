def indicator_key(exchange: str, market: str, symbol: str, interval: str) -> str:
    return f"{exchange_code(exchange)}:{market}:ind:{symbol}:{interval}"


def data_health_key(exchange: str, market: str, symbol: str, interval: str) -> str:
    return f"{exchange_code(exchange)}:{market}:health:{symbol}:{interval}"


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
