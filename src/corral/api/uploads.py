"""API routes for file uploads (images for agent input)."""

from __future__ import annotations

import logging
import uuid
from datetime import datetime
from pathlib import Path

from fastapi import APIRouter, UploadFile, File

log = logging.getLogger(__name__)

router = APIRouter()

UPLOAD_DIR = Path.home() / ".corral" / "uploads"

ALLOWED_EXTENSIONS = {".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".tiff"}
MAX_FILE_SIZE = 20 * 1024 * 1024  # 20 MB

# Map content types to extensions for clipboard pastes that lack a filename
CONTENT_TYPE_TO_EXT = {
    "image/png": ".png",
    "image/jpeg": ".jpg",
    "image/gif": ".gif",
    "image/webp": ".webp",
    "image/bmp": ".bmp",
    "image/svg+xml": ".svg",
    "image/tiff": ".tiff",
}


@router.post("/api/upload")
async def upload_file(file: UploadFile = File(...)):
    """Upload an image file and return the absolute path for agent input."""
    filename = file.filename or ""
    ext = Path(filename).suffix.lower() if filename else ""

    # For clipboard pastes the filename is often empty or generic (e.g. "image.png").
    # Fall back to content_type to determine extension.
    if not ext and file.content_type:
        ext = CONTENT_TYPE_TO_EXT.get(file.content_type, "")

    if not ext or ext not in ALLOWED_EXTENSIONS:
        return {"error": f"Unsupported file type: {ext or '(none)'}. Allowed: {', '.join(sorted(ALLOWED_EXTENSIONS))}"}

    content = await file.read()
    if len(content) > MAX_FILE_SIZE:
        return {"error": f"File too large ({len(content)} bytes). Max: {MAX_FILE_SIZE} bytes"}

    UPLOAD_DIR.mkdir(parents=True, exist_ok=True)

    # For clipboard screenshots (generic name like "image.png"), use a timestamp-based name.
    # For real files, keep the original name with a short UUID prefix.
    # Always replace spaces with underscores so file paths work cleanly in tmux/CLI.
    is_clipboard = not filename or filename.lower() in ("image.png", "blob")
    if is_clipboard:
        ts = datetime.now().strftime("%Y%m%d_%H%M%S")
        safe_name = f"screenshot_{ts}_{uuid.uuid4().hex[:4]}{ext}"
    else:
        safe_name = f"{uuid.uuid4().hex[:8]}_{Path(filename).name}"
    safe_name = safe_name.replace(" ", "_")

    dest = UPLOAD_DIR / safe_name
    dest.write_bytes(content)

    log.info("Uploaded image: %s (%d bytes)", dest, len(content))
    return {"ok": True, "path": str(dest), "filename": filename or safe_name, "size": len(content)}
