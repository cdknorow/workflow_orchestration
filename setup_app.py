"""py2app configuration for building Coral.app (macOS menu bar app).

Usage:
    python setup_app.py py2app

Or use the build script:
    scripts/build_macos.sh
"""

import glob
import os

# Prevent setuptools from reading pyproject.toml dependencies, which
# py2app rejects via its "install_requires is no longer supported" check.
# We use a custom Distribution that always reports install_requires as empty.
from setuptools import setup, Distribution as _Distribution


class _Py2appDistribution(_Distribution):
    @property
    def install_requires(self):
        return []

    @install_requires.setter
    def install_requires(self, value):
        pass  # ignore any attempts to set it

APP = ["src/coral/tray.py"]

# Build DATA_FILES by appending — no fragile index-based mutation.
DATA_FILES = []

# Static assets (CSS, JS, images, icons)
static_files = (
    glob.glob("src/coral/static/*.css")
    + glob.glob("src/coral/static/*.js")
    + glob.glob("src/coral/static/*.png")
    + glob.glob("src/coral/static/*.ico")
)
DATA_FILES.append(("coral/static", static_files))

css_files = glob.glob("src/coral/static/css/*.css")
if css_files:
    DATA_FILES.append(("coral/static/css", css_files))

# Templates
DATA_FILES.append(("coral/templates", glob.glob("src/coral/templates/*.html")))

include_files = glob.glob("src/coral/templates/includes/*.html")
if include_files:
    DATA_FILES.append(("coral/templates/includes", include_files))

view_files = glob.glob("src/coral/templates/includes/views/*.html")
if view_files:
    DATA_FILES.append(("coral/templates/includes/views", view_files))

# Docs and guides
DATA_FILES.append(("coral", ["src/coral/PROTOCOL.md"]))
if os.path.exists("src/coral/messageboard/AGENT_GUIDE.md"):
    DATA_FILES.append(("coral/messageboard", ["src/coral/messageboard/AGENT_GUIDE.md"]))

# Bundled themes
theme_files = glob.glob("src/coral/bundled_themes/*.json")
if theme_files:
    DATA_FILES.append(("coral/bundled_themes", theme_files))

OPTIONS = {
    "argv_emulation": False,
    "iconfile": "Coral.icns",
    "plist": {
        "CFBundleName": "Coral",
        "CFBundleDisplayName": "Coral",
        "CFBundleIdentifier": "com.coral.dashboard",
        "CFBundleVersion": "2.2.1",
        "CFBundleShortVersionString": "2.2.1",
        "LSUIElement": True,  # Menu bar only — no Dock icon
        "NSHighResolutionCapable": True,
    },
    "packages": [
        "coral",
        "fastapi",
        "uvicorn",
        "jinja2",
        "aiosqlite",
        "httpx",
        "starlette",
        "anyio",
        "multipart",
        "rumps",
    ],
    "includes": [
        "coral.web_server",
        "coral.api.live_sessions",
        "coral.api.history",
        "coral.api.system",
        "coral.api.schedule",
        "coral.api.webhooks",
        "coral.api.tasks",
        "coral.api.uploads",
        "coral.api.themes",
        "coral.store",
        "coral.store.sessions",
        "coral.store.connection",
        "coral.store.git",
        "coral.store.tasks",
        "coral.store.schedule",
        "coral.store.webhooks",
        "coral.tools.session_manager",
        "coral.tools.tmux_manager",
        "coral.tools.log_streamer",
        "coral.tools.pulse_detector",
        "coral.tools.jsonl_reader",
        "coral.tools.cron_parser",
        "coral.tools.run_callback",
        "coral.tools.utils",
        "coral.background_tasks",
        "coral.agents",
        "coral.messageboard.store",
        "coral.messageboard.api",
        "coral.messageboard.cli",
    ],
}

setup(
    name="Coral",
    app=APP,
    data_files=DATA_FILES,
    options={"py2app": OPTIONS},
    distclass=_Py2appDistribution,
)
