// Copyright 2022 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

// +build !windows

package pipeline

import (
	"os/exec"
)

// HideCmd adds a flag to hide any console window from being
// displayed, if necessary for the platform
func HideCmd(cmd *exec.Cmd) {
}
