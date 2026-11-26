//go:build semgrep_fixture

package fixture

import (
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/testkit/await"
	"github.com/lusoris/SnatchArr/api/internal/testkit/refuse"
)

// fatalAwaitInGoroutine documents a fatal-capable await from a goroutine.
func fatalAwaitInGoroutine(t *testing.T) {
	done := make(chan struct{})
	go func() {
		// ruleid: test-fatal-helper-in-goroutine
		await.Await(t, done, "goroutine completion")
	}()
}

// fatalListenerInGoroutine documents a fatal-capable listener from a goroutine.
func fatalListenerInGoroutine(t *testing.T) {
	go func() {
		// ruleid: test-fatal-helper-in-goroutine
		refuse.Listener(t)
	}()
}

// directFatalInGoroutine documents a direct fatal call from a goroutine.
func directFatalInGoroutine(t *testing.T) {
	go func() {
		// ruleid: test-fatal-helper-in-goroutine
		t.Fatalf("goroutine failed")
	}()
}

// fatalAwaitInDirectGo documents a directly launched fatal-capable await.
func fatalAwaitInDirectGo(t *testing.T) {
	done := make(chan struct{})
	// ruleid: test-fatal-helper-in-goroutine
	go await.Await(t, done, "signal")
}

// fatalMethodInDirectGo documents a directly launched fatal method.
func fatalMethodInDirectGo(t *testing.T) {
	// ruleid: test-fatal-helper-in-goroutine
	go t.Fatalf("goroutine failed")
}

// fatalHelperInGoArgument documents a helper evaluated before the goroutine starts.
func fatalHelperInGoArgument(t *testing.T) {
	// ok: test-fatal-helper-in-goroutine
	go func(u string) { _ = u }(refuse.Listener(t))
}

// helperArgInImmediateLiteral documents a helper argument in an immediate literal.
func helperArgInImmediateLiteral(t *testing.T) {
	func() {
		// ok: test-fatal-helper-in-goroutine
		go func(u string) { _ = u }(refuse.Listener(t))
	}()
}

// helperArgInDeferredLiteral documents a helper argument in a deferred literal.
func helperArgInDeferredLiteral(t *testing.T) {
	defer func() {
		// ok: test-fatal-helper-in-goroutine
		go func(u string) { _ = u }(refuse.Listener(t))
	}()
}

// helperArgInNestedGoroutine documents a helper argument in a nested goroutine.
func helperArgInNestedGoroutine(t *testing.T) {
	go func() {
		// ruleid: test-fatal-helper-in-goroutine
		go func(u string) { _ = u }(refuse.Listener(t))
	}()
}

// fatalBodyInImmediateLiteral documents a fatal call in an immediate literal goroutine.
func fatalBodyInImmediateLiteral(t *testing.T) {
	func() {
		go func(u string) {
			// ruleid: test-fatal-helper-in-goroutine
			t.Fatal(u)
		}("x")
	}()
}

// fatalInNestedBlock documents a fatal call in a nested block.
func fatalInNestedBlock(t *testing.T) {
	go func() {
		if true {
			// ruleid: test-fatal-helper-in-goroutine
			t.Fatal("x")
		}
	}()
}

// fatalBodyWithHelperArgument documents a fatal body with a helper argument.
func fatalBodyWithHelperArgument(t *testing.T) {
	go func(u string) {
		// ruleid: test-fatal-helper-in-goroutine
		t.Fatal(u)
		// ok: test-fatal-helper-in-goroutine
	}(refuse.Listener(t))
}

// helperSecondArgInImmediateLiteral documents a helper in a later argument position.
func helperSecondArgInImmediateLiteral(t *testing.T) {
	func() {
		// ok: test-fatal-helper-in-goroutine
		go func(a, u string) { _ = a + u }("x", refuse.Listener(t))
	}()
}

// fatalBodyWithGoArgument documents a fatal call in a goroutine body with an argument.
func fatalBodyWithGoArgument(t *testing.T) {
	// ruleid: test-fatal-helper-in-goroutine
	go func(u string) { t.Fatal(u) }("x")
}

// fatalOnSubtestReceiver documents a fatal call owned by the child test.
func fatalOnSubtestReceiver(t *testing.T) {
	go func() {
		t.Run("child", func(st *testing.T) {
			// ok: test-fatal-helper-in-goroutine
			st.Fatal("child failed")
		})
	}()
}

// awaitOnSubtestReceiver documents a fatal-capable helper owned by the child test.
func awaitOnSubtestReceiver(t *testing.T) {
	done := make(chan struct{})
	go func() {
		t.Run("child", func(st *testing.T) {
			// ok: test-fatal-helper-in-goroutine
			await.Await(st, done, "child")
		})
	}()
}

// fatalOnParentInSubtest documents a parent fatal call from the child callback.
func fatalOnParentInSubtest(t *testing.T) {
	go func() {
		t.Run("child", func(_ *testing.T) {
			// ruleid: test-fatal-helper-in-goroutine
			t.Fatalf("parent failed")
		})
	}()
}

// fatalOnSubtestReceiverInNestedGo documents a child fatal from a nested goroutine.
func fatalOnSubtestReceiverInNestedGo(t *testing.T) {
	go func() {
		t.Run("child", func(st *testing.T) {
			go func() {
				// ruleid: test-fatal-helper-in-goroutine
				st.Fatal("nested goroutine failed")
			}()
		})
	}()
}

// awaitInTestGoroutine documents an await in the test goroutine.
func awaitInTestGoroutine(t *testing.T) {
	done := make(chan struct{})
	// ok: test-fatal-helper-in-goroutine
	await.Await(t, done, "test completion")
}

// errorInGoroutine documents a non-fatal report from a goroutine.
func errorInGoroutine(t *testing.T) {
	go func() {
		// ok: test-fatal-helper-in-goroutine
		t.Errorf("goroutine failed")
	}()
}

// awaitInCleanup documents an await in a cleanup callback.
func awaitInCleanup(t *testing.T) {
	done := make(chan struct{})
	t.Cleanup(func() {
		// ok: test-fatal-helper-in-goroutine
		await.Await(t, done, "cleanup completion")
	})
}

// parentFatalfInRun fails the parent from the child goroutine.
func parentFatalfInRun(t *testing.T) {
	t.Run("c", func(_ *testing.T) {
		// ruleid: test-fatal-helper-in-goroutine
		t.Fatalf("parent")
	})
}

// parentAwaitInRun passes the parent to a fatal-capable helper.
func parentAwaitInRun(t *testing.T, done <-chan struct{}) {
	t.Run("c", func(st *testing.T) {
		// ruleid: test-fatal-helper-in-goroutine
		await.Await(t, done, "parent")
	})
}

// parentListenerInGoArgument evaluates the listener on the subtest goroutine.
func parentListenerInGoArgument(t *testing.T, consume func(string)) {
	t.Run("c", func(st *testing.T) {
		// ruleid: test-fatal-helper-in-goroutine
		go consume(refuse.Listener(t))
	})
}

// parentSkipInBenchmarkRun skips the enclosing benchmark from its child.
func parentSkipInBenchmarkRun(bm *testing.B) {
	bm.Run("c", func(_ *testing.B) {
		// ruleid: test-fatal-helper-in-goroutine
		bm.SkipNow()
	})
}

// parentFatalInNestedRun fails the parent of a nested subtest.
func parentFatalInNestedRun(t *testing.T) {
	t.Run("a", func(st *testing.T) {
		st.Run("b", func(_ *testing.T) {
			// ruleid: test-fatal-helper-in-goroutine
			st.Fatal("parent")
		})
	})
}

// parentFatalInTableRun fails the enclosing subtest from a table case.
func parentFatalInTableRun(t *testing.T, names []string) {
	t.Run("a", func(st *testing.T) {
		for _, name := range names {
			st.Run(name, func(_ *testing.T) {
				// ruleid: test-fatal-helper-in-goroutine
				st.Fatal("parent")
			})
		}
	})
}

// shadowedReceiverInRun fails the child whose parameter shadows the parent.
func shadowedReceiverInRun(t *testing.T) {
	t.Run("c", func(t *testing.T) {
		// ok: test-fatal-helper-in-goroutine
		t.Fatal("own")
	})
}

// ownReceiverInRun fails the current subtest.
func ownReceiverInRun(t *testing.T) {
	t.Run("c", func(st *testing.T) {
		// ok: test-fatal-helper-in-goroutine
		st.Fatal("own")
	})
}

// nonfatalParentInRun reports to the parent without stopping its goroutine.
func nonfatalParentInRun(t *testing.T) {
	t.Run("c", func(_ *testing.T) {
		// ok: test-fatal-helper-in-goroutine
		t.Errorf("parent")
		// ok: test-fatal-helper-in-goroutine
		t.Log("x")
	})
}

// ownReceiverInNestedRun fails only the innermost subtest.
func ownReceiverInNestedRun(t *testing.T) {
	t.Run("a", func(st *testing.T) {
		st.Run("b", func(st2 *testing.T) {
			// ok: test-fatal-helper-in-goroutine
			st2.Fatal("own")
		})
	})
}

// ownReceiverInTableRun fails only the current table case.
func ownReceiverInTableRun(t *testing.T, names []string) {
	t.Run("a", func(st *testing.T) {
		for _, name := range names {
			st.Run(name, func(st2 *testing.T) {
				// ok: test-fatal-helper-in-goroutine
				st2.Fatal("own")
			})
		}
	})
}

// ownReceiverInBenchmarkRun fails the current sub-benchmark.
func ownReceiverInBenchmarkRun(bm *testing.B) {
	bm.Run("c", func(sb *testing.B) {
		// ok: test-fatal-helper-in-goroutine
		sb.Fatal("own")
	})
}

// mixedRunOwnership evaluates the subtest name in the benchmark, then runs the callback on the subtest.
func mixedRunOwnership(t *testing.T, b *testing.B) {
	b.Run("child", func(sb *testing.B) {
		// ruleid: test-fatal-helper-in-goroutine
		if !t.Run(refuse.Listener(b), func(st *testing.T) {
			// ok: test-fatal-helper-in-goroutine
			st.Fatal("own")
		}) {
			sb.Log("subtest failed")
		}
	})
}
