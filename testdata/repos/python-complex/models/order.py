from .base import BaseModel, TimestampMixin
from .user import User
from typing import Optional


class Order(BaseModel, TimestampMixin):
    """Order entity with circular dependency to User."""

    def __init__(self, id: int, user: User, total: float):
        super().__init__(id)
        TimestampMixin.__init__(self)
        self.user = user
        self.total = total
        self.status = "pending"

    def validate(self) -> bool:
        return self.total > 0 and self.user is not None

    def to_dict(self) -> dict:
        base = super().to_dict()
        base.update({
            "user_id": self.user.id,
            "total": self.total,
            "status": self.status,
        })
        return base

    def cancel(self) -> bool:
        if self.status == "shipped":
            return False
        self.status = "cancelled"
        return True

    def get_user_email(self) -> Optional[str]:
        return self.user.email if self.user else None
