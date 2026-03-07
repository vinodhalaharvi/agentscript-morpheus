#!/bin/bash
# AgentScript Comprehensive Test Suite
# Run this after: go mod tidy && go build -o agentscript .

set -e

echo "========================================"
echo "  AgentScript Test Suite"
echo "========================================"
echo ""

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

pass() { echo -e "${GREEN}✅ PASS${NC}: $1"; }
fail() { echo -e "${RED}❌ FAIL${NC}: $1"; exit 1; }
skip() { echo -e "${YELLOW}⏭️ SKIP${NC}: $1 (requires: $2)"; }

# Check if binary exists
if [ ! -f "./agentscript" ]; then
    echo "Building agentscript..."
    go build -o agentscript . || fail "Build failed"
fi

echo ""
echo "=== 1. Grammar Parsing Tests ==="
echo ""

# Test all 30 commands parse correctly
./agentscript 'search "test"' 2>/dev/null && pass "search" || fail "search"
./agentscript 'summarize' 2>/dev/null && pass "summarize" || fail "summarize"
./agentscript 'ask "test"' 2>/dev/null && pass "ask" || fail "ask"
./agentscript 'analyze' 2>/dev/null && pass "analyze" || fail "analyze"
echo "test" | ./agentscript 'save "test_output.txt"' 2>/dev/null && pass "save" || fail "save"
rm -f test_output.txt

./agentscript 'list "."' 2>/dev/null && pass "list" || fail "list"
./agentscript 'merge' 2>/dev/null && pass "merge" || fail "merge"

# Commands requiring APIs (test parsing only by checking help/error message)
./agentscript 'email "test@test.com"' 2>&1 | grep -q "GOOGLE_CREDENTIALS" && pass "email (parse)" || fail "email"
./agentscript 'calendar "test"' 2>&1 | grep -q "GOOGLE_CREDENTIALS" && pass "calendar (parse)" || fail "calendar"
./agentscript 'meet "test"' 2>&1 | grep -q "GOOGLE_CREDENTIALS" && pass "meet (parse)" || fail "meet"
./agentscript 'drive_save "test"' 2>&1 | grep -q "GOOGLE_CREDENTIALS" && pass "drive_save (parse)" || fail "drive_save"
./agentscript 'doc_create "test"' 2>&1 | grep -q "GOOGLE_CREDENTIALS" && pass "doc_create (parse)" || fail "doc_create"
./agentscript 'sheet_create "test"' 2>&1 | grep -q "GOOGLE_CREDENTIALS" && pass "sheet_create (parse)" || fail "sheet_create"
./agentscript 'sheet_append "test"' 2>&1 | grep -q "GOOGLE_CREDENTIALS" && pass "sheet_append (parse)" || fail "sheet_append"
./agentscript 'task "test"' 2>&1 | grep -q "GOOGLE_CREDENTIALS" && pass "task (parse)" || fail "task"
./agentscript 'contact_find "test"' 2>&1 | grep -q "GOOGLE_CREDENTIALS" && pass "contact_find (parse)" || fail "contact_find"
./agentscript 'youtube_search "test"' 2>&1 | grep -q "GOOGLE_CREDENTIALS" && pass "youtube_search (parse)" || fail "youtube_search"
./agentscript 'youtube_upload "test"' 2>&1 | grep -q "GOOGLE_CREDENTIALS" && pass "youtube_upload (parse)" || fail "youtube_upload"
./agentscript 'youtube_shorts "test"' 2>&1 | grep -q "GOOGLE_CREDENTIALS" && pass "youtube_shorts (parse)" || fail "youtube_shorts"

./agentscript 'image_generate "test"' 2>&1 | grep -q "GEMINI_API_KEY" && pass "image_generate (parse)" || fail "image_generate"
./agentscript 'video_generate "test"' 2>&1 | grep -q "GEMINI_API_KEY" && pass "video_generate (parse)" || fail "video_generate"
./agentscript 'images_to_video "test"' 2>&1 | grep -q "GEMINI_API_KEY" && pass "images_to_video (parse)" || fail "images_to_video"
./agentscript 'text_to_speech "test"' 2>&1 | grep -q "GEMINI_API_KEY" && pass "text_to_speech (parse)" || fail "text_to_speech"

./agentscript 'github_pages "test"' 2>&1 | grep -q "GITHUB_CLIENT" && pass "github_pages (parse)" || fail "github_pages"

echo ""
echo "=== 2. Pipeline Tests ==="
echo ""

./agentscript 'list "." -> summarize' 2>/dev/null && pass "simple pipeline" || fail "simple pipeline"
./agentscript 'list "." -> summarize -> save "pipe_test.txt"' 2>/dev/null && pass "3-stage pipeline" || fail "3-stage pipeline"
rm -f pipe_test.txt

echo ""
echo "=== 3. Parallel Tests ==="
echo ""

./agentscript 'parallel { list "." list ".." }' 2>/dev/null && pass "simple parallel" || fail "simple parallel"
./agentscript 'parallel { list "." list ".." } -> merge' 2>/dev/null && pass "parallel + merge" || fail "parallel + merge"
./agentscript 'parallel { list "." -> summarize list ".." -> summarize } -> merge' 2>/dev/null && pass "parallel pipelines" || fail "parallel pipelines"

echo ""
echo "=== 4. Nested Parallel Tests ==="
echo ""

./agentscript 'parallel { parallel { list "." list ".." } -> merge parallel { list "." list ".." } -> merge } -> merge' 2>/dev/null && pass "nested parallel" || fail "nested parallel"

echo ""
echo "=== 5. Confirm Command Test ==="
echo ""

# Test confirm with auto-yes
echo "y" | ./agentscript 'list "." -> confirm "Continue?" -> summarize' 2>/dev/null && pass "confirm (yes)" || fail "confirm (yes)"

# Test confirm with auto-no
echo "n" | ./agentscript 'list "." -> confirm "Continue?" -> summarize' 2>&1 | grep -q "cancelled" && pass "confirm (no)" || fail "confirm (no)"

echo ""
echo "========================================"
echo "  Basic Tests Complete!"
echo "========================================"
echo ""

# API-dependent tests
echo "=== 6. API-Dependent Tests (optional) ==="
echo ""

if [ -n "$GEMINI_API_KEY" ]; then
    echo "Testing with Gemini API..."
    ./agentscript 'ask "say hello"' 2>/dev/null && pass "ask (with API)" || fail "ask (with API)"
    ./agentscript 'search "test" -> summarize' 2>/dev/null && pass "search+summarize (with API)" || fail "search+summarize"
else
    skip "Gemini tests" "GEMINI_API_KEY"
fi

if [ -n "$GOOGLE_CREDENTIALS_FILE" ]; then
    echo "Google Workspace API available - run manual tests"
else
    skip "Google Workspace tests" "GOOGLE_CREDENTIALS_FILE"
fi

if [ -n "$GITHUB_CLIENT_ID" ] && [ -n "$GITHUB_CLIENT_SECRET" ]; then
    echo "GitHub API available - run manual tests"
else
    skip "GitHub tests" "GITHUB_CLIENT_ID + GITHUB_CLIENT_SECRET"
fi

echo ""
echo "========================================"
echo "  All Automated Tests Complete!"
echo "========================================"
