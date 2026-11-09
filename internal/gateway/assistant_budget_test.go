package gateway

import (
	"sync"
	"sync/atomic"
	"testing"

	"cirrusux/internal/assistant"
)

func TestReserveAssistantIsAtomic(t *testing.T) {
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if st.ReserveAssistant(100, 500) {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	// Each reservation adds 100 and is allowed while the count is under 500.
	if ok.Load() != 5 {
		t.Errorf("%d reservations passed a 500 limit at 100 each, want 5", ok.Load())
	}
	if used := st.AssistantUsage(0); used != 500 {
		t.Errorf("used = %d", used)
	}
	// Settling refunds the difference.
	if used := st.AssistantUsage(-300); used != 200 {
		t.Errorf("after refund used = %d", used)
	}
	if !st.ReserveAssistant(10, 500) {
		t.Error("reservation refused with room left")
	}
}

func TestSettleDelta(t *testing.T) {
	if d := settleDelta(1000, assistant.Reply{TokensIn: 300, TokensOut: 50}, 9999); d != -650 {
		t.Errorf("reported usage: %d", d)
	}
	// No usage reported: (400+400)/4 = 200 tokens estimated.
	if d := settleDelta(1000, assistant.Reply{Text: string(make([]byte, 400))}, 400); d != -800 {
		t.Errorf("estimate: %d", d)
	}
	if d := settleDelta(10, assistant.Reply{Text: string(make([]byte, 400))}, 4000); d != 1090 {
		t.Errorf("estimate above the reservation: %d", d)
	}
}
