package oauth

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser attempts to open the given URL in the user's default browser.
// Returns an error if the launch command fails. Callers should treat failures
// as non-fatal and fall back to displaying the URL for manual opening.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("oauth/browser: unsupported OS %q", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("oauth/browser: open browser: %w", err)
	}
	return nil
}
