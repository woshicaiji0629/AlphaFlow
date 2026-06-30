import json

from alphaflow.strategy import ExitReasonType, ExitRule, PositionSide, PositionState
from alphaflow.strategy.position_store import decode_position, encode_position, position_key


def test_position_key_includes_strategy_name() -> None:
    assert (
        position_key("binance", "um", "ETHUSDT", "trend_momentum")
        == "strategy:position:binance:um:ETHUSDT:trend_momentum"
    )


def test_position_round_trip_preserves_entry_reason() -> None:
    position = PositionState(
        exchange="binance",
        market="um",
        symbol="ETHUSDT",
        strategy_name="trend_momentum",
        side=PositionSide.LONG,
        size=0.7,
        entry_price="101.5",
        exit_rules=(
            ExitRule(
                rule_type=ExitReasonType.TAKE_PROFIT,
                reason="first resistance target",
                trigger_price="110",
                size_pct=0.5,
                metadata={"source": "strategy"},
            ),
        ),
        entry_time=1_700_000_000_000,
        entry_reason="ema bullish; buy signal opens long exposure",
        updated_at=1_700_000_060_000,
    )

    decoded = decode_position(json.dumps(encode_position(position)))

    assert decoded == position
