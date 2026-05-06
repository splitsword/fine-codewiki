"""Base model with common functionality."""
from dataclasses import dataclass, asdict
from typing import Dict, Any


@dataclass
class BaseModel:
    """Base class for all domain models."""

    def to_dict(self) -> Dict[str, Any]:
        """Convert model to dictionary."""
        return asdict(self)

    def validate(self) -> bool:
        """Validate model state. Override in subclasses."""
        return True
