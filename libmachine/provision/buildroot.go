package provision

import (
	"bytes"
	"fmt"
	"net"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/leoh0/machine/libmachine/auth"
	"github.com/leoh0/machine/libmachine/drivers"
	"github.com/leoh0/machine/libmachine/engine"
	"github.com/leoh0/machine/libmachine/log"
	"github.com/leoh0/machine/libmachine/provision/pkgaction"
	"github.com/leoh0/machine/libmachine/swarm"
)

func init() {
	Register("buildroot", &RegisteredProvisioner{
		New: NewBuildRootProvisioner,
	})
}

func NewBuildRootProvisioner(d drivers.Driver) Provisioner {
	return &BuildRootProvisioner{
		NewSystemdProvisioner("buildroot", d),
		"default",
	}
}

// for escaping systemd template specifiers (e.g. '%i'), which are not supported by minikube
var systemdSpecifierEscaper = strings.NewReplacer("%", "%%")

// escapeSystemdDirectives escapes special characters in the input variables used to create the
// systemd unit file, which would otherwise be interpreted as systemd directives. An example
// are template specifiers (e.g. '%i') which are predefined variables that get evaluated dynamically
// (see systemd man pages for more info). This is not supported by minikube, thus needs to be escaped.
func escapeSystemdDirectives(engineConfigContext *EngineConfigContext) {
	// escape '%' in Environment option so that it does not evaluate into a template specifier
	engineConfigContext.EngineOptions.Env = replaceChars(engineConfigContext.EngineOptions.Env, systemdSpecifierEscaper)
	// input might contain whitespaces, wrap it in quotes
	engineConfigContext.EngineOptions.Env = concatStrings(engineConfigContext.EngineOptions.Env, "\"", "\"")
}

// replaceChars returns a copy of the src slice with each string modified by the replacer
func replaceChars(src []string, replacer *strings.Replacer) []string {
	ret := make([]string, len(src))
	for i, s := range src {
		ret[i] = replacer.Replace(s)
	}
	return ret
}

// concatStrings concatenates each string in the src slice with prefix and postfix and returns a new slice
func concatStrings(src []string, prefix string, postfix string) []string {
	var buf bytes.Buffer
	ret := make([]string, len(src))
	for i, s := range src {
		buf.WriteString(prefix)
		buf.WriteString(s)
		buf.WriteString(postfix)
		ret[i] = buf.String()
		buf.Reset()
	}
	return ret
}

// updateUnit efficiently updates a systemd unit file
func updateUnit(p SSHCommander, name string, content string, dst string) error {
	log.Infof("Updating %s unit: %s ...", name, dst)

	if _, err := p.SSHCommand(fmt.Sprintf("sudo mkdir -p %s && printf %%s \"%s\" | sudo tee %s.new", path.Dir(dst), content, dst)); err != nil {
		return err
	}
	if _, err := p.SSHCommand(fmt.Sprintf("sudo diff -u %s %s.new || { sudo mv %s.new %s; sudo systemctl -f daemon-reload && sudo systemctl -f enable %s && sudo systemctl -f restart %s; }", dst, dst, dst, dst, name, name)); err != nil {
		return err
	}
	return nil
}

type BuildRootProvisioner struct {
	SystemdProvisioner
	clusterName string
}

func (p *BuildRootProvisioner) String() string {
	return "buildroot"
}

// func (p *BuildRootProvisioner) Service(name string, action serviceaction.ServiceAction) error {
// 	_, err := p.SSHCommand(fmt.Sprintf("sudo /etc/init.d/%s %s", name, action.String()))
// 	return err
// }

func (p *BuildRootProvisioner) Package(name string, action pkgaction.PackageAction) error {
	return nil
}

func (p *BuildRootProvisioner) Hostname() (string, error) {
	return p.SSHCommand("hostname")
}

// func (p *BuildRootProvisioner) SetHostname(hostname string) error {
// 	if _, err := p.SSHCommand(fmt.Sprintf(
// 		"sudo /usr/bin/sethostname %s && echo %q | sudo tee /var/lib/buildroot/etc/hostname",
// 		hostname,
// 		hostname,
// 	)); err != nil {
// 		return err
// 	}

// 	return nil
// }

func (p *BuildRootProvisioner) GetDockerOptionsDir() string {
	return "/var/lib/buildroot"
}

func (p *BuildRootProvisioner) GetAuthOptions() auth.Options {
	return p.AuthOptions
}

func (p *BuildRootProvisioner) GetSwarmOptions() swarm.Options {
	return p.SwarmOptions
}

func (p *BuildRootProvisioner) GenerateDockerOptions(dockerPort int) (*DockerOptions, error) {
	var (
		engineCfg bytes.Buffer
	)

	driverNameLabel := fmt.Sprintf("provider=%s", p.Driver.DriverName())
	p.EngineOptions.Labels = append(p.EngineOptions.Labels, driverNameLabel)

	engineConfigTmpl := `[Unit]
Description=Docker Application Container Engine
Documentation=https://docs.docker.com
After=network.target  minikube-automount.service docker.socket
Requires= minikube-automount.service docker.socket 
StartLimitBurst=3
StartLimitIntervalSec=60

[Service]
Type=notify
Restart=on-failure
{{range .EngineOptions.Env}}Environment={{.}}
{{end}}

# This file is a systemd drop-in unit that inherits from the base dockerd configuration.
# The base configuration already specifies an 'ExecStart=...' command. The first directive
# here is to clear out that command inherited from the base configuration. Without this,
# the command from the base configuration and the command specified here are treated as
# a sequence of commands, which is not the desired behavior, nor is it valid -- systemd
# will catch this invalid input and refuse to start the service with an error like:
#  Service has more than one ExecStart= setting, which is only allowed for Type=oneshot services.

# NOTE: default-ulimit=nofile is set to an arbitrary number for consistency with other
# container runtimes. If left unlimited, it may result in OOM issues with MySQL.
ExecStart=
ExecStart=/usr/bin/dockerd -H tcp://0.0.0.0:2376 -H unix:///var/run/docker.sock --default-ulimit=nofile=1048576:1048576 --tlsverify --tlscacert {{.AuthOptions.CaCertRemotePath}} --tlscert {{.AuthOptions.ServerCertRemotePath}} --tlskey {{.AuthOptions.ServerKeyRemotePath}} {{ range .EngineOptions.Labels }}--label {{.}} {{ end }}{{ range .EngineOptions.InsecureRegistry }}--insecure-registry {{.}} {{ end }}{{ range .EngineOptions.RegistryMirror }}--registry-mirror {{.}} {{ end }}{{ range .EngineOptions.ArbitraryFlags }}--{{.}} {{ end }}
ExecReload=/bin/kill -s HUP \$MAINPID

# Having non-zero Limit*s causes performance problems due to accounting overhead
# in the kernel. We recommend using cgroups to do container-local accounting.
LimitNOFILE=infinity
LimitNPROC=infinity
LimitCORE=infinity

# Uncomment TasksMax if your systemd version supports it.
# Only systemd 226 and above support this version.
TasksMax=infinity
TimeoutStartSec=0

# set delegate yes so that systemd does not reset the cgroups of docker containers
Delegate=yes

# kill only the docker process, not all processes in the cgroup
KillMode=process

[Install]
WantedBy=multi-user.target
`
	t, err := template.New("engineConfig").Parse(engineConfigTmpl)
	if err != nil {
		return nil, err
	}

	engineConfigContext := EngineConfigContext{
		DockerPort:    dockerPort,
		AuthOptions:   p.AuthOptions,
		EngineOptions: p.EngineOptions,
	}

	escapeSystemdDirectives(&engineConfigContext)
	
	t.Execute(&engineCfg, engineConfigContext)

	do := &DockerOptions{
		EngineOptions:     engineCfg.String(),
		EngineOptionsPath: "/lib/systemd/system/docker.service",
	}

	return do, updateUnit(p, "docker", do.EngineOptions, do.EngineOptionsPath)
}

func (p *BuildRootProvisioner) CompatibleWithHost() bool {
	return p.OsReleaseInfo.ID == "buildroot"
}

func (p *BuildRootProvisioner) SetOsReleaseInfo(info *OsRelease) {
	p.OsReleaseInfo = info
}

func (p *BuildRootProvisioner) GetOsReleaseInfo() (*OsRelease, error) {
	return p.OsReleaseInfo, nil
}

func (p *BuildRootProvisioner) AttemptIPContact(dockerPort int) {
	ip, err := p.Driver.GetIP()
	if err != nil {
		log.Warnf("Could not get IP address for created machine: %s", err)
		return
	}

	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ip, dockerPort), 5*time.Second); err != nil {
		log.Warnf(`
This machine has been allocated an IP address, but Docker Machine could not
reach it successfully.

SSH for the machine should still work, but connecting to exposed ports, such as
the Docker daemon port (usually <ip>:%d), may not work properly.

You may need to add the route manually, or use another related workaround.

This could be due to a VPN, proxy, or host file configuration issue.

You also might want to clear any VirtualBox host only interfaces you are not using.`, engine.DefaultPort)
	} else {
		conn.Close()
	}
}

func (p *BuildRootProvisioner) Provision(swarmOptions swarm.Options, authOptions auth.Options, engineOptions engine.Options) error {
	var (
		err error
	)

	defer func() {
		if err == nil {
			p.AttemptIPContact(engine.DefaultPort)
		}
	}()

	p.SwarmOptions = swarmOptions
	p.AuthOptions = authOptions
	p.EngineOptions = engineOptions
	swarmOptions.Env = engineOptions.Env

	if p.EngineOptions.StorageDriver == "" {
		p.EngineOptions.StorageDriver = "overlay2"
	}

	if err = p.SetHostname(p.Driver.GetMachineName()); err != nil {
		return err
	}

	// b2d hosts need to wait for the daemon to be up
	// before continuing with provisioning
	// if err = WaitForDocker(p, engine.DefaultPort); err != nil {
	// 	return err
	// }

	if err = makeDockerOptionsDir(p); err != nil {
		return err
	}

	p.AuthOptions = setRemoteAuthOptions(p)
	log.Infof("set auth options %+v", p.AuthOptions)

	log.Infof("setting up certificates")
	if err = ConfigureAuth(p); err != nil {
		return err
	}

	err = configureSwarm(p, swarmOptions, p.AuthOptions)
	return err
}

func (p *BuildRootProvisioner) SSHCommand(args string) (string, error) {
	return drivers.RunSSHCommandFromDriver(p.Driver, args)
}

func (p *BuildRootProvisioner) GetDriver() drivers.Driver {
	return p.Driver
}
