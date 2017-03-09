// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "runtime/internal/atomic"

// A SchedStats records statistics about the goroutine scheduler.
//
// NOTE: This is an experimental feature locally patched into Go.
// It is not part of the standard Go release.
type SchedStats struct {
	// States records the number of goroutines in each of several
	// distinct scheduling states.
	//
	// These counts are all approximate.
	States struct {
		// Running counts the number of goroutines actively
		// executing Go code.
		//
		// These goroutines may or may not be actively
		// scheduled on a CPU, as this is dependent on the OS
		// kernel. However, they are at least ready to be
		// scheduled on a CPU.
		//
		// This also does not count OS threads actively
		// running non-Go code, such as cgo calls, so it
		// should not be used as an estimate of the CPU load
		// of a Go process.
		//
		// This is bounded by GOMAXPROCS.
		Running int

		// Runnable counts the number of goroutines that could
		// run, but are waiting for CPU resources to become
		// available (specifically, an OS thread).
		//
		// To estimate the total tasks in a Go program,
		// including both goroutines and OS threads executing
		// non-Go code, the caller should query the OS for the
		// number of running or runnable OS threads and add
		// the Runnable count.
		Runnable int

		// NonGo counts the number of goroutines executing
		// non-Go code, such as system calls or cgo calls.
		// These goroutines may or may not be consuming CPU
		// resources.
		//
		// The line between NonGo and Blocked can be fuzzy.
		// For example, some operations that appear to be
		// system calls may in fact be centrally dispatched
		// (e.g., network operations), in which case
		// goroutines blocked on these calls count as Blocked
		// rather than NonGo.
		NonGo int

		// Blocked counts the number of blocked goroutines
		// waiting on some event, such as a channel receive or
		// timer.
		Blocked int
	}
}

// SchedStatsFlags controls the behavior of ReadSchedStats.
type SchedStatsFlags int

const (
	// SchedStatsStates indicates that ReadSchedStats should fill
	// the SchedStats.States field.
	SchedStatsStates SchedStatsFlags = 1 << iota
)

// ReadSchedStats populates s with scheduler statistics.
//
// The flags argument controls which parts of s are populated. In the
// future this may be used to avoid populating statistics that are
// expensive to collect when the caller does not need them.
//
// NOTE: This is an experimental feature locally patched into Go.
// It is not part of the standard Go release.
func ReadSchedStats(s *SchedStats, flags SchedStatsFlags) {
	*s = SchedStats{}

	if flags&SchedStatsStates != 0 {
		readSchedStatsStates(s)
	}
}

func readSchedStatsStates(s *SchedStats) {
	ss := &s.States

retry:
	// Lock the scheduler so the global run queue can't change and
	// the number of Ps can't change. This doesn't prevent the
	// local run queues from changing, so the results are still
	// approximate.
	lock(&sched.lock)

	// Collect running/runnable from per-P run queues.
	for _, p := range allp {
		if p == nil || p.status == _Pdead {
			break
		}
		switch p.status {
		case _Prunning:
			ss.Running++
		case _Psyscall:
			ss.NonGo++
		case _Pgcstop:
			// We're in the middle of stopping the world.
			// Try again.
			unlock(&sched.lock)
			Gosched()
			*ss = struct{ Running, Runnable, NonGo, Blocked int }{}
			goto retry
		}

		for {
			h := atomic.Load(&p.runqhead)
			t := atomic.Load(&p.runqtail)
			next := atomic.Loaduintptr((*uintptr)(&p.runnext))
			runnable := int32(t - h)
			if atomic.Load(&p.runqhead) != h || runnable < 0 {
				continue
			}
			if next != 0 {
				runnable++
			}
			ss.Runnable += int(runnable)
			break
		}
	}

	// Global run queue.
	ss.Runnable += int(sched.runqsize)

	// Account for Gs that are in _Gsyscall without a P in _Psyscall.
	nGsyscallNoP := int32(atomic.Load(&sched.nGsyscallNoP))
	// nGsyscallNoP can go negative during temporary races.
	if nGsyscallNoP >= 0 {
		ss.NonGo += int(nGsyscallNoP)
	}

	// Compute the number of blocked goroutines. We have to
	// include system goroutines in this count because we included
	// them above.
	totalGs := int(gcount(true))
	ss.Blocked = totalGs - (ss.Running + ss.Runnable + ss.NonGo)
	if ss.Blocked < 0 {
		ss.Blocked = 0
	}

	unlock(&sched.lock)
}
