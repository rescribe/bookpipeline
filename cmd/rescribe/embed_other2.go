// Copyright 2021 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

//go:build (!darwin && !linux && !windows) || (!embed && !flatpak)

package main

// if not one of the above platforms, we won't embed tessdata, so
// just create an empty byte slice
var tessdatazip []byte
