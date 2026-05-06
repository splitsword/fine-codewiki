"""Logging utilities."""
import logging
from typing import Optional


def get_logger(name: str) -> logging.Logger:
    """Get a logger instance."""
    return logging.getLogger(name)
