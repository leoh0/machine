package commands

import (
	"github.com/leoh0/machine/libmachine"
	"github.com/leoh0/machine/libmachine/log"
)

func cmdStart(c CommandLine, api libmachine.API) error {
	if err := runAction("start", c, api); err != nil {
		return err
	}

	log.Info("Started machines may have new IP addresses. You may need to re-run the `docker-machine env` command.")

	return nil
}
