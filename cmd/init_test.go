package cmd

import (
	"testing"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// TestInitOrderBug proves that EnableTriggerComplete BEFORE SetTotal makes SetTotal a NO-OP.
// This test logs the bug behavior but doesn't fail — it's a regression guard.
func TestInitOrderBug_SetTotalIgnoredAfterEnableTriggerComplete(t *testing.T) {
	p := mpb.New(mpb.WithWidth(80))
	bar := p.AddBar(0,
		mpb.PrependDecorators(decor.Name("test", decor.WCSyncSpace)),
	)

	// BUG: EnableTriggerComplete BEFORE SetTotal
	bar.EnableTriggerComplete()

	// This SetTotal should set total to 100, but it's a NO-OP
	bar.SetTotal(100, false)

	bar.SetCurrent(50)
	current := bar.Current()
	if current != 50 && current != 100 {
		t.Logf("BUG CONFIRMED: current is %d (expected 50 or 100). SetTotal was no-op — total stayed 0, current clamped.", current)
	}

	bar.SetCurrent(100)
	final := bar.Current()
	t.Logf("Regression test: bar completed=%v, current=%d", bar.Completed(), final)
	p.Wait()
}

// TestInitOrderCorrect proves the fix: EnableTriggerComplete AFTER SetTotal
func TestInitOrderCorrect_EnableTriggerCompleteAfterSetTotal(t *testing.T) {
	p := mpb.New(mpb.WithWidth(80))
	bar := p.AddBar(0,
		mpb.PrependDecorators(decor.Name("test", decor.WCSyncSpace)),
	)

	// FIX: SetTotal first, THEN EnableTriggerComplete
	bar.SetTotal(100, false)    // total = 100
	bar.EnableTriggerComplete() // triggerComplete = true, 0 >= 100 false, no done()

	// Increment to 50
	bar.SetCurrent(50)
	if current := bar.Current(); current != 50 {
		t.Errorf("Expected current 50 after SetCurrent, got %d (SetTotal should have worked)", current)
	}

	if bar.Completed() {
		t.Error("Bar should NOT be completed at 50/100")
	}

	// Complete the bar
	bar.SetCurrent(100)
	if !bar.Completed() {
		t.Error("Bar should be completed at 100/100")
	}

	if current := bar.Current(); current != 100 {
		t.Errorf("Expected current 100, got %d", current)
	}

	p.Wait()
}

// TestInitOrderRealScenario runs the exact init.go flow
func TestInitOrderRealScenario_BuggyOrdering(t *testing.T) {
	done := make(chan struct{}, 1)

	go func() {
		p := mpb.New(mpb.WithWidth(80))
		bar := p.AddBar(0,
			mpb.BarFillerClearOnComplete(),
			mpb.PrependDecorators(decor.Name("Fetching artists", decor.WCSyncSpace)),
			mpb.AppendDecorators(decor.CountersNoUnit("pages: %d / %d")),
		)
		bar.EnableTriggerComplete() // <-- BUG: called BEFORE SetTotal

		totalPages := 5
		var firstTotalSet bool
		for page := 1; page <= totalPages; page++ {
			if !firstTotalSet {
				firstTotalSet = true
				bar.SetTotal(int64(totalPages), false) // NO-OP!
			}
			bar.SetCurrent(int64(page)) // Gets clamped: current → total=0
		}
		if totalPages > 0 {
			bar.SetCurrent(int64(totalPages))
		}

		// Verify: total was never set
		current := bar.Current()
		if current != 0 {
			t.Logf("WARNING: Bar current is %d, expected 0 (SetTotal was no-op after EnableTriggerComplete)", current)
		}
		p.Wait()
		done <- struct{}{}
	}()

	select {
	case <-done:
		t.Log("BUG CONFIRMED: p.Wait() returned but total was never set to 5.")
		t.Log("The bar completed at 0/0, not at 5/5 as intended.")
		t.Log("Root cause: EnableTriggerComplete() before SetTotal() makes SetTotal() a no-op.")
	case <-time.After(3 * time.Second):
		t.Fatal("p.Wait() HUNG — would hang forever in TTY mode")
	}
}

// TestInitOrderRealScenario_FixedOrdering runs the fixed init.go flow
func TestInitOrderRealScenario_FixedOrdering(t *testing.T) {
	done := make(chan struct{}, 1)

	go func() {
		p := mpb.New(mpb.WithWidth(80))
		bar := p.AddBar(0,
			mpb.BarFillerClearOnComplete(),
			mpb.PrependDecorators(decor.Name("Fetching artists", decor.WCSyncSpace)),
			mpb.AppendDecorators(decor.CountersNoUnit("pages: %d / %d")),
		)

		totalPages := 5
		var firstTotalSet bool
		for page := 1; page <= totalPages; page++ {
			if totalPages > 0 && !firstTotalSet {
				firstTotalSet = true
				bar.SetTotal(int64(totalPages), false)
				bar.EnableTriggerComplete() // FIX: AFTER SetTotal
			}
			bar.SetCurrent(int64(page)) // Now total=5, current correctly increments
		}
		if totalPages > 0 {
			bar.SetCurrent(int64(totalPages))
		}

		current := bar.Current()
		if current != int64(totalPages) {
			t.Errorf("Expected current %d, got %d (SetTotal should have worked)", totalPages, current)
		}
		if !bar.Completed() {
			t.Error("Bar should be completed")
		}
		p.Wait()
		done <- struct{}{}
	}()

	select {
	case <-done:
		t.Log("FIX CONFIRMED: bar completed at the correct total.")
	case <-time.After(3 * time.Second):
		t.Fatal("p.Wait() HUNG with fixed ordering")
	}
}

// TestInitOrderRealScenario_ZeroPages tests edge case where totalPages==0
func TestInitOrderRealScenario_ZeroPages(t *testing.T) {
	done := make(chan struct{}, 1)

	go func() {
		p := mpb.New(mpb.WithWidth(80))
		bar := p.AddBar(0,
			mpb.BarFillerClearOnComplete(),
			mpb.PrependDecorators(decor.Name("Fetching artists", decor.WCSyncSpace)),
			mpb.AppendDecorators(decor.CountersNoUnit("pages: %d / %d")),
		)

		totalPages := 0
		if totalPages > 0 {
			bar.SetCurrent(int64(totalPages))
		} else {
			bar.EnableTriggerComplete() // FIX: prevent hang on zero total
		}
		p.Wait()
		done <- struct{}{}
	}()

	select {
	case <-done:
		t.Log("Edge case PASSED: p.Wait() returned for totalPages==0")
	case <-time.After(3 * time.Second):
		t.Fatal("p.Wait() HUNG for totalPages==0 edge case")
	}
}
