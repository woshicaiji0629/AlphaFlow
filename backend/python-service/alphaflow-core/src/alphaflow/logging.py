from __future__ import annotations

import json
import logging
import sys
from dataclasses import dataclass
from datetime import UTC, datetime
from logging.handlers import RotatingFileHandler
from pathlib import Path
from types import MappingProxyType
from typing import Any, Literal

LogOutput = Literal["stdout", "stderr", "file"]
LogFormat = Literal["json", "text"]


@dataclass(frozen=True)
class LoggingConfig:
    service: str = "alphaflow-core"
    level: str = "info"
    format: LogFormat = "json"
    output: LogOutput = "file"
    dir: str = "../../logs/python-service"
    filename: str = "alphaflow-core.log"
    max_size_mb: int = 100
    max_backups: int = 10
    compress: bool = False


class JsonFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            "time": datetime.fromtimestamp(record.created, UTC).isoformat(),
            "level": record.levelname.lower(),
            "msg": record.getMessage(),
            "source": {
                "path": record.pathname,
                "file": Path(record.pathname).name,
                "function": record.funcName,
                "line": record.lineno,
            },
        }
        service = getattr(record, "service", None)
        if service:
            payload["service"] = service
        for key, value in structured_fields(record).items():
            payload[key] = value
        if record.exc_info:
            payload["error"] = self.formatException(record.exc_info)
        return json.dumps(payload, ensure_ascii=False, separators=(",", ":"))


def setup_logging(cfg: LoggingConfig | None = None) -> logging.Logger:
    config = cfg or LoggingConfig()
    logger = logging.getLogger()
    logger.handlers.clear()
    logger.setLevel(parse_level(config.level))

    handler = build_handler(config)
    if config.format == "json":
        handler.setFormatter(JsonFormatter())
    else:
        handler.setFormatter(logging.Formatter("%(asctime)s %(levelname)s %(name)s %(message)s"))
    logger.addHandler(ServiceHandler(handler, config.service))
    return logger


class ServiceHandler(logging.Handler):
    def __init__(self, handler: logging.Handler, service: str) -> None:
        super().__init__(handler.level)
        self._handler = handler
        self._service = service

    def emit(self, record: logging.LogRecord) -> None:
        record.service = self._service
        self._handler.emit(record)

    def flush(self) -> None:
        self._handler.flush()

    def close(self) -> None:
        self._handler.close()
        super().close()


def build_handler(config: LoggingConfig) -> logging.Handler:
    match config.output:
        case "stdout":
            return logging.StreamHandler(sys.stdout)
        case "stderr":
            return logging.StreamHandler(sys.stderr)
        case "file":
            log_path = Path(config.dir) / config.filename
            log_path.parent.mkdir(parents=True, exist_ok=True)
            return RotatingFileHandler(
                log_path,
                maxBytes=positive_or_default(config.max_size_mb, 100) * 1024 * 1024,
                backupCount=positive_or_default(config.max_backups, 10),
                encoding="utf-8",
            )


def parse_level(level: str) -> int:
    match level.lower():
        case "debug":
            return logging.DEBUG
        case "warn" | "warning":
            return logging.WARNING
        case "error":
            return logging.ERROR
        case _:
            return logging.INFO


def positive_or_default(value: int, fallback: int) -> int:
    if value <= 0:
        return fallback
    return value


def structured_fields(record: logging.LogRecord) -> MappingProxyType[str, Any]:
    reserved = set(logging.LogRecord("", 0, "", 0, "", (), None).__dict__)
    fields = {
        key: value
        for key, value in record.__dict__.items()
        if key not in reserved and key not in {"message", "asctime", "service"}
    }
    return MappingProxyType(fields)
