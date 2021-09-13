package commands

import "github.com/leoh0/machine/libmachine"

func cmdUpgrade(c CommandLine, api libmachine.API) error {
	return runAction("upgrade", c, api)
}
