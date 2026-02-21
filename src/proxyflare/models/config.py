from typing import Literal

from pydantic import SecretStr
from pydantic_settings import BaseSettings, SettingsConfigDict

__all__ = ["Config"]


class Config(BaseSettings):
    """
    Configuration settings for Proxyflare.

    Loaded from environment variables with 'PROXYFLARE_' prefix or a .env file.
    """

    account_id: str
    """Cloudflare account ID."""

    api_token: SecretStr
    """Cloudflare API token."""

    worker_type: Literal["python", "rust", "js", "ts"] = "ts"
    """Default worker type to create if not specified."""

    worker_prefix: str = "proxyflare"
    """Prefix for worker names to isolate resources."""

    model_config = SettingsConfigDict(
        env_prefix="PROXYFLARE_",
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )
