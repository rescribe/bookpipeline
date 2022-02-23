// Copyright 2021 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package main

import _ "embed"

//go:embed tesseract-linux-v5.0.0-alpha.20210510.zip
var tesszip []byte

//go:embed getgbook-linux-cac42fb.zip
var gbookzip []byte
