package app

import (
	"io"
	"strings"
)

// bannerArt is "SKINLOOM" in plain ASCII, so every console shows it.
const bannerArt = `
 ____    _  __  ___   _   _   _        ___     ___    __  __
/ ___|  | |/ / |_ _| | \ | | | |      / _ \   / _ \  |  \/  |
\___ \  | ' /   | |  |  \| | | |     | | | | | | | | | |\/| |
 ___) | | . \   | |  | |\  | | |___  | |_| | | |_| | | |  | |
|____/  |_|\_\ |___| |_| \_| |_____|  \___/   \___/  |_|  |_|
`

// writeBanner greets someone who starts the site in a terminal. Log files, Docker
// and systemd (pretty = false) get none: it would repeat on every restart.
func writeBanner(w io.Writer, pretty, color bool, version string) {
	if !pretty {
		return
	}
	var b strings.Builder
	style := func(code, s string) {
		if color {
			b.WriteString(code + s + "\x1b[0m")
		} else {
			b.WriteString(s)
		}
	}
	style("\x1b[1;36m", strings.TrimPrefix(bannerArt, "\n"))
	b.WriteString("\n")
	style("\x1b[2m", " 云织 SkinLoom "+version+" · 开源、自托管的 Minecraft 皮肤站")
	b.WriteString("\n\n")
	_, _ = io.WriteString(w, b.String())
}
