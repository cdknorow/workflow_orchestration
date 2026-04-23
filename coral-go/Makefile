# Coral Go — development build targets

# Sync agent_docs to the static embed dir before any build
.PHONY: sync-docs
sync-docs:
	@../scripts/sync-agent-docs.sh

# Dev build (skips EULA + license)
.PHONY: dev
dev: sync-docs
	go build -tags dev -o coral ./cmd/coral/

# Production build
.PHONY: build
build: sync-docs
	go build -o coral ./cmd/coral/

# Dev build + run
.PHONY: run
run: dev
	./coral --host 127.0.0.1 --port 8420

# Test server (accessible from other machines)
.PHONY: test-server
test-server: dev
	./coral --host 0.0.0.0 --port 8450

# Run all tests
.PHONY: test
test: sync-docs
	go test ./...

# End-to-end frontend tests (headless Chrome + CDP). Requires node + Chrome.
.PHONY: test-frontend
test-frontend:
	@bash ../tests/frontend/run.sh
