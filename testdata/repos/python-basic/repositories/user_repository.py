"""User data access layer."""
from typing import List, Optional, Dict

from ..models.user import User


class UserRepository:
    """In-memory user repository for demonstration."""

    def __init__(self):
        self._users: Dict[int, User] = {}
        self._next_id = 1

    def save(self, user: User) -> User:
        """Save or update a user."""
        if user.id == 0:
            user.id = self._next_id
            self._next_id += 1
        self._users[user.id] = user
        return user

    def find_by_id(self, user_id: int) -> Optional[User]:
        """Find user by ID."""
        return self._users.get(user_id)

    def find_by_username(self, username: str) -> Optional[User]:
        """Find user by username."""
        for user in self._users.values():
            if user.username == username:
                return user
        return None

    def find_all(self) -> List[User]:
        """Get all users."""
        return list(self._users.values())
