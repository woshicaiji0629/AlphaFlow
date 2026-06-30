from alphaflow.strategy.models import (
    ExitReasonType,
    ExitRule,
    PositionAction,
    PositionPlan,
    PositionSide,
    PositionState,
    Signal,
    SignalSide,
)


class PositionManager:
    def __init__(
        self,
        max_position_size: float = 1.0,
        min_confidence_to_open: float = 0.65,
        allow_short: bool = True,
    ) -> None:
        self._max_position_size = max_position_size
        self._min_confidence_to_open = min_confidence_to_open
        self._allow_short = allow_short

    def plan(
        self,
        strategy_name: str,
        signal: Signal | None,
        position: PositionState | None = None,
        current_price: str = "",
        exit_rules: tuple[ExitRule, ...] = (),
    ) -> PositionPlan:
        risk_exit = self._risk_exit(position, current_price)
        if risk_exit is not None:
            return risk_exit

        if signal is None or signal.side == SignalSide.HOLD:
            current_side = position.side if position is not None else PositionSide.FLAT
            current_size = position.size if position is not None else 0.0
            return PositionPlan(
                action=PositionAction.HOLD,
                target_side=current_side,
                target_size=current_size,
                reason="no actionable signal",
            )

        current = position or PositionState(
            exchange=signal.exchange,
            market=signal.market,
            symbol=signal.symbol,
            strategy_name=strategy_name,
        )
        target_size = self._target_size(signal.confidence)
        if signal.confidence < self._min_confidence_to_open:
            return PositionPlan(
                action=PositionAction.HOLD,
                target_side=current.side,
                target_size=current.size,
                reason="signal confidence below position threshold",
            )

        if signal.side == SignalSide.BUY:
            return self._plan_long(
                current,
                target_size,
                signal.reason,
                exit_rules,
            )
        if signal.side == SignalSide.SELL:
            return self._plan_short(
                current,
                target_size,
                signal.reason,
                exit_rules,
            )
        return PositionPlan(
            action=PositionAction.HOLD,
            target_side=current.side,
            target_size=current.size,
            reason="unsupported signal side",
        )

    def _plan_long(
        self,
        current: PositionState,
        target_size: float,
        signal_reason: str,
        exit_rules: tuple[ExitRule, ...],
    ) -> PositionPlan:
        if current.side == PositionSide.SHORT and current.size > 0:
            return PositionPlan(
                action=PositionAction.CLOSE_SHORT,
                target_side=PositionSide.FLAT,
                target_size=0.0,
                reason=f"{signal_reason}; buy signal conflicts with short position",
                exit_reason_type=ExitReasonType.STRATEGY,
            )
        if current.side == PositionSide.LONG and current.size > 0:
            return PositionPlan(
                action=PositionAction.HOLD,
                target_side=PositionSide.LONG,
                target_size=current.size,
                reason="long position already open",
            )
        return PositionPlan(
            action=PositionAction.OPEN_LONG,
            target_side=PositionSide.LONG,
            target_size=target_size,
            reason=f"{signal_reason}; buy signal opens long exposure",
            exit_rules=exit_rules,
        )

    def _plan_short(
        self,
        current: PositionState,
        target_size: float,
        signal_reason: str,
        exit_rules: tuple[ExitRule, ...],
    ) -> PositionPlan:
        if not self._allow_short:
            if current.side == PositionSide.LONG and current.size > 0:
                return PositionPlan(
                    action=PositionAction.CLOSE_LONG,
                    target_side=PositionSide.FLAT,
                    target_size=0.0,
                    reason=f"{signal_reason}; sell signal exits long exposure",
                    exit_reason_type=ExitReasonType.STRATEGY,
                )
            return PositionPlan(
                action=PositionAction.HOLD,
                target_side=current.side,
                target_size=current.size,
                reason="short exposure disabled",
            )
        if current.side == PositionSide.LONG and current.size > 0:
            return PositionPlan(
                action=PositionAction.CLOSE_LONG,
                target_side=PositionSide.FLAT,
                target_size=0.0,
                reason=f"{signal_reason}; sell signal conflicts with long position",
                exit_reason_type=ExitReasonType.STRATEGY,
            )
        if current.side == PositionSide.SHORT and current.size > 0:
            return PositionPlan(
                action=PositionAction.HOLD,
                target_side=PositionSide.SHORT,
                target_size=current.size,
                reason="short position already open",
            )
        return PositionPlan(
            action=PositionAction.OPEN_SHORT,
            target_side=PositionSide.SHORT,
            target_size=target_size,
            reason=f"{signal_reason}; sell signal opens short exposure",
            exit_rules=exit_rules,
        )

    def _target_size(self, confidence: float) -> float:
        size = self._max_position_size * max(0.0, min(1.0, confidence))
        return round(size, 6)

    def _risk_exit(
        self,
        position: PositionState | None,
        current_price: str,
    ) -> PositionPlan | None:
        if position is None or position.is_flat():
            return None
        price = optional_float(current_price)
        if price is None:
            return None
        for rule in position.exit_rules:
            if exit_rule_triggered(position.side, price, rule):
                action = (
                    PositionAction.CLOSE_LONG
                    if position.side == PositionSide.LONG
                    else PositionAction.CLOSE_SHORT
                )
                return close_plan(
                    action,
                    rule.reason,
                    rule.rule_type,
                    rule,
                    exit_size=exit_size(position.size, rule.size_pct),
                )
        return None


def close_plan(
    action: PositionAction,
    reason: str,
    exit_reason_type: ExitReasonType,
    triggered_exit_rule: ExitRule | None = None,
    exit_size: float = 0.0,
) -> PositionPlan:
    return PositionPlan(
        action=action,
        target_side=PositionSide.FLAT,
        target_size=0.0,
        reason=reason,
        exit_size=exit_size,
        exit_reason_type=exit_reason_type,
        triggered_exit_rule=triggered_exit_rule,
    )


def optional_float(value: str) -> float | None:
    if value.strip() == "":
        return None
    try:
        return float(value)
    except ValueError:
        return None


def exit_rule_triggered(side: PositionSide, price: float, rule: ExitRule) -> bool:
    trigger_price = optional_float(rule.trigger_price)
    if rule.rule_type == ExitReasonType.TRAILING_STOP:
        return trailing_stop_triggered(side, price, rule)
    if trigger_price is None:
        return False
    if side == PositionSide.LONG:
        if rule.rule_type == ExitReasonType.TAKE_PROFIT:
            return price >= trigger_price
        if rule.rule_type == ExitReasonType.STOP_LOSS:
            return price <= trigger_price
    if side == PositionSide.SHORT:
        if rule.rule_type == ExitReasonType.TAKE_PROFIT:
            return price <= trigger_price
        if rule.rule_type == ExitReasonType.STOP_LOSS:
            return price >= trigger_price
    return False


def trailing_stop_triggered(side: PositionSide, price: float, rule: ExitRule) -> bool:
    trail_pct = optional_float(rule.metadata.get("trail_pct", ""))
    reference_price = optional_float(rule.metadata.get("reference_price", ""))
    if trail_pct is None or reference_price is None or trail_pct <= 0:
        return False
    distance = reference_price * trail_pct / 100
    if side == PositionSide.LONG:
        return price <= reference_price - distance
    if side == PositionSide.SHORT:
        return price >= reference_price + distance
    return False


def exit_size(position_size: float, size_pct: float) -> float:
    pct = max(0.0, min(1.0, size_pct))
    return round(position_size * pct, 6)
