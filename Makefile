# AgentScript Makefile — Morpheus DSL frontend

# Load .env file if it exists
ifneq (,$(wildcard .env))
    include .env
    export
endif

# Binary name
BINARY=agentscript

# Build the binary
build:
	go build -o $(BINARY) .

# Run tests
test: build
	./$(BINARY) -e 'list "."'

# Run with expression (Morpheus DSL syntax)
run: build
	./$(BINARY) -e '$(EXPR)'

# Run file
run-file: build
	./$(BINARY) -f $(FILE)

# Interactive REPL
repl: build
	./$(BINARY) -i

# Natural language mode
natural: build
	./$(BINARY) -n "$(QUERY)"

# Run examples
example-simple: build
	./$(BINARY) -f examples/simple-research.as

example-parallel: build
	./$(BINARY) -f examples/competitor-analysis.as

example-nested: build
	./$(BINARY) -f examples/nested-parallel.as

example-multimodal: build
	./$(BINARY) -f examples/multimodal.as

# Clean build artifacts
clean:
	rm -f $(BINARY)
	rm -f *.md *.png *.mp4

# Install dependencies
deps:
	go mod tidy

# Show help
help:
	@echo "AgentScript — AI Agent Orchestration DSL (Morpheus frontend)"
	@echo ""
	@echo "Setup:"
	@echo "  1. Create .env file with: GEMINI_API_KEY=your-key"
	@echo "  2. Run: make build"
	@echo ""
	@echo "Targets:"
	@echo "  make build          - Build the binary"
	@echo "  make test           - Build and run simple test"
	@echo "  make repl           - Start interactive REPL"
	@echo "  make run EXPR='...' - Run DSL expression"
	@echo "  make run-file FILE=examples/simple-research.as"
	@echo "  make natural QUERY='compare google and microsoft'"
	@echo "  make example-simple   - Run simple example"
	@echo "  make example-parallel - Run parallel example"
	@echo "  make example-nested   - Run nested parallel example"
	@echo "  make clean          - Remove build artifacts"
	@echo "  make deps           - Install dependencies"
	@echo ""
	@echo "Morpheus DSL Syntax:"
	@echo "  Sequential  : search \"golang\" >=> summarize >=> save \"out.md\""
	@echo "  Fan-out     : ( search \"AWS\" >=> analyze <*> search \"GCP\" >=> analyze ) >=> merge"
	@echo "  Nested      : ( ( a <*> b ) >=> merge >=> ask \"...\" <*> c ) >=> merge"
	@echo ""
	@echo "Operators:"
	@echo "  >=>   Kleisli composition — sequential pipeline stage"
	@echo "  <*>   Fan-out — run branches concurrently (inside parentheses)"
	@echo "  ( )   Group  — enclose a fan-out block"

# Upload to a new private GitHub repo (requires: gh auth login)
git-upload:
	@if [ ! -d .git ]; then \
		git init && \
		git add . && \
		git commit -m "Initial commit — AgentScript with Morpheus DSL frontend"; \
	fi
	gh repo create agentscript-mcp-tools --private --source=. --remote=origin --push

.PHONY: build test run run-file repl natural example-simple example-parallel example-nested example-multimodal clean deps help git-upload
