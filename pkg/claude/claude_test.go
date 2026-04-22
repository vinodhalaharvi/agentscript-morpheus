package claude

import (
	"strings"
	"testing"
)

// helper: build a session with a predictable user/assistant turn structure.
func buildSession(turns int) *Session {
	s := &Session{}
	for i := 0; i < turns; i++ {
		s.messages = append(s.messages, claudeMessage{
			Role:    "user",
			Content: "ask-" + string(rune('A'+i)),
		})
		s.messages = append(s.messages, claudeMessage{
			Role:    "assistant",
			Content: strings.Repeat("X", 5000), // simulate a large proposal
		})
	}
	return s
}

func TestCompactKeepsLastAssistantVerbatim(t *testing.T) {
	s := buildSession(4)
	s.CompactOldAssistantMessages(1)

	// The most recent assistant message must be unchanged.
	last := s.messages[len(s.messages)-1]
	if last.Role != "assistant" {
		t.Fatalf("expected last message to be assistant, got %s", last.Role)
	}
	if len(last.Content) != 5000 {
		t.Errorf("last assistant message was modified: got %d chars, want 5000", len(last.Content))
	}
}

func TestCompactElidesOlderAssistantMessages(t *testing.T) {
	s := buildSession(4)
	s.CompactOldAssistantMessages(1)

	// Older assistant messages (indices 1, 3, 5) should have been elided.
	// Most recent assistant is at index 7 (last).
	elidedCount := 0
	for i, m := range s.messages {
		if m.Role != "assistant" {
			continue
		}
		if i == len(s.messages)-1 {
			continue // skip the preserved one
		}
		if !strings.HasPrefix(m.Content, "[prior response elided") {
			t.Errorf("assistant message at index %d was not elided: %q", i, m.Content[:40])
		} else {
			elidedCount++
		}
	}
	if elidedCount != 3 {
		t.Errorf("want 3 elided assistant messages, got %d", elidedCount)
	}
}

func TestCompactLeavesUserMessagesAlone(t *testing.T) {
	s := buildSession(4)
	s.CompactOldAssistantMessages(1)

	for i, m := range s.messages {
		if m.Role != "user" {
			continue
		}
		if strings.HasPrefix(m.Content, "ask-") == false {
			t.Errorf("user message at %d was modified: %q", i, m.Content)
		}
	}
}

func TestCompactNoOpWhenAssistantCountBelowThreshold(t *testing.T) {
	s := buildSession(2) // 2 assistant messages total
	s.CompactOldAssistantMessages(3)

	for i, m := range s.messages {
		if m.Role != "assistant" {
			continue
		}
		if len(m.Content) != 5000 {
			t.Errorf("assistant at %d was modified when keepLast > count: got %d chars", i, len(m.Content))
		}
	}
}

func TestCompactKeepLastClampedToOne(t *testing.T) {
	s := buildSession(3)
	s.CompactOldAssistantMessages(0) // should be clamped to 1

	// Most recent assistant must still be intact.
	last := s.messages[len(s.messages)-1]
	if len(last.Content) != 5000 {
		t.Errorf("keepLast=0 should clamp to 1 and preserve most recent; got %d chars", len(last.Content))
	}
}
