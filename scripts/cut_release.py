#!/usr/bin/env python3
"""Cut an xurl release: resolve the next version and promote CHANGELOG.md.

Used by .github/workflows/cut-release.yml (and runnable locally).
"""

from __future__ import annotations

import argparse
import datetime as dt
import re
import subprocess
import sys
from pathlib import Path

SEMVER_TAG_RE = re.compile(r"^v?(\d+)\.(\d+)\.(\d+)$")
# Do not let \s consume the trailing newline — that would insert an extra blank
# line when we rebuild the Unreleased section.
UNRELEASED_HEADING = re.compile(r"^## Unreleased[ \t]*$", re.MULTILINE)
VERSION_HEADING = re.compile(r"^## v(\d+\.\d+\.\d+)(?:\s|$)", re.MULTILINE)


def parse_semver(tag: str) -> tuple[int, int, int]:
    m = SEMVER_TAG_RE.match(tag.strip())
    if not m:
        raise ValueError(f"not a semver tag: {tag!r}")
    return int(m.group(1)), int(m.group(2)), int(m.group(3))


def format_semver(parts: tuple[int, int, int]) -> str:
    return f"{parts[0]}.{parts[1]}.{parts[2]}"


def bump_semver(version: str, bump: str) -> str:
    major, minor, patch = parse_semver(version)
    if bump == "major":
        return format_semver((major + 1, 0, 0))
    if bump == "minor":
        return format_semver((major, minor + 1, 0))
    if bump == "patch":
        return format_semver((major, minor, patch + 1))
    raise ValueError(f"unknown bump: {bump!r}")


def git_tags(repo: Path) -> list[str]:
    result = subprocess.run(
        ["git", "tag", "--list", "v*"],
        cwd=repo,
        check=True,
        capture_output=True,
        text=True,
    )
    return [line.strip() for line in result.stdout.splitlines() if line.strip()]


def latest_semver_tag(tags: list[str]) -> str | None:
    best: tuple[int, int, int] | None = None
    best_raw: str | None = None
    for tag in tags:
        try:
            parts = parse_semver(tag)
        except ValueError:
            continue
        if best is None or parts > best:
            best = parts
            best_raw = tag if tag.startswith("v") else f"v{tag}"
    return best_raw


def resolve_version(
    *,
    explicit: str | None,
    bump: str,
    tags: list[str],
) -> str:
    if explicit:
        version = explicit.strip().removeprefix("v")
        parse_semver(version)  # validate
        return version

    latest = latest_semver_tag(tags)
    if latest is None:
        if bump == "major":
            return "1.0.0"
        if bump == "minor":
            return "0.1.0"
        return "0.0.1"
    return bump_semver(latest, bump)


def unreleased_body(changelog: str) -> str:
    match = UNRELEASED_HEADING.search(changelog)
    if not match:
        raise ValueError("CHANGELOG.md has no '## Unreleased' heading")

    start = match.end()
    rest = changelog[start:]
    next_heading = re.search(r"^## ", rest, re.MULTILINE)
    body = rest[: next_heading.start()] if next_heading else rest
    return body.strip("\n")


def has_releasable_changes(body: str) -> bool:
    """True when Unreleased contains at least one bullet entry."""
    for line in body.splitlines():
        stripped = line.strip()
        if stripped.startswith("- ") or stripped.startswith("* "):
            return True
    return False


def promote_changelog(changelog: str, version: str, date: str) -> str:
    """Move the Unreleased body under ## vX.Y.Z - date; leave Unreleased empty."""
    parse_semver(version)
    if VERSION_HEADING.search(changelog):
        for m in VERSION_HEADING.finditer(changelog):
            if m.group(1) == version:
                raise ValueError(f"CHANGELOG.md already has a section for v{version}")

    body = unreleased_body(changelog)
    if not has_releasable_changes(body):
        raise ValueError(
            "CHANGELOG.md '## Unreleased' has no bullet entries to ship; "
            "add notes or pass --skip-changelog"
        )

    match = UNRELEASED_HEADING.search(changelog)
    assert match is not None
    start = match.end()
    rest = changelog[start:]
    next_heading = re.search(r"^## ", rest, re.MULTILINE)
    end = start + next_heading.start() if next_heading else len(changelog)

    # ## Unreleased
    # <blank>
    # ## vX.Y.Z - date
    # <body>
    # <blank>
    # ## previous...
    shipped = body.strip()
    replacement = f"\n\n## v{version} - {date}\n\n{shipped}\n\n"
    tail = changelog[end:].lstrip("\n")
    return changelog[: match.end()] + replacement + tail


def write_github_output(path: Path, values: dict[str, str]) -> None:
    with path.open("a", encoding="utf-8") as fh:
        for key, value in values.items():
            fh.write(f"{key}={value}\n")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--repo",
        type=Path,
        default=Path.cwd(),
        help="Repository root (default: cwd)",
    )
    parser.add_argument(
        "--changelog",
        type=Path,
        default=None,
        help="Path to CHANGELOG.md (default: <repo>/CHANGELOG.md)",
    )
    parser.add_argument(
        "--bump",
        choices=("patch", "minor", "major"),
        default="patch",
        help="Semver bump when --version is not set",
    )
    parser.add_argument(
        "--version",
        default="",
        help="Explicit version (e.g. 1.2.3). Overrides --bump.",
    )
    parser.add_argument(
        "--date",
        default="",
        help="Release date YYYY-MM-DD (default: today UTC)",
    )
    parser.add_argument(
        "--skip-changelog",
        action="store_true",
        help="Only resolve the version; do not edit CHANGELOG.md",
    )
    parser.add_argument(
        "--github-output",
        type=Path,
        default=None,
        help="Append version= / tag= lines for GitHub Actions",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print the resolved version / new changelog; do not write files",
    )
    args = parser.parse_args(argv)

    repo = args.repo.resolve()
    changelog_path = (args.changelog or (repo / "CHANGELOG.md")).resolve()
    date = args.date or dt.datetime.now(dt.timezone.utc).date().isoformat()

    tags = git_tags(repo) if (repo / ".git").exists() or (repo / ".git").is_file() else []
    # When .git is missing (rare), still allow explicit --version.
    if not tags and not args.version:
        # Still try git_tags — it works when cwd is a work tree.
        try:
            tags = git_tags(repo)
        except subprocess.CalledProcessError as exc:
            print(f"error: failed to list git tags: {exc}", file=sys.stderr)
            return 1

    try:
        version = resolve_version(
            explicit=args.version or None,
            bump=args.bump,
            tags=tags,
        )
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    tag = f"v{version}"
    print(f"version={version}")
    print(f"tag={tag}")

    changelog_changed = "false"
    if not args.skip_changelog:
        text = changelog_path.read_text(encoding="utf-8")
        try:
            updated = promote_changelog(text, version, date)
        except ValueError as exc:
            print(f"error: {exc}", file=sys.stderr)
            return 1
        if updated != text:
            changelog_changed = "true"
            if args.dry_run:
                print("--- CHANGELOG.md (dry-run) ---")
                print(updated)
            else:
                changelog_path.write_text(updated, encoding="utf-8")
                print(f"updated {changelog_path}")
        else:
            print("CHANGELOG.md already up to date")

    if args.github_output:
        write_github_output(
            args.github_output,
            {
                "version": version,
                "tag": tag,
                "changelog_changed": changelog_changed,
            },
        )

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
