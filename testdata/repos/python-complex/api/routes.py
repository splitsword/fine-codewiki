from ..services.user_service import UserService
from ..services.order_service import OrderService
from ..models.user import User
from ..models.order import Order
from typing import Dict, Any


class Router:
    """Simple API router for user and order endpoints."""

    def __init__(self, user_service: UserService, order_service: OrderService):
        self.user_service = user_service
        self.order_service = order_service
        self._routes: Dict[str, Any] = {}

    def register(self, path: str, handler: Any):
        self._routes[path] = handler

    def get_user(self, user_id: int) -> Dict[str, Any]:
        user = self.user_service.get_user(user_id)
        if not user:
            return {"error": "Not found"}
        return user.to_dict()

    def create_user(self, data: Dict[str, str]) -> Dict[str, Any]:
        user = self.user_service.create_user(
            data["username"],
            data["email"],
            data["password"]
        )
        return user.to_dict()

    def create_order(self, user_id: int, total: float) -> Dict[str, Any]:
        order = self.order_service.create_order(user_id, total)
        return order.to_dict()

    def list_user_orders(self, user_id: int) -> list:
        orders = self.order_service.list_user_orders(user_id)
        return [o.to_dict() for o in orders]
