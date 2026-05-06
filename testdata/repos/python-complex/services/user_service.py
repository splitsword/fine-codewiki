from ..models.user import User, AdminUser
from ..models.order import Order
from ..utils.decorators import cached, retry
from ..repositories.user_repository import UserRepository
from typing import Optional, List


class UserService:
    """Service layer for user operations with repository pattern."""

    def __init__(self, repository: UserRepository):
        self.repository = repository

    @cached(ttl=120)
    def get_user(self, user_id: int) -> Optional[User]:
        return self.repository.find_by_id(user_id)

    @retry(max_attempts=3)
    def create_user(self, username: str, email: str, password: str) -> User:
        user = User(id=0, username=username, email=email)
        return self.repository.save(user)

    def list_users(self) -> List[User]:
        return self.repository.find_all()

    def get_user_orders(self, user_id: int) -> List[Order]:
        user = self.get_user(user_id)
        if user:
            return user.orders
        return []

    def promote_to_admin(self, user: User) -> AdminUser:
        admin = AdminUser(user.id, user.username, user.email)
        return self.repository.save(admin)
