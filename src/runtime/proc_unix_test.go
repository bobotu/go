// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !windows

package runtime_test

import "syscall"

// Portability shim for TestSchedStats.

func pipe() (r, w int, err error) {
	var p [2]int
	err = syscall.Pipe(p[:])
	return p[0], p[1], err
}
