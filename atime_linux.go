// Copyright 2015 Vilibald Wanča. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

// +build linux

package main

import (
    "os"
    "syscall"
    "time"
)

func atime(fi os.FileInfo) time.Time {
    stat := fi.Sys().(*syscall.Stat_t)
    return time.Unix(stat.Atim.Unix())
}
