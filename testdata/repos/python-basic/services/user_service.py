"""User management service."""
from typing import List, Optional

from ..models.user import User
from ..repositories.user_repository import UserRepository
from ..utils.logger import get_logger

logger = get_logger(__name__)


class UserService:
    """Handles user registration, authentication, and management."""

    def __init__(self, repository: UserRepository):
        self._repository = repository

    def register(self, username: str, email: str, password: str) -> User:
        """Register a new user."""
        logger.info(f"Registering user: {username}")

        if self._repository.find_by_username(username):
            raise ValueError(f"Username '{username}' already exists")

        user = User.create(username, email, password)
        return self._repository.save(user)

    def authenticate(self, username: str, password: str) -> Optional[User]:
        """Authenticate a user by username and password."""
        user = self._repository.find_by_username(username)
        if user and user.authenticate(password):
            return user
        return None

    def list_users(self) -> List[User]:
        """List all active users."""
        return [u for u in self._repository.find_all() if u.is_active]
