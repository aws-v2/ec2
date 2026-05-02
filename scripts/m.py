#!/usr/bin/env python3
"""
map_project.py
Prints the full directory tree of a Go project, skipping noise like
vendor/, node_modules/, .git/, and binary build artefacts.
Output is copyable plain text.

Usage:
    python3 map_project.py                  # maps current directory
    python3 map_project.py /path/to/project # maps a specific path
    python3 map_project.py --depth 4        # limit tree depth
"""

import os
import sys
import argparse
from pathlib import Path

# ── Directories to skip entirely ─────────────────────────────────────────────
SKIP_DIRS = {
    ".git", ".github", ".idea", ".vscode",
    "vendor", "node_modules", "__pycache__",
    "dist", "build", "bin", "tmp", ".cache",
}

# ── File extensions to skip (compiled / generated artefacts) ─────────────────
SKIP_EXTENSIONS = {
    ".exe", ".dll", ".so", ".dylib",
    ".test", ".out", ".o", ".a",
    ".pyc", ".pyo",
}

# ── Files to skip by exact name ───────────────────────────────────────────────
SKIP_FILES = {
    ".DS_Store", "Thumbs.db", ".env",
}

PIPE     = "│"
TEE      = "├──"
LAST     = "└──"
SPACER   = "    "
BRANCH   = "│   "


def should_skip_dir(name: str) -> bool:
    return name in SKIP_DIRS or name.startswith(".")


def should_skip_file(name: str) -> bool:
    if name in SKIP_FILES:
        return True
    ext = Path(name).suffix.lower()
    return ext in SKIP_EXTENSIONS


def walk(path: Path, prefix: str = "", depth: int = 0, max_depth: int = 0) -> list[str]:
    """Recursively build tree lines for *path*."""
    if max_depth and depth >= max_depth:
        return []

    try:
        entries = sorted(path.iterdir(), key=lambda e: (e.is_file(), e.name.lower()))
    except PermissionError:
        return [prefix + "[permission denied]"]

    dirs  = [e for e in entries if e.is_dir()  and not should_skip_dir(e.name)]
    files = [e for e in entries if e.is_file() and not should_skip_file(e.name)]
    items = dirs + files

    lines = []
    for i, entry in enumerate(items):
        is_last   = i == len(items) - 1
        connector = LAST if is_last else TEE
        lines.append(f"{prefix}{connector} {entry.name}")

        if entry.is_dir():
            extension = SPACER if is_last else BRANCH
            lines.extend(walk(entry, prefix + extension, depth + 1, max_depth))

    return lines


def build_legend() -> str:
    return """
─────────────────────────────────────────────
  RECOMMENDED STRUCTURE FOR THIS PROJECT
─────────────────────────────────────────────
  cmd/                 → main entrypoints (one dir per binary)
  internal/
    domain/            → pure domain models & errors (no deps)
    interfaces/        → repository & service interfaces
    application/       → use-case / service implementations
    infrastructure/
      postgres/        → postgres repository implementations
      libvirt/         → libvirt client & helpers
      minio/           → minio / S3 storage adapter
      messaging/       → NATS / pub-sub publisher
    api/
      http/
        handlers/      → Gin route handlers
        middleware/    → auth, logging, rate-limit
        router/        → route registration
      grpc/            → (future) gRPC handlers
  pkg/                 → reusable packages safe to import externally
  config/              → config structs & loaders (viper / env)
  migrations/          → SQL migration files
  scripts/             → build, deploy, seed scripts
  deployments/         → Docker, Compose, Swarm, K8s manifests
  docs/                → API docs, architecture notes
─────────────────────────────────────────────
"""


def main():
    parser = argparse.ArgumentParser(description="Map a Go project directory tree.")
    parser.add_argument("root", nargs="?", default=".", help="Root directory (default: current dir)")
    parser.add_argument("--depth", type=int, default=0, help="Max depth (0 = unlimited)")
    parser.add_argument("--no-legend", action="store_true", help="Skip the recommended structure legend")
    args = parser.parse_args()

    root = Path(args.root).resolve()
    if not root.exists():
        print(f"Error: path does not exist: {root}", file=sys.stderr)
        sys.exit(1)

    print("=" * 60)
    print(f"  PROJECT ROOT: {root}")
    print("=" * 60)
    print(root.name + "/")

    lines = walk(root, prefix="", depth=0, max_depth=args.depth)
    print("\n".join(lines))

    total_files = sum(1 for _ in root.rglob("*") if _.is_file() and not should_skip_file(_.name))
    total_dirs  = sum(1 for _ in root.rglob("*") if _.is_dir()  and not should_skip_dir(_.name))

    print()
    print("=" * 60)
    print(f"  {total_dirs} directories   {total_files} files")
    print("=" * 60)

    if not args.no_legend:
        print(build_legend())


if __name__ == "__main__":
    main()