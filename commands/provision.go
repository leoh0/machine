package commands

import "github.com/leoh0/machine/libmachine"

func cmdProvision(c CommandLine, api libmachine.API) error {
	return runAction("provision", c, api)
}
