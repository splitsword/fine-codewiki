"""Cryptographic utilities."""
import hashlib
import secrets


def hash_password(password: str) -> str:
    """Hash a password using PBKDF2."""
    salt = secrets.token_hex(16)
    hash_value = hashlib.pbkdf2_hmac("sha256", password.encode(), salt.encode(), 100000)
    return f"{salt}${hash_value.hex()}"


def verify_password(password: str, password_hash: str) -> bool:
    """Verify a password against its hash."""
    salt, stored_hash = password_hash.split("$")
    hash_value = hashlib.pbkdf2_hmac("sha256", password.encode(), salt.encode(), 100000)
    return hash_value.hex() == stored_hash
