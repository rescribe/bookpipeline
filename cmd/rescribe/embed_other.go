// Copyright 2021 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

//go:build (!darwin && !linux && !windows) || !embed

package main

// if not one of the above platforms, we won't embed anything, so
// just create empty byte slices
var tesszip []byte
var gbookzip []byte
var tessdatazip []byte
