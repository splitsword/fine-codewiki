from .base import BaseModel, TimestampMixin, SerializableMixin
from .order import Order
from ..utils.decorators import require_auth, cached


class User(BaseModel, TimestampMixin, SerializableMixin):
    """User entity with multiple inheritance and decorators."""

    def __init__(self, id: int, username: str, email: str):
        super().__init__(id)
        TimestampMixin.__init__(self)
        self.username = username
        self.email = email
        self._orders: list[Order] = []

    def validate(self) -> bool:
        return len(self.username) > 0 and "@" in self.email

    def to_dict(self) -> dict:
        base = super().to_dict()
        base.update({
            "username": self.username,
            "email": self.email,
        })
        return base

    @property
    def orders(self) -> list[Order]:
        return self._orders

    @require_auth
    def add_order(self, order: Order):
        self._orders.append(order)

    @cached(ttl=60)
    def get_order_count(self) -> int:
        return len(self._orders)


class AdminUser(User):
    """Admin user with elevated privileges."""

    def __init__(self, id: int, username: str, email: str):
        super().__init__(id, username, email)
        self.role = "admin"

    def ban_user(self, user: User) -> bool:
        user.is_banned = True
        return True
