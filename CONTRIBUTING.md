# Contributing to Coral

We love your input! We want to make contributing to this project as easy and transparent as possible.

## Pull Requests

1. Fork the repo and create your branch from `main`.
2. If you've added code that should be tested, add tests.
3. If you've changed APIs, update the documentation.
4. Ensure the test suite passes (`pytest`).
5. Make sure your code lints (`flake8` and `black`).
6. Issue that pull request!

## Local Development Setup

To run the web dashboard locally with auto-reload for development:

```bash
# Clone the repository
git clone https://github.com/cdknorow/coral.git
cd coral

# Install in editable mode with development dependencies
pip install -e ".[dev]"

# Start the web dashboard with auto-reload
coral --reload
```

## Running Tests and Linting

We use `pytest` for testing, `black` for formatting, and `flake8` for linting.

```bash
# Run tests
pytest

# Format code
black .

# Lint code
flake8 .
```

## Code of Conduct

Please note that this project is released with a Contributor Code of Conduct. By participating in this project you agree to abide by its terms.

## Adding Support for New Agents

If you want to add native support for a new AI coding agent (e.g., Aider, OpenDevin, Cursor), we highly encourage it!
Please review `src/coral/PROTOCOL.md` and ensure the new agent can reliably emit the required `||PULSE:STATUS ...||` tracking tokens.
