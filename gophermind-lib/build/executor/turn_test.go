package executor

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTurnsTakeTheMainTreeOneAtATimeInOrder(t *testing.T) {
	t.Parallel()
	var body sync.Mutex
	const n = 6
	turns := make([]*turn, n)
	var prev <-chan struct{}
	for i := range turns {
		turns[i] = newTurn(prev, &body)
		prev = turns[i].done
	}
	var mu sync.Mutex
	var order []int
	var cur, max int32
	var wg sync.WaitGroup
	for i := n - 1; i >= 0; i-- { // started in reverse: the order must not depend on it
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer turns[i].release()
			time.Sleep(time.Duration((n-i)*3) * time.Millisecond)
			if err := turns[i].wait(context.Background()); err != nil {
				t.Error(err)
				return
			}
			c := atomic.AddInt32(&cur, 1)
			for {
				m := atomic.LoadInt32(&max)
				if c <= m || atomic.CompareAndSwapInt32(&max, m, c) {
					break
				}
			}
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
			atomic.AddInt32(&cur, -1)
		}()
	}
	wg.Wait()
	if !reflect.DeepEqual(order, []int{0, 1, 2, 3, 4, 5}) {
		t.Fatalf("order = %v", order)
	}
	if max != 1 {
		t.Fatalf("%d jobs held the main tree at once", max)
	}
	if !body.TryLock() {
		t.Fatal("the body mutex is still held after every release")
	}
	body.Unlock()
}

func TestTurnWaitEndsWithTheContextAndTakesNothing(t *testing.T) {
	t.Parallel()
	var body sync.Mutex
	tn := newTurn(make(chan struct{}), &body)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if err := tn.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait = %v", err)
	}
	if !body.TryLock() {
		t.Fatal("an interrupted wait took the main tree")
	}
	body.Unlock()
	tn.release()
	tn.release() // twice is safe
	select {
	case <-tn.done:
	default:
		t.Fatal("release did not let the next job go")
	}
	free := newTurn(nil, &body)
	if err := free.wait(ctx); err == nil {
		t.Fatal("an ended context took a free turn")
	}
	free.release()
}

func TestAJobThatEndsWithoutItsTurnDoesNotBlockTheNext(t *testing.T) {
	t.Parallel()
	var body sync.Mutex
	first := newTurn(nil, &body)
	second := newTurn(first.done, &body)
	first.release() // it failed before it needed the main tree
	done := make(chan error, 1)
	go func() { done <- second.wait(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the next job never got its turn")
	}
	second.release()
}

// Ruling C5 (spec section 7.3): once a landing failed, the jobs behind it do
// not land. The fault is read after the previous job ended, so a job that was
// already waiting ends without the main tree, and the next ones still go.
func TestAJobBehindAFailedLandingDoesNotTakeTheMainTree(t *testing.T) {
	t.Parallel()
	var body sync.Mutex
	var fault atomic.Bool
	first := newTurn(nil, &body)
	first.fault = &fault
	second := newTurn(first.done, &body)
	second.fault = &fault
	third := newTurn(second.done, &body)
	third.fault = &fault
	if err := first.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- second.wait(context.Background()) }()
	fault.Store(true) // the first job's commit failed: halt(nil)
	first.release()
	select {
	case err := <-done:
		if !errors.Is(err, errLandingFault) {
			t.Fatalf("wait = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job behind the failed landing never ended")
	}
	if !body.TryLock() {
		t.Fatal("a job behind a failed landing took the main tree")
	}
	body.Unlock()
	second.release()
	if err := third.wait(context.Background()); !errors.Is(err, errLandingFault) {
		t.Fatalf("third wait = %v", err)
	}
	third.release()
}
