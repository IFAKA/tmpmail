package main

import (
	"fmt"
	"github.com/IFAKA/tmpmail/internal/provider/mailtm"
	"github.com/IFAKA/tmpmail/internal/tui"
	"os"
)

func main() {
	if err := tui.New(mailtm.New()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tmpmail:", err)
		os.Exit(1)
	}
}
