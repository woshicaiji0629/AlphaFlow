import json
import logging

from alphaflow.logging import JsonFormatter, LoggingConfig, setup_logging


def test_json_formatter_outputs_structured_record() -> None:
    record = logging.LogRecord(
        name="alphaflow",
        level=logging.INFO,
        pathname="/tmp/example.py",
        lineno=12,
        msg="strategy signal",
        args=(),
        exc_info=None,
    )
    record.service = "alphaflow-core"
    record.symbol = "ETHUSDT"

    payload = json.loads(JsonFormatter().format(record))

    assert payload["service"] == "alphaflow-core"
    assert payload["msg"] == "strategy signal"
    assert payload["symbol"] == "ETHUSDT"
    assert payload["source"]["file"] == "example.py"


def test_setup_logging_creates_file_handler(tmp_path) -> None:
    logger = setup_logging(
        LoggingConfig(
            output="file",
            dir=str(tmp_path),
            filename="alphaflow-core.log",
            max_size_mb=1,
            max_backups=1,
        )
    )

    logger.info("hello")

    assert (tmp_path / "alphaflow-core.log").exists()
