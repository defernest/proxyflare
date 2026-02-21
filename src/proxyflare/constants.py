from dataclasses import dataclass
from typing import Literal

__all__ = [
    "COMPATIBILITY_DATE",
    "CONTENT_TYPES",
    "DEFAULT_DEPLOY_CONCURRENCY",
    "DEFAULT_WORKER_TIMEOUT",
    "DEFAULT_WORKER_WAIT",
    "WORKER_META",
    "WORKER_TYPES",
    "WorkerMeta",
    "WorkerType",
]

# Cloudflare
COMPATIBILITY_DATE = "2024-04-01"

# Worker Types
WorkerType = Literal["python", "rust", "js", "ts"]
WORKER_TYPES: set[WorkerType] = {"python", "rust", "js", "ts"}

# Defaults
DEFAULT_DEPLOY_CONCURRENCY = 5
DEFAULT_WORKER_TIMEOUT = 10.0
DEFAULT_WORKER_WAIT = 2.0


@dataclass(frozen=True)
class WorkerMeta:
    """Metadata describing how a worker type is deployed to Cloudflare."""

    main_module: str
    source_file: str
    compatibility_flags: tuple[str, ...] = ()
    wasm_file: str | None = None


WORKER_META: dict[WorkerType, WorkerMeta] = {
    "python": WorkerMeta(
        main_module="worker.py",
        source_file="worker.py",
        compatibility_flags=("python_workers",),
    ),
    "js": WorkerMeta(
        main_module="worker.js",
        source_file="worker.js",
    ),
    "rust": WorkerMeta(
        main_module="worker.js",
        source_file="index.js",
        wasm_file="index_bg.wasm",
    ),
    "ts": WorkerMeta(
        main_module="worker.js",
        source_file="dist/worker.js",
    ),
}

CONTENT_TYPES: dict[str, str] = {
    "python": "text/x-python",
    "js": "application/javascript+module",
    "rust": "application/javascript+module",
    "wasm": "application/wasm",
    "ts": "application/javascript+module",
}
