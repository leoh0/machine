package provision

import (
	"github.com/leoh0/machine/libmachine/auth"
	"github.com/leoh0/machine/libmachine/engine"
)

type EngineConfigContext struct {
	DockerPort       int
	AuthOptions      auth.Options
	EngineOptions    engine.Options
	DockerOptionsDir string
}
