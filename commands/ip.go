package commands

import "github.com/leoh0/machine/libmachine"

func cmdIP(c CommandLine, api libmachine.API) error {
	return runAction("ip", c, api)
}
