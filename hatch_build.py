import subprocess
import sys
from pathlib import Path
from typing import Any

from hatchling.builders.hooks.plugin.interface import (  # ty:ignore[unresolved-import]
    BuildHookInterface,
)


class CustomBuildHook(BuildHookInterface):
    def initialize(self, version: str, build_data: dict[str, Any]) -> None:
        print("Running CustomBuildHook to build workers...")  # noqa: T201

        project_root = Path.cwd()
        script_rust = project_root / "src" / "proxyflare" / "scripts" / "build_rust.py"
        script_ts = project_root / "src" / "proxyflare" / "scripts" / "build_ts.py"

        try:
            if not script_rust.exists():
                print(f"Warning: Rust build script not found at {script_rust}. Skipping.")  # noqa: T201
            else:
                print(f"Executing: {sys.executable} {script_rust}")  # noqa: T201
                subprocess.run(
                    [sys.executable, str(script_rust)], check=True, cwd=str(project_root)
                )  # noqa: S603

            if not script_ts.exists():
                print(f"Warning: TS build script not found at {script_ts}. Skipping.")  # noqa: T201
            else:
                print(f"Executing: {sys.executable} {script_ts}")  # noqa: T201
                subprocess.run([sys.executable, str(script_ts)], check=True, cwd=str(project_root))  # noqa: S603

            print("Build hook complete.")  # noqa: T201
        except subprocess.CalledProcessError as e:
            print(f"Error in build hook (compilation failed): {e}")  # noqa: T201
            raise RuntimeError("Worker build failed") from e
        except Exception as e:
            print(f"Unexpected error in build hook: {e}")  # noqa: T201
            raise RuntimeError("Unexpected build hook error") from e
