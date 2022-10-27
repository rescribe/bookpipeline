// Copyright 2022 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

//go:build embed

package main

import _ "embed"

//go:embed tessdata.20220322.zip
var tessdatazip []byte
