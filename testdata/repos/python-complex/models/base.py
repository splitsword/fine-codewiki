from abc import ABC, abstractmethod
from typing import Generic, TypeVar

T = TypeVar('T')


class BaseModel(ABC, Generic[T]):
    """Abstract base model with generic support."""

    def __init__(self, id: int):
        self.id = id

    @abstractmethod
    def validate(self) -> bool:
        raise NotImplementedError

    def to_dict(self) -> dict:
        return {"id": self.id}


class TimestampMixin:
    """Adds created_at / updated_at timestamps."""

    def __init__(self):
        self.created_at = None
        self.updated_at = None

    def touch(self):
        from datetime import datetime
        self.updated_at = datetime.utcnow()


class SerializableMixin:
    """Provides JSON serialization."""

    def to_json(self) -> str:
        import json
        return json.dumps(self.to_dict())
