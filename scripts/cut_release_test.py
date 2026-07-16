#!/usr/bin/env python3
"""Unit tests for scripts/cut_release.py."""

from __future__ import annotations

import unittest

from cut_release import (
    bump_semver,
    has_releasable_changes,
    latest_semver_tag,
    promote_changelog,
    resolve_version,
    unreleased_body,
)


SAMPLE = """# Changelog

All user-visible bugs and enhancements should be recorded here.

## Unreleased

### Fixed

- [2026-07-15] example fix

## v1.2.2 - 2026-06-29

### Fixed

- [2026-06-29] older fix
"""


class CutReleaseTest(unittest.TestCase):
    def test_bump_semver(self) -> None:
        self.assertEqual(bump_semver("1.2.2", "patch"), "1.2.3")
        self.assertEqual(bump_semver("1.2.2", "minor"), "1.3.0")
        self.assertEqual(bump_semver("1.2.2", "major"), "2.0.0")
        self.assertEqual(bump_semver("v1.2.2", "patch"), "1.2.3")

    def test_latest_semver_tag(self) -> None:
        self.assertEqual(
            latest_semver_tag(["v1.2.0", "v1.2.2", "v1.1.9", "not-a-tag"]),
            "v1.2.2",
        )
        self.assertIsNone(latest_semver_tag(["foo", "bar"]))

    def test_resolve_version(self) -> None:
        self.assertEqual(
            resolve_version(explicit="1.2.3", bump="patch", tags=["v1.2.2"]),
            "1.2.3",
        )
        self.assertEqual(
            resolve_version(explicit="v1.2.3", bump="patch", tags=["v1.2.2"]),
            "1.2.3",
        )
        self.assertEqual(
            resolve_version(explicit=None, bump="patch", tags=["v1.2.2"]),
            "1.2.3",
        )
        self.assertEqual(
            resolve_version(explicit=None, bump="patch", tags=[]),
            "0.0.1",
        )

    def test_unreleased_body(self) -> None:
        body = unreleased_body(SAMPLE)
        self.assertIn("example fix", body)
        self.assertNotIn("v1.2.2", body)

    def test_has_releasable_changes(self) -> None:
        self.assertTrue(has_releasable_changes("### Fixed\n\n- fix\n"))
        self.assertFalse(has_releasable_changes("\n\n"))
        self.assertFalse(has_releasable_changes("### Fixed\n\n"))

    def test_promote_changelog(self) -> None:
        out = promote_changelog(SAMPLE, "1.2.3", "2026-07-15")
        self.assertIn("## Unreleased\n\n## v1.2.3 - 2026-07-15\n\n### Fixed\n", out)
        self.assertIn("- [2026-07-15] example fix", out)
        self.assertIn("## v1.2.2 - 2026-06-29", out)
        # Unreleased should no longer contain the shipped bullet.
        body = unreleased_body(out)
        self.assertFalse(has_releasable_changes(body))

    def test_promote_rejects_duplicate_version(self) -> None:
        with self.assertRaises(ValueError):
            promote_changelog(SAMPLE, "1.2.2", "2026-07-15")

    def test_promote_rejects_empty_unreleased(self) -> None:
        empty = SAMPLE.replace(
            "## Unreleased\n\n### Fixed\n\n- [2026-07-15] example fix\n",
            "## Unreleased\n\n",
        )
        with self.assertRaises(ValueError):
            promote_changelog(empty, "1.2.3", "2026-07-15")


if __name__ == "__main__":
    unittest.main()
