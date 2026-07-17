.PHONY: install install-local

install: install-local

# Build the host binary directly in ~/bin so the final rename stays on one
# filesystem and replaces an existing installation atomically.
install-local:
	mkdir -p "$$HOME/bin"; tmp=$$(mktemp "$$HOME/bin/.skills-pick.XXXXXX"); trap 'rm -f "$$tmp"' EXIT; go build -o "$$tmp" . && mv -f "$$tmp" "$$HOME/bin/skills-pick"
