// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

// Package postprocessing connects the experimental rule runner to BloodHound.
// The separate rule container stores files. BloodHound owns every calculation
// and database change, so losing the container cannot leave a remote writer alive.
package postprocessing

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Gate lets ordinary graph writers work together, then gives extension rules a
// turn alone. Reading never enters this gate. A gate belongs to ONE application
// process; it does not coordinate separate BloodHound instances or direct SQL.
type Gate struct {
	mutex        sync.Mutex
	changed      chan struct{}
	writers      int
	changeNumber uint64
	turn         *writeTurn
	blocked      error
}

type writeTurn struct {
	gate    *Gate
	open    bool
	writers int
}
type turnKey struct{}

func NewGate() *Gate { return &Gate{changed: make(chan struct{})} }

func (s *Gate) notify() {
	close(s.changed)
	s.changed = make(chan struct{})
}

// ChangeNumber changes whenever ordinary code starts a write, including a write that
// later fails. This deliberately errs on the side of recalculating after a new
// native analysis instead of retrying old work against potentially changed data.
func (s *Gate) ChangeNumber() uint64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.changeNumber
}

// EnterWrite fits DatabaseSwitch's optional write hook. The returned function
// must run only when the underlying database operation has actually finished.
func (s *Gate) EnterWrite(ctx context.Context) (context.Context, func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return ctx, nil, err
		}
		s.mutex.Lock()
		if s.blocked != nil {
			err := s.blocked
			s.mutex.Unlock()
			return ctx, nil, err
		}
		if turn, ok := ctx.Value(turnKey{}).(*writeTurn); ok && turn.gate == s {
			if !turn.open || s.turn != turn {
				s.mutex.Unlock()
				return ctx, nil, fmt.Errorf("the extension's turn to change the graph has ended")
			}
			turn.writers++
			s.mutex.Unlock()
			var once sync.Once
			return ctx, func() {
				once.Do(func() {
					s.mutex.Lock()
					defer s.mutex.Unlock()
					turn.writers--
					s.notify()
				})
			}, nil
		}
		if s.turn == nil {
			s.writers++
			s.changeNumber++
			s.mutex.Unlock()
			var once sync.Once
			return ctx, func() {
				once.Do(func() {
					s.mutex.Lock()
					defer s.mutex.Unlock()
					s.writers--
					s.notify()
				})
			}, nil
		}
		changed := s.changed
		s.mutex.Unlock()
		select {
		case <-ctx.Done():
			return ctx, nil, ctx.Err()
		case <-changed:
		}
	}
}

// Alone waits at most wait for current writers to finish. While waiting it
// admits ordinary writes: native code sometimes starts a write inside another
// write, and blocking that inner operation would prevent the outer one ending.
// A continuously busy graph can therefore postpone extension work. On timeout
// callers keep old results and retry; they never force native writers to stop.
func (s *Gate) Alone(ctx context.Context, wait time.Duration, action func(context.Context) error) error {
	// Native analysis and its extension share one turn. Only the original,
	// still-active context can reuse it; this is not a second independent lease.
	if turn, ok := ctx.Value(turnKey{}).(*writeTurn); ok && turn.gate == s {
		s.mutex.Lock()
		valid := turn.open && s.turn == turn && s.blocked == nil
		s.mutex.Unlock()
		if !valid {
			return fmt.Errorf("the exclusive graph turn is no longer available")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return action(ctx)
	}
	var waiting, stopWaiting = context.WithTimeout(ctx, wait)
	defer stopWaiting()
	for {
		if err := waiting.Err(); err != nil {
			return fmt.Errorf("waiting for graph writers: %w", err)
		}
		s.mutex.Lock()
		if s.blocked != nil {
			err := s.blocked
			s.mutex.Unlock()
			return err
		}
		if s.writers == 0 && s.turn == nil {
			turn := &writeTurn{gate: s, open: true}
			s.turn = turn
			s.mutex.Unlock()
			turnContext, cancel := context.WithCancel(context.WithValue(ctx, turnKey{}, turn))
			defer func() {
				cancel()
				s.mutex.Lock()
				turn.open = false
				// Cancellation is a request to stop, not proof that a write has
				// stopped. Do not release other writers until every call ends.
				for turn.writers != 0 {
					changed := s.changed
					s.mutex.Unlock()
					<-changed
					s.mutex.Lock()
				}
				s.turn = nil
				s.notify()
				s.mutex.Unlock()
			}()
			return action(turnContext)
		}
		changed := s.changed
		s.mutex.Unlock()
		select {
		case <-waiting.Done():
			return fmt.Errorf("waiting for graph writers: %w", waiting.Err())
		case <-changed:
		}
	}
}

// BlockWrites keeps queries available but refuses further changes when the
// database could have committed a run whose acknowledgement was lost. Recovery
// requires inspecting the saved run record and restarting this application.
func (s *Gate) BlockWrites(cause error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.blocked = fmt.Errorf("graph changes stopped until the uncertain extension result is checked: %w", cause)
	s.notify()
}
