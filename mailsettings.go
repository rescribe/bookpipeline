// Copyright 2020 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package bookpipeline

// This file contains various mail account specific stuff; set this if
// you want to use the email notification functionality.

// TODO: these should be set in a dotfile on the host, not here where the world can see them
const (
	MailServer = ""
	MailPort   = 587
	MailUser   = ""
	MailPass   = ""
	MailFrom   = ""
	MailTo     = ""
)
