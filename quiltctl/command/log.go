package command

import (
	"errors"
	"flag"
	"fmt"
	"strconv"

	"github.com/NetSys/quilt/api"
	"github.com/NetSys/quilt/quiltctl/ssh"
	log "github.com/Sirupsen/logrus"
)

// Log is the structure for the `quilt logs` command.
type Log struct {
	privateKey     string
	sinceTimestamp string
	showTimestamps bool
	shouldTail     bool

	targetContainer int
	SSHClient       ssh.Client

	common *commonFlags
}

// NewLogCommand creates a new Log command instance.
func NewLogCommand(c ssh.Client) *Log {
	return &Log{
		SSHClient: c,
		common:    &commonFlags{},
	}
}

// InstallFlags sets up parsing for command line flags.
func (lCmd *Log) InstallFlags(flags *flag.FlagSet) {
	lCmd.common.InstallFlags(flags)

	flags.StringVar(&lCmd.privateKey, "i", "",
		"the private key to use to connect to the host")
	flags.StringVar(&lCmd.sinceTimestamp, "since", "", "show logs since timestamp")
	flags.BoolVar(&lCmd.shouldTail, "f", false, "follow log output")
	flags.BoolVar(&lCmd.showTimestamps, "t", false, "show timestamps")

	flags.Usage = func() {
		fmt.Println("usage: quilt logs [-H=<daemon_host>] [-i=<private_key>] " +
			"<stitch_id> <command>")
		fmt.Println("`logs` fetches the logs of a container. " +
			"The container is identified by the stitch ID provided by " +
			"`quilt containers`.")
		fmt.Println("For example, to get the logs of container 5 with a " +
			"specific private key: `quilt logs -i ~/.ssh/quilt 5`")
		flags.PrintDefaults()
	}
}

// Parse parses the command line arguments for the `logs` command.
func (lCmd *Log) Parse(args []string) error {
	if len(args) == 0 {
		return errors.New("must specify a target container")
	}

	targetContainer, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("target container must be a number: %s", args[0])
	}

	lCmd.targetContainer = targetContainer
	return nil
}

// Run finds the target continer and outputs logs.
func (lCmd *Log) Run() int {
	localClient, leaderClient, err := getClients(lCmd.common.host)
	if err != nil {
		log.Error(err)
		return 1
	}
	defer localClient.Close()
	defer leaderClient.Close()

	containerHost, err := getContainerHost(localClient, leaderClient,
		lCmd.targetContainer)
	if err != nil {
		log.WithError(err).
			Error("Error getting the host on which the container is running.")
		return 1
	}

	containerClient, err := getClient(api.RemoteAddress(containerHost))
	if err != nil {
		log.WithError(err).Error("Error connecting to container client.")
		return 1
	}
	defer containerClient.Close()

	container, err := getContainer(containerClient, lCmd.targetContainer)
	if err != nil {
		log.WithError(err).Error("Error retrieving the container information " +
			"from the container host.")
		return 1
	}
	dockerCmd := "docker logs"
	if lCmd.sinceTimestamp != "" {
		dockerCmd += fmt.Sprintf(" --since=%s", lCmd.sinceTimestamp)
	}
	if lCmd.showTimestamps {
		dockerCmd += " --timestamps"
	}
	if lCmd.shouldTail {
		dockerCmd += " --follow"
	}
	dockerCmd += " " + container.DockerID

	err = lCmd.SSHClient.Connect(containerHost, lCmd.privateKey)
	if err != nil {
		log.WithError(err).Info("Error opening SSH connection")
		return 1
	}
	defer lCmd.SSHClient.Disconnect()

	if err = lCmd.SSHClient.Run(dockerCmd); err != nil {
		log.WithError(err).Info("Error running command over SSH")
		return 1
	}

	return 0
}
