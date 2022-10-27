// Copyright 2021 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

//go:build embed

package main

import _ "embed"

//go:embed tesseract-w32-v5.0.0-alpha.20210506.zip
var tesszip []byte

//go:embed getgbook-w32-c2824685.zip
var gbookzip []byte
