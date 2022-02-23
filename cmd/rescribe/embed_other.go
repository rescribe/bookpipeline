// Copyright 2021 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// +build !darwin
// +build !linux
// +build !windows

package main

// if not one of the above platforms, we won't embed anything, so
// just create an empty byte slice
var tesszip []byte
var gbookzip []byte
