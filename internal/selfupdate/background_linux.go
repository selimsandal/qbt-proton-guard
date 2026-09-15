package selfupdate

import "os/exec"

func startBackground(binary, log string, arguments ...string) error {
	args := []string{"--user", "--collect", "--quiet", "--property=StandardOutput=append:" + log, "--property=StandardError=append:" + log, "--", binary}
	return exec.Command("systemd-run", append(args, arguments...)...).Run()
}
