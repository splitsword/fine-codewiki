from api.routes import Router
from services.user_service import UserService
from services.order_service import OrderService
from repositories.user_repository import UserRepository


def main():
    """Application entry point."""

    repository = UserRepository()
    user_service = UserService(repository)
    order_service = OrderService(user_service)
    router = Router(user_service, order_service)

    # Bootstrap sample data
    alice = user_service.create_user("alice", "alice@example.com", "secret")
    bob = user_service.create_user("bob", "bob@example.com", "secret")

    order_service.create_order(alice.id, 99.99)
    order_service.create_order(bob.id, 49.99)

    print(f"Users: {len(user_service.list_users())}")
    print(f"Alice orders: {len(order_service.list_user_orders(alice.id))}")


if __name__ == "__main__":
    main()
