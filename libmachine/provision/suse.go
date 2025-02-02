package provision

import (
	"fmt"
	"strings"

	"github.com/leoh0/machine/libmachine/auth"
	"github.com/leoh0/machine/libmachine/drivers"
	"github.com/leoh0/machine/libmachine/engine"
	"github.com/leoh0/machine/libmachine/log"
	"github.com/leoh0/machine/libmachine/mcnutils"
	"github.com/leoh0/machine/libmachine/provision/pkgaction"
	"github.com/leoh0/machine/libmachine/provision/serviceaction"
	"github.com/leoh0/machine/libmachine/swarm"
)

func init() {
	Register("SUSE", &RegisteredProvisioner{
		New: NewSUSEProvisioner,
	})
}

func NewSUSEProvisioner(d drivers.Driver) Provisioner {
	return &SUSEProvisioner{
		NewSystemdProvisioner("SUSE", d),
	}
}

type SUSEProvisioner struct {
	SystemdProvisioner
}

func (provisioner *SUSEProvisioner) CompatibleWithHost() bool {
	ids := strings.Split(provisioner.OsReleaseInfo.IDLike, " ")
	for _, id := range ids {
		if id == "suse" {
			return true
		}
	}
	return false
}

func (provisioner *SUSEProvisioner) String() string {
	return "SUSE"
}

func (provisioner *SUSEProvisioner) Package(name string, action pkgaction.PackageAction) error {
	var packageAction string

	switch action {
	case pkgaction.Install:
		packageAction = "in"
		// This is an optimization that reduces the provisioning time of certain
		// systems in a significant way.
		// The invocation of "zypper in <pkg>" causes the download of the metadata
		// of all the repositories that have never been refreshed or that have
		// automatic refresh toggled and have not been refreshed recently.
		// Refreshing the repository metadata can take quite some time and can cause
		// longer provisioning times for machines that have been pre-optimized for
		// docker by including all the needed packages.
		if _, err := provisioner.SSHCommand(fmt.Sprintf("rpm -q %s", name)); err == nil {
			log.Debugf("%s is already installed, skipping operation", name)
			return nil
		}
	case pkgaction.Remove:
		packageAction = "rm"
	case pkgaction.Upgrade:
		packageAction = "up"
	}

	command := fmt.Sprintf("sudo -E zypper -n %s %s", packageAction, name)

	log.Debugf("zypper: action=%s name=%s", action.String(), name)

	if _, err := provisioner.SSHCommand(command); err != nil {
		return err
	}

	return nil
}

func (provisioner *SUSEProvisioner) dockerDaemonResponding() bool {
	log.Debug("checking docker daemon")

	if out, err := provisioner.SSHCommand("sudo docker version"); err != nil {
		log.Warnf("Error getting SSH command to check if the daemon is up: %s", err)
		log.Debugf("'sudo docker version' output:\n%s", out)
		return false
	}

	// The daemon is up if the command worked.  Carry on.
	return true
}

func (provisioner *SUSEProvisioner) Provision(swarmOptions swarm.Options, authOptions auth.Options, engineOptions engine.Options) error {
	provisioner.SwarmOptions = swarmOptions
	provisioner.AuthOptions = authOptions
	provisioner.EngineOptions = engineOptions
	swarmOptions.Env = engineOptions.Env

	// figure out the filesystem used by /var/lib/docker
	fs, err := provisioner.SSHCommand("stat -f -c %T /var/lib/docker")
	if err != nil {
		// figure out the filesystem used by /var/lib
		fs, err = provisioner.SSHCommand("stat -f -c %T /var/lib/")
		if err != nil {
			return err
		}
	}
	graphDriver := "overlay"
	if strings.Contains(fs, "btrfs") {
		graphDriver = "btrfs"
	}

	storageDriver, err := decideStorageDriver(provisioner, graphDriver, engineOptions.StorageDriver)
	if err != nil {
		return err
	}
	provisioner.EngineOptions.StorageDriver = storageDriver

	log.Debug("Setting hostname")
	if err := provisioner.SetHostname(provisioner.Driver.GetMachineName()); err != nil {
		return err
	}

	if !strings.HasPrefix(strings.ToLower(provisioner.OsReleaseInfo.ID), "opensuse") {
		// This is a SLE machine, enable the containers module to have access
		// to the docker packages
		if _, err := provisioner.SSHCommand("sudo -E SUSEConnect -p sle-module-containers/12/$(uname -m) -r ''"); err != nil {
			return fmt.Errorf(
				"Error while adding the 'containers' module, make sure this machine is registered either against SUSE Customer Center (SCC) or to a local Subscription Management Tool (SMT): %v",
				err)
		}
	}

	log.Debug("Installing base packages")
	for _, pkg := range provisioner.Packages {
		if err := provisioner.Package(pkg, pkgaction.Install); err != nil {
			return err
		}
	}

	log.Debug("Installing docker")
	if err := provisioner.Package("docker", pkgaction.Install); err != nil {
		return err
	}

	// create symlinks for containerd, containerd-shim and optional runc.
	// We have to do that because machine overrides the openSUSE systemd
	// unit of docker
	if _, err := provisioner.SSHCommand("yes no | sudo -E ln -si /usr/sbin/runc /usr/sbin/docker-runc"); err != nil {
		return err
	}
	if _, err := provisioner.SSHCommand("sudo -E ln -sf /usr/sbin/containerd /usr/sbin/docker-containerd"); err != nil {
		return err
	}
	if _, err := provisioner.SSHCommand("sudo -E ln -sf /usr/sbin/containerd-shim /usr/sbin/docker-containerd-shim"); err != nil {
		return err
	}

	// Is yast2 firewall installed?
	if _, installed := provisioner.SSHCommand("rpm -q yast2-firewall"); installed == nil {
		// Open the firewall port required by docker
		if _, err := provisioner.SSHCommand("sudo -E /sbin/yast2 firewall services add ipprotocol=tcp tcpport=2376 zone=EXT"); err != nil {
			return err
		}
	}

	log.Debug("Starting systemd docker service")
	if err := provisioner.Service("docker", serviceaction.Start); err != nil {
		return err
	}

	log.Debug("Waiting for docker daemon")
	if err := mcnutils.WaitFor(provisioner.dockerDaemonResponding); err != nil {
		return err
	}

	provisioner.AuthOptions = setRemoteAuthOptions(provisioner)

	log.Debug("Configuring auth")
	if err := ConfigureAuth(provisioner); err != nil {
		return err
	}

	log.Debug("Configuring swarm")
	if err := configureSwarm(provisioner, swarmOptions, provisioner.AuthOptions); err != nil {
		return err
	}

	// enable in systemd
	log.Debug("Enabling docker in systemd")
	err = provisioner.Service("docker", serviceaction.Enable)
	return err
}
