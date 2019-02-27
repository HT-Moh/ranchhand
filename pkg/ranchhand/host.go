package ranchhand

import (
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/semver"
	"github.com/cerebrotech/ranchhand/pkg/ssh"
	"github.com/pkg/errors"
)

const HostCheckTimeout = 1 * time.Minute

func processHosts(cfg *Config) error {
	errChan := make(chan error)

	for _, hostname := range cfg.Nodes {
		hostAddr := fmt.Sprintf("%s:%d", hostname, cfg.SSHPort)

		go func(addr, user, keyPath string, c chan<- error) {
			c <- processHost(addr, user, keyPath)
		}(hostAddr, cfg.SSHUser, cfg.SSHKeyPath, errChan)
	}

	var errs []error
	for i := 0; i < len(cfg.Nodes); i++ {
		select {
		case err := <-errChan:
			if err != nil {
				errs = append(errs, err)
			}
		case <-time.After(HostCheckTimeout):
			return errors.New("host check timeout exceeded")
		}
	}

	if len(errs) > 0 {
		return unifiedErrorF("one or more nodes failed requirement checks: %s", errs)
	}

	return nil
}

func processHost(addr, username, keyPath string) error {
	client, err := ssh.Connect(addr, username, keyPath)
	if err != nil {
		return err
	}

	// enforce requirements
	osi, err := client.ExecuteCmd("cat /etc/os-release")
	if err != nil {
		return errors.Wrap(err, "os info check failed")
	}
	osInfo := parseOSInfo(osi)

	var vErr error
	switch osInfo["ID"] {
	case "ubuntu":
		vErr = runVersionComparison("~16.04", osInfo["VERSION_ID"])
	case "rhel", "centos":
		vErr = runVersionComparison("~7", osInfo["VERSION_ID"])
	default:
		vErr = errors.Errorf("os %s is not supported", osInfo["PRETTY_NAME"])
	}
	if vErr != nil {
		return vErr
	}

	// todo: check hardware resources: should be 4 CPUs and 16GB RAM

	// install docker client and server
	dockerV, err := client.ExecuteCmd("sudo docker version --format '{{.Server.Version}}'")
	if err == nil { // docker is already installed
		v1, err := semver.NewVersion(dockerV)
		if err != nil {
			return err
		}

		v2, err := semver.NewVersion("18.09.2")
		if err != nil {
			return err
		}

		if !v1.Equal(v2) {
			return errors.Errorf("invalid version of docker installed %q", dockerV)
		}
	} else {
		var cmds []string

		switch osInfo["ID"] {
		case "ubuntu":
			cmds = append(cmds,
				"sudo apt-get update",
				"sudo apt-get remove docker docker-engine docker.io containerd runc",
				"sudo apt-get install -y apt-transport-https ca-certificates curl gnupg-agent software-properties-common",
				"curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -",
				"sudo apt-key fingerprint 0EBFCD88",
				"sudo add-apt-repository \"deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable\"",
				"sudo apt-get update",
				"sudo apt-get install -y docker-ce=5:18.09.2~3-0~ubuntu-xenial docker-ce-cli=5:18.09.2~3-0~ubuntu-xenial containerd.io",
			)
		case "centos":
			cmds = append(cmds,
				"sudo yum remove docker docker-client docker-client-latest docker-common docker-latest docker-latest-logrotate docker-logrotate docker-engine",
				"sudo sudo yum install -y yum-utils device-mapper-persistent-data lvm2",
				"sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo",
				"sudo yum install -y docker-ce-18.09.2 docker-ce-cli-18.09.2 containerd.io",
				"sudo systemctl start docker",
			)
		case "rhel":
			return errors.New("cannot install docker-ee on rhel, contact admin")
		}

		_, err := client.ExecuteCmd(strings.Join(cmds, " && "))
		if err != nil {
			panic(err)
		}
	}

	return nil
}

func runVersionComparison(constraint, version string) error {
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return err
	}

	v, err := semver.NewVersion(version)
	if err != nil {
		return err
	}

	if ok, errs := c.Validate(v); !ok {
		return unifiedErrorF("invalid os version: %s", errs)
	}

	return nil
}

func unifiedErrorF(format string, errs []error) error { // NOTE: util func
	if len(errs) == 0 {
		return nil
	}

	var msgs []string
	for idx, err := range errs {
		msgs = append(msgs, fmt.Sprintf("[%d: %s]", idx, err.Error()))
	}
	fullMsg := strings.Join(msgs, ", ")

	return errors.Errorf(format, fullMsg)
}

func parseOSInfo(s string) map[string]string {
	kvPairs := strings.Split(s, "\n")
	osInfo := make(map[string]string)
	for _, pair := range kvPairs {
		if len(pair) > 0 {
			z := strings.Split(pair, "=")
			osInfo[z[0]] = strings.Trim(z[1], "\"")
		}
	}

	return osInfo
}

//func enforceRequirements(cfg *Config) error {
//	eChan := make(chan error)
//
//	for _, hostname := range cfg.Nodes {
//		addr := fmt.Sprintf("%s:%d", hostname, cfg.SSHPort)
//
//		go func(addr, user, keyPath string, c chan<- error) {
//			c <- checkHost(addr, user, keyPath)
//		}(addr, cfg.SSHUser, cfg.SSHKeyPath, eChan)
//	}
//
//	var errs []error
//	for i := 0; i < len(cfg.Nodes); i++ {
//		select {
//		case err := <-eChan:
//			if err != nil {
//				errs = append(errs, err)
//			}
//		case <-time.After(HostCheckTimeout):
//			return errors.New("host check timeout exceeded")
//		}
//	}
//
//	if len(errs) > 0 {
//		return unifiedErrorF("one or more nodes failed requirement checks: %s", errs)
//	}
//
//	return nil
//}

//func checkHost(addr, username, keyPath string) error {
//	client, err := ssh.Connect(addr, username, keyPath)
//	if err != nil {
//		return err
//	}
//
//	out, err := client.ExecuteCmd("cat /etc/os-release")
//	if err != nil {
//		return errors.Wrap(err, "os check failed")
//	}
//
//	kvPairs := strings.Split(out, "\n") // NOTE: this should be a util func
//	osInfo := make(map[string]string)
//	for _, pair := range kvPairs {
//		if len(pair) > 0 {
//			z := strings.Split(pair, "=")
//			osInfo[z[0]] = z[1]
//		}
//	}
//	id := strings.Trim(osInfo["ID"], "\"")
//	version := strings.Trim(osInfo["VERSION_ID"], "\"")
//
//	switch id {
//	case "ubuntu":
//		return runVersionComparison("~16.04", version)
//	case "rhel", "centos":
//		return runVersionComparison("~7", version)
//	default:
//		return errors.Errorf("os %s is not supported", osInfo["PRETTY_NAME"])
//	}
//
//	// TODO: check hardware resources
//
//	out, err := client.ExecuteCmd("which docker")
//	if err != nil {
//		return errors.Wrap(err, "unable to determine docker version")
//	}
//}
