package selfupdate

import "os/exec"

func startBackground(binary, log string) error {
	return exec.Command("systemd-run", "--user", "--collect", "--quiet", "--property=StandardOutput=append:"+log, "--property=StandardError=append:"+log, "--", binary, "update").Run()
}
