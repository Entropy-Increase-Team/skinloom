// Command skinloom runs the SkinLoom (云织) skin site. See `skinloom --help`.
package main

import (
	"os"

	"github.com/Entropy-Increase-Team/skinloom/internal/cli"
)

func main() { os.Exit(cli.Execute()) }
