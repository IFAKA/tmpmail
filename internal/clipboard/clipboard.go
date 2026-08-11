package clipboard

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

func Copy(s string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("pbcopy")
	case "linux":
		if _, e := exec.LookPath("wl-copy"); e == nil {
			c = exec.Command("wl-copy")
		} else if _, e := exec.LookPath("xclip"); e == nil {
			c = exec.Command("xclip", "-selection", "clipboard")
		} else {
			return errors.New("install wl-clipboard or xclip")
		}
	default:
		return errors.New("clipboard unsupported on this OS")
	}
	c.Stdin = strings.NewReader(s)
	return c.Run()
}
