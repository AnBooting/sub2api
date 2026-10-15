//go:build unit

package service

import (
	"sync"
	"testing"
	"time"
)

// Regression for the ABBA lock inversion that independent review confirmed in the
// first K-6 implementation of GetPricingControlStatus.
//
// Writer order (pricing_service.go savePricingData / pricing_native_cny.go):
//
//	controlsMu -> mu
//
// The buggy status read took mu.RLock() and only then resolved the effective
// interval, which reads controls under controlsMu, i.e. mu -> controlsMu. A status
// read racing a background sync therefore deadlocked both sides permanently.
//
// This runs real readers against real writers; the inverted order deadlocks the pair
// and the test fails on the timeout instead of hanging forever.
func TestGetPricingControlStatusNoLockOrderInversion(t *testing.T) {
	svc := &PricingService{}
	svc.pricingData = map[string]*LiteLLMModelPricing{"seed": {}}
	controls := PricingControls{RemoteEnabled: true, NativeCNYEnabled: true, UpdateIntervalHours: 6}
	svc.controls = &controls

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer: takes the locks in the production order (controlsMu, then mu).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			svc.controlsMu.Lock()
			svc.mu.Lock()
			svc.lastUpdated = time.Now()
			svc.mu.Unlock()
			svc.controlsMu.Unlock()
		}
	}()

	// Readers: the status path under test.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				status := svc.GetPricingControlStatus()
				if got, ok := status["effective_interval_hours"].(int); !ok || got != 6 {
					t.Errorf("effective_interval_hours = %v, want 6", status["effective_interval_hours"])
					return
				}
			}
		}()
	}

	finished := make(chan struct{})
	go func() {
		time.Sleep(2 * time.Second)
		close(stop)
		wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("GetPricingControlStatus deadlocked against a concurrent pricing writer: lock order inverted (mu -> controlsMu vs the writer's controlsMu -> mu)")
	}
}
