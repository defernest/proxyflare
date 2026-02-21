#!/usr/bin/env python3
"""
Script to build TypeScript workers for Proxyflare.
Wraps 'npx esbuild' to bundle the TS file.
"""

import subprocess
import sys
from pathlib import Path


def main() -> None:
    ts_dir = Path(__file__).parent.parent / "workers" / "ts"

    if not ts_dir.exists():
        print(f"Error: TypeScript worker directory not found at {ts_dir}")
        sys.exit(1)

    try:
        print("Running tsc / build...")
        subprocess.run(
            [
                "npx",
                "-y",
                "esbuild",
                "worker.ts",
                "--bundle",
                "--outfile=dist/worker.js",
                "--format=esm",
                "--platform=browser",
                "--target=es2022",
                "--minify",
            ],
            cwd=ts_dir,
            check=True,
        )

        print("TS Build successful!")

    except subprocess.CalledProcessError as e:
        print(f"\nError building TS worker: {e}")
        sys.exit(1)
    except Exception as e:
        print(f"Unexpected error: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
