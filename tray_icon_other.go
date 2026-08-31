//go:build !windows

package main

import _ "embed"

//go:embed tray.png
var trayIcon []byte
