/*
Copyright 2026 Specter Ops, Inc.

Licensed under the Apache License, Version 2.0
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/
package postprocessing_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/postprocessing"
)

func waitGateResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("gate operation did not finish")
		return nil
	}
}

func TestGatePendingTurnAdmitsNestedNativeWrites(t *testing.T) {
	var (
		gate         = postprocessing.NewGate()
		entered      = make(chan struct{})
		done         = make(chan error, 1)
		actionCalled = make(chan struct{}, 1)
	)
	_, outerRelease, err := gate.EnterWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer outerRelease()
	go func() {
		close(entered)
		done <- gate.Alone(context.Background(), 100*time.Millisecond, func(context.Context) error { actionCalled <- struct{}{}; return nil })
	}()
	<-entered
	nestedContext, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, innerRelease, err := gate.EnterWrite(nestedContext)
	if err != nil {
		t.Fatalf("nested native writer was blocked while outer writer remained active: %v", err)
	}
	innerRelease()
	if err := waitGateResult(t, done); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquisition did not time out without interrupting native writer: %v", err)
	}
	select {
	case <-actionCalled:
		t.Fatal("extension action ran during active native write")
	default:
	}
	outerRelease()
	if err := gate.Alone(context.Background(), time.Second, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("timeout left gate unusable: %v", err)
	}
	if gate.ChangeNumber() != 2 {
		t.Fatalf("nested native admission changeNumber=%d, want2", gate.ChangeNumber())
	}
}

func TestGateExclusiveTurnBlocksNewNativeWritesAndAllowsPrivateWrites(t *testing.T) {
	var (
		gate             = postprocessing.NewGate()
		exclusiveStarted = make(chan struct{})
		exclusiveFinish  = make(chan struct{})
		exclusiveDone    = make(chan error, 1)
		nativeStarted    = make(chan struct{})
		nativeDone       = make(chan error, 1)
	)
	var finishOnce sync.Once
	defer finishOnce.Do(func() { close(exclusiveFinish) })
	go func() {
		exclusiveDone <- gate.Alone(context.Background(), time.Second, func(ctx context.Context) error {
			_, release, err := gate.EnterWrite(ctx)
			if err != nil {
				return err
			}
			release()
			release()
			close(exclusiveStarted)
			<-exclusiveFinish
			return nil
		})
	}()
	<-exclusiveStarted
	nativeContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		close(nativeStarted)
		_, release, err := gate.EnterWrite(nativeContext)
		if err == nil {
			release()
		}
		nativeDone <- err
	}()
	<-nativeStarted
	cancel()
	if err := waitGateResult(t, nativeDone); !errors.Is(err, context.Canceled) {
		t.Fatalf("new native write entered exclusive turn or ignored cancellation: %v", err)
	}
	if gate.ChangeNumber() != 0 {
		t.Fatal("private or canceled native admission advanced changeNumber")
	}
	// Release through a separate channel so deferred cleanup remains safe on failure.
	finishOnce.Do(func() { close(exclusiveFinish) })
	if err := waitGateResult(t, exclusiveDone); err != nil {
		t.Fatal(err)
	}
	_, release, err := gate.EnterWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	if gate.ChangeNumber() != 1 {
		t.Fatal("native writer could not enter after extension turn")
	}
}

func TestGateDeadlineWaitsForPhysicalPrivateWriteCompletion(t *testing.T) {
	var (
		gate           = postprocessing.NewGate()
		privateStarted = make(chan context.Context, 1)
		writeFinish    = make(chan struct{})
		turnDone       = make(chan error, 1)
		nativeDone     = make(chan error, 1)
	)
	parent, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	var finishOnce sync.Once
	defer finishOnce.Do(func() { close(writeFinish) })
	go func() {
		turnDone <- gate.Alone(parent, time.Second, func(ctx context.Context) error {
			_, release, err := gate.EnterWrite(ctx)
			if err != nil {
				return err
			}
			privateStarted <- ctx
			go func() { <-writeFinish; release() }()
			return nil
		})
	}()
	var turnContext = <-privateStarted
	<-turnContext.Done()
	<-parent.Done()
	// The callback ended and its context expired, but its write has not finished.
	select {
	case <-turnDone:
		t.Fatal("turn returned before its active writer finished")
	default:
	}
	nativeContext, nativeCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer nativeCancel()
	go func() {
		_, release, err := gate.EnterWrite(nativeContext)
		if err == nil {
			release()
		}
		nativeDone <- err
	}()
	if err := waitGateResult(t, nativeDone); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("native write overlapped incomplete private write: %v", err)
	}
	finishOnce.Do(func() { close(writeFinish) })
	if err := waitGateResult(t, turnDone); err != nil {
		t.Fatal(err)
	}
	if gate.ChangeNumber() != 0 {
		t.Fatal("waiting native write advanced changeNumber")
	}
}

func TestGateExpiredPrivateContextCannotBeReused(t *testing.T) {
	var (
		gate    = postprocessing.NewGate()
		expired context.Context
	)
	if err := gate.Alone(context.Background(), time.Second, func(ctx context.Context) error { expired = context.WithoutCancel(ctx); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := gate.EnterWrite(expired); err == nil || !strings.Contains(err.Error(), "turn to change the graph has ended") {
		t.Fatalf("expired private context was accepted: %v", err)
	}
	if err := gate.Alone(context.Background(), time.Second, func(context.Context) error {
		_, _, err := gate.EnterWrite(expired)
		if err == nil {
			return errors.New("old private context was accepted during a new turn")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if gate.ChangeNumber() != 0 {
		t.Fatal("expired context changed changeNumber")
	}
}

func TestGatePrivatePrivilegeBelongsToOneGate(t *testing.T) {
	var (
		first  = postprocessing.NewGate()
		second = postprocessing.NewGate()
	)
	if err := first.Alone(context.Background(), time.Second, func(ctx context.Context) error {
		_, release, err := second.EnterWrite(ctx)
		if err != nil {
			return err
		}
		release()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if first.ChangeNumber() != 0 || second.ChangeNumber() != 1 {
		t.Fatal("private context granted privilege on another gate")
	}
}

func TestGateBlockWritesWakesWaitingWritersAndRefusesNewTurns(t *testing.T) {
	var (
		gate          = postprocessing.NewGate()
		cause         = errors.New("commit acknowledgement lost")
		actionStarted = make(chan struct{})
		finish        = make(chan struct{})
		actionDone    = make(chan error, 1)
		writerDone    = make(chan error, 1)
	)
	var finishOnce sync.Once
	defer finishOnce.Do(func() { close(finish) })
	go func() {
		actionDone <- gate.Alone(context.Background(), time.Second, func(context.Context) error { close(actionStarted); <-finish; return nil })
	}()
	<-actionStarted
	go func() {
		_, release, err := gate.EnterWrite(context.Background())
		if err == nil {
			release()
		}
		writerDone <- err
	}()
	gate.BlockWrites(cause)
	if err := waitGateResult(t, writerDone); !errors.Is(err, cause) {
		t.Fatalf("blocked waiting writer lost cause: %v", err)
	}
	finishOnce.Do(func() { close(finish) })
	if err := waitGateResult(t, actionDone); err != nil {
		t.Fatal(err)
	}
	if _, _, err := gate.EnterWrite(context.Background()); !errors.Is(err, cause) {
		t.Fatalf("new writer bypassed quarantine: %v", err)
	}
	var called bool
	if err := gate.Alone(context.Background(), time.Second, func(context.Context) error { called = true; return nil }); !errors.Is(err, cause) || called {
		t.Fatalf("new extension turn bypassed quarantine: %v", err)
	}
	if gate.ChangeNumber() != 0 {
		t.Fatal("refused writes changed changeNumber")
	}
}

func TestGateChangeNumberCountsAdmissionsAndReleaseIsIdempotent(t *testing.T) {
	var gate = postprocessing.NewGate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := gate.EnterWrite(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if gate.ChangeNumber() != 0 {
		t.Fatal("canceled admission changed changeNumber")
	}
	_, release, err := gate.EnterWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
	if gate.ChangeNumber() != 1 {
		t.Fatal("release changed changeNumber")
	}
	if err := gate.Alone(context.Background(), time.Second, func(turnContext context.Context) error {
		_, privateRelease, err := gate.EnterWrite(turnContext)
		if err != nil {
			return err
		}
		privateRelease()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if gate.ChangeNumber() != 1 {
		t.Fatal("extension writer changed native changeNumber")
	}
	_, nextRelease, err := gate.EnterWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	nextRelease()
	if gate.ChangeNumber() != 2 {
		t.Fatal("new ordinary admission did not change changeNumber")
	}
}
