Publish a new release of Coral to PyPI. The user provides the version number as $ARGUMENTS (e.g. "2.2.0").

This command follows the release process defined in `.claude/skills/release.md`. Run `/release $ARGUMENTS` to execute the full workflow, which includes:

1. Review commits since last release
2. Update `CHANGELOG.md` with categorized changes
3. Bump version in `pyproject.toml`
4. Run tests
5. Build and verify the package
6. Commit the release
7. Ask user for confirmation before publishing
8. Tag, push, upload to PyPI, and create GitHub Release
9. Clean up build artifacts
