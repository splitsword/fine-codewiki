from ..models.order import Order
from ..models.user import User
from ..services.user_service import UserService
from ..utils.decorators import retry
from typing import List, Optional


class OrderService:
    """Service layer for order operations with cross-service dependency."""

    def __init__(self, user_service: UserService):
        self.user_service = user_service

    @retry(max_attempts=3)
    def create_order(self, user_id: int, total: float) -> Order:
        user = self.user_service.get_user(user_id)
        if not user:
            raise ValueError(f"User {user_id} not found")
        order = Order(id=0, user=user, total=total)
        user.add_order(order)
        return order

    def cancel_order(self, order: Order) -> bool:
        return order.cancel()

    def list_user_orders(self, user_id: int) -> List[Order]:
        return self.user_service.get_user_orders(user_id)

    def get_order_user(self, order: Order) -> Optional[User]:
        return order.user
