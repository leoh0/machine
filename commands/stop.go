package commands

import "github.com/leoh0/machine/libmachine"

func cmdStop(c CommandLine, api libmachine.API) error {
	return runAction("stop", c, api)
}
