# syntax=docker/dockerfile:1
FROM python:3.11-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /app

# Install system dependencies (tmux is required for the application logic)
RUN apt-get update && \
    apt-get install -y --no-install-recommends tmux git && \
    rm -rf /var/lib/apt/lists/*

# Copy the application
COPY pyproject.toml /app/
COPY src/ /app/src/

# Install the application
RUN pip install .

# The application listens on port 8420 by default
EXPOSE 8420

# Run the web server directly
CMD ["agent-fleet", "--host", "0.0.0.0", "--port", "8420"]
