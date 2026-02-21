"""Pydantic models for worker result JSON files."""

from typing import Literal

from pydantic import BaseModel, RootModel

__all__ = ["WorkerRecord", "WorkerResultFile"]


class WorkerRecord(BaseModel):
    """
    A single worker deployment record.

    Contains the name, URL, and metadata of a deployed worker.
    """

    name: str
    """The unique name of the worker."""

    url: str
    """The public URL for the worker."""

    type: Literal["python", "rust", "js", "ts"]
    """The type of worker (language or framework)."""

    created_at: float
    """Unix timestamp of when the worker was created."""


class WorkerResultFile(RootModel[list[WorkerRecord]]):
    """Typed model for proxyflare-workers.json — a list of WorkerRecord entries."""

    pass
