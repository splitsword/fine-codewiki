"""User model with authentication support."""
from dataclasses import dataclass
from typing import Optional
from datetime import datetime

from .base import BaseModel
from ..utils.crypto import hash_password, verify_password


@dataclass
class User(BaseModel):
    """Represents a user in the system."""

    id: int
    username: str
    email: str
    password_hash: str
    created_at: datetime
    is_active: bool = True

    @classmethod
    def create(cls, username: str, email: str, password: str) -> "User":
        """Create a new user with hashed password."""
        return cls(
            id=0,
            username=username,
            email=email,
            password_hash=hash_password(password),
            created_at=datetime.now(),
        )

    def authenticate(self, password: str) -> bool:
        """Verify the provided password."""
        return verify_password(password, self.password_hash)

    def deactivate(self) -> None:
        """Deactivate the user account."""
        self.is_active = False
