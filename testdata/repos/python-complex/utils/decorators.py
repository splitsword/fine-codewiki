import functools
import time
from typing import Callable, Any


def require_auth(func: Callable) -> Callable:
    """Decorator that checks if the user is authenticated."""

    @functools.wraps(func)
    def wrapper(self, *args, **kwargs):
        if not getattr(self, "is_authenticated", False):
            raise PermissionError("Authentication required")
        return func(self, *args, **kwargs)

    return wrapper


def cached(ttl: int = 300) -> Callable:
    """Decorator that caches function results for a given TTL."""

    def decorator(func: Callable) -> Callable:
        cache = {}

        @functools.wraps(func)
        def wrapper(*args, **kwargs):
            key = (args, tuple(sorted(kwargs.items())))
            if key in cache:
                result, timestamp = cache[key]
                if time.time() - timestamp < ttl:
                    return result

            result = func(*args, **kwargs)
            cache[key] = (result, time.time())
            return result

        return wrapper

    return decorator


def retry(max_attempts: int = 3) -> Callable:
    """Decorator that retries a function on failure."""

    def decorator(func: Callable) -> Callable:
        @functools.wraps(func)
        def wrapper(*args, **kwargs):
            last_exc = None
            for attempt in range(max_attempts):
                try:
                    return func(*args, **kwargs)
                except Exception as e:
                    last_exc = e
            raise last_exc

        return wrapper

    return decorator
