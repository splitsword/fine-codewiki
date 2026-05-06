"""Application entry point."""
from services.user_service import UserService
from repositories.user_repository import UserRepository


def main():
    """Run the application."""
    repository = UserRepository()
    service = UserService(repository)

    user = service.register("alice", "alice@example.com", "secret123")
    print(f"Created user: {user.username}")

    authenticated = service.authenticate("alice", "secret123")
    if authenticated:
        print("Authentication successful")


if __name__ == "__main__":
    main()
