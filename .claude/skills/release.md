---
name: release
description: Prepare and publish a new release of Coral to PyPI with changelog, GitHub Release, and automated publishing.
user_invocable: true
arguments: version number (e.g. "2.2.0")
---

# Release Skill

Prepare and publish a new release of agent-coral. The user provides the version number as `$ARGUMENTS`.

Follow these steps in order. **Stop and ask for confirmation before any destructive or public-facing step** (pushing, uploading, creating a GitHub release).

## 1. Determine what changed

Review commits since the last release tag:

```bash
git log $(git describe --tags --abbrev=0 2>/dev/null || git rev-list --max-parents=0 HEAD)..HEAD --oneline --no-merges
```

Categorize changes into **Added**, **Changed**, **Fixed**, **Removed** buckets following [Keep a Changelog](https://keepachangelog.com/) conventions.

## 2. Update the changelog

Add a new section at the top of `CHANGELOG.md` (below the header) for the new version:

```markdown
## <version> — <YYYY-MM-DD>

### Added
- ...

### Fixed
- ...
```

Use today's date. Only include categories that have entries.

## 3. Bump the version

Update the `version` field in `pyproject.toml` to the provided version number.

## 4. Run the tests

```bash
.venv/bin/python -m pytest tests/ -v
```

If any tests fail, stop and fix them before proceeding.

## 5. Build and verify the package

```bash
.venv/bin/pip install build twine
.venv/bin/python -m build
.venv/bin/python -m twine check dist/agent_coral-<version>*
```

Verify the correct version appears in the filenames under `dist/`.

## 6. Commit the release

Stage `pyproject.toml` and `CHANGELOG.md`, then commit:

```
chore: bump version to <version>
```

## 7. Ask the user before publishing

Before proceeding, confirm with the user that they want to:
- Push the commit and tag to the remote
- Upload to PyPI
- Create a GitHub Release

**Do not proceed without explicit confirmation.**

## 8. Tag and push

```bash
git tag v<version>
git push && git push origin v<version>
```

## 9. Upload to PyPI

```bash
.venv/bin/python -m twine upload dist/agent_coral-<version>*
```

Note: If `.github/workflows/publish.yml` is configured with trusted publishing, the GitHub Release creation (step 10) will trigger an automatic publish. In that case, skip this manual upload step and tell the user.

## 10. Create GitHub Release

Extract the changelog section for this version and create a GitHub Release:

```bash
gh release create v<version> \
  --title "v<version>" \
  --notes "$(sed -n '/^## '"<version>"'/,/^## [0-9]/{/^## [0-9]/!p}' CHANGELOG.md)" \
  dist/agent_coral-<version>*
```

## 11. Clean up

```bash
rm -rf dist/ build/ src/*.egg-info
```

## 12. Summary

Report back with:
- The version released
- The git tag created
- Link to the GitHub Release
- Link to PyPI: https://pypi.org/project/agent-coral/
