package commands

import (
	"testing"

	"github.com/leoh0/machine/commands/commandstest"
	"github.com/leoh0/machine/drivers/fakedriver"
	"github.com/leoh0/machine/libmachine/host"
	"github.com/leoh0/machine/libmachine/libmachinetest"
	"github.com/leoh0/machine/libmachine/state"
	"github.com/stretchr/testify/assert"
)

func TestCmdURLMissingMachineName(t *testing.T) {
	commandLine := &commandstest.FakeCommandLine{}
	api := &libmachinetest.FakeAPI{}

	err := cmdURL(commandLine, api)

	assert.Equal(t, ErrNoDefault, err)
}

func TestCmdURLTooManyNames(t *testing.T) {
	commandLine := &commandstest.FakeCommandLine{
		CliArgs: []string{"machineToRemove1", "machineToRemove2"},
	}
	api := &libmachinetest.FakeAPI{}

	err := cmdURL(commandLine, api)

	assert.EqualError(t, err, "Error: Expected one machine name as an argument")
}

func TestCmdURL(t *testing.T) {
	commandLine := &commandstest.FakeCommandLine{
		CliArgs: []string{"machine"},
	}
	api := &libmachinetest.FakeAPI{
		Hosts: []*host.Host{
			{
				Name: "machine",
				Driver: &fakedriver.Driver{
					MockState: state.Running,
					MockIP:    "120.0.0.1",
				},
			},
		},
	}

	stdoutGetter := commandstest.NewStdoutGetter()
	defer stdoutGetter.Stop()

	err := cmdURL(commandLine, api)

	assert.NoError(t, err)
	assert.Equal(t, "tcp://120.0.0.1:2376\n", stdoutGetter.Output())
}
