package commands

import "github.com/leoh0/machine/libmachine"

func cmdKill(c CommandLine, api libmachine.API) error {
	return runAction("kill", c, api)
}
