import asyncio
import time
from pathlib import Path
from typing import TYPE_CHECKING, Annotated, cast

import typer
from rich.progress import Progress, SpinnerColumn, TextColumn

from proxyflare.cli.console import console, err_console
from proxyflare.cli.context import get_app_context
from proxyflare.cli.exceptions import ConfigError, WorkerError
from proxyflare.constants import DEFAULT_DEPLOY_CONCURRENCY, WorkerType
from proxyflare.models.deployment import DeploymentConfig
from proxyflare.models.worker_result import WorkerRecord, WorkerResultFile

if TYPE_CHECKING:
    from rich.progress import TaskID

    from proxyflare.cli.context import AppContext


async def _deploy_workers_parallel(
    ctx: "AppContext",
    script_content: str,
    worker_type: WorkerType,
    wasm_content: bytes | None,
    count: int,
    progress: Progress,
    task_id: "TaskID",
) -> list[WorkerRecord]:
    """
    Deploy multiple workers in parallel using asyncio.gather and a semaphore.

    Args:
        ctx: Application context containing the Cloudflare service.
        script_content: Source code of the worker.
        worker_type: Type of worker (python/rust/js).
        wasm_content: Optional WASM binary.
        count: Number of workers to deploy.
        progress: Rich progress bar instance.
        task_id: Rich task ID for progress tracking.

    Returns:
        A list of WorkerRecord objects for successful deployments.
    """
    sem = asyncio.Semaphore(DEFAULT_DEPLOY_CONCURRENCY)

    async def _deploy_one() -> WorkerRecord | None:
        name = ctx.service.generate_worker_name()
        async with sem:
            try:
                config = DeploymentConfig(
                    name=name,
                    script_content=script_content,
                    worker_type=worker_type,
                    wasm_content=wasm_content,
                )
                url = await ctx.service.deploy_worker(config)
                progress.advance(task_id)
                return WorkerRecord(
                    name=name,
                    url=url,
                    type=worker_type,
                    created_at=time.time(),
                )
            except Exception as e:
                # Log error but don't fail everything
                err_console.print(
                    f"[bold red]Error:[/bold red] Failed to create worker [bold]{name}[/bold]: {e}"
                )
                progress.advance(task_id)
                return None

    tasks = [_deploy_one() for _ in range(count)]
    settled = await asyncio.gather(*tasks)
    return [r for r in settled if r is not None]


async def _create_async(
    count: int,
    worker_type: str | None,
    result: Path,
) -> None:
    """
    Asynchronous implementation of the worker creation command.

    Args:
        count: Number of workers to create.
        worker_type: Optional override for the worker type.
        result: Path to save the deployment results.

    Raises:
        ConfigError: If configuration is invalid.
        WorkerError: If deployment fails.
    """
    async with get_app_context() as ctx:
        # Resolve worker type
        final_worker_type_str = worker_type or ctx.config.worker_type

        # Validate worker type
        if final_worker_type_str not in ["python", "rust", "js", "ts"]:
            raise ConfigError(
                f"Invalid worker type '{final_worker_type_str}'. "
                "Must be 'python', 'rust', 'js', or 'ts'."
            )

        final_worker_type = cast(WorkerType, final_worker_type_str)

        # Load Source
        try:
            script_content, wasm_content = ctx.service.get_worker_source(final_worker_type)
        except FileNotFoundError as e:
            raise WorkerError(str(e)) from e
        except Exception as e:
            raise WorkerError(
                f"Failed to load worker source: {e}\n"
                "Hint: For Rust workers, ensure artifacts are built (proxyflare init)."
            ) from e

        with Progress(
            SpinnerColumn(),
            TextColumn("[progress.description]{task.description}"),
            console=console,
        ) as progress:
            # Ensure Subdomain + Create Workers
            task_subdomain = progress.add_task("Checking subdomain...", total=None)

            try:
                subdomain = await ctx.service.ensure_subdomain()
                progress.update(task_subdomain, completed=True, visible=False)
                console.print(
                    f"[green]✓[/green] Using subdomain: [bold]{subdomain}.workers.dev[/bold]"
                )

                task_create = progress.add_task(f"Creating {count} workers...", total=count)
                results_data = await _deploy_workers_parallel(
                    ctx,
                    script_content,
                    final_worker_type,
                    wasm_content,
                    count,
                    progress,
                    task_create,
                )

            except RuntimeError as e:
                raise WorkerError(str(e)) from e
            except Exception as e:
                raise WorkerError(
                    "Could not complete deployment. Check your API token and account settings."
                ) from e

        # Save Results
        if results_data:
            result_file = WorkerResultFile(root=results_data)
            result.write_text(result_file.model_dump_json(indent=2))

            console.print(
                f"\n[bold green]Successfully created {len(results_data)} workers![/bold green]"
            )
            console.print(f"Results saved to: [bold]{result}[/bold]")
        else:
            console.print("\n[yellow]No workers were created.[/yellow]")


def create(
    count: Annotated[int, typer.Option(help="Number of workers to create")] = 1,
    worker_type: Annotated[
        str | None,
        typer.Option("--type", help="Type of worker (python/rust/js/ts). Defaults to config."),
    ] = None,
    result: Annotated[Path, typer.Option(help="Path to save result JSON")] = Path(
        "proxyflare-workers.json"
    ),
) -> None:
    """Deploy new Cloudflare proxy workers."""
    asyncio.run(_create_async(count, worker_type, result))
