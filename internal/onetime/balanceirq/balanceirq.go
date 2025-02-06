/*
Copyright 2024 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package balanceirq provides optimal IRQ balancing on X4 instances.
package balanceirq

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

type (
	// writeFileFunc provides a testable replacement for os.WriteFile.
	writeFileFunc func(string, []byte, os.FileMode) error

	// readFileFunc provides a testable replacement for os.ReadFile.
	readFileFunc func(string) ([]byte, error)
)

const (
	systemdContent = `[Unit]
Description=Google Cloud Agent for SAP Balance IRQ
After=multi-user.target

[Service]
Type=idle
ExecStart=/usr/bin/google_cloud_sap_agent balanceirq

[Install]
WantedBy=multi-user.target`
)

// BalanceIRQ has args for balanceirq subcommands.
type BalanceIRQ struct {
	pinToSocket   string
	install, help bool

	writeFile   writeFileFunc
	readFile    readFileFunc
	ExecuteFunc commandlineexecutor.Execute
	IIOTEParams *onetime.InternallyInvokedOTE
	logPath     string
	oteLogger   *onetime.OTELogger
}

type socketCores struct {
	socket string
	cores  string
}

// Name implements the subcommand interface for balanceirq.
func (*BalanceIRQ) Name() string { return "balanceirq" }

// Synopsis implements the subcommand interface for balanceirq.
func (*BalanceIRQ) Synopsis() string {
	return "provides optimal IRQ balancing on X4 instances"
}

// Usage implements the subcommand interface for balanceirq.
func (*BalanceIRQ) Usage() string {
	return `Usage: balanceirq [subcommand]

  Subcommand (optional):
    -pin-to-socket=<"socket">	Pins all interrupts to a specific socket
    -install=<true|false>	Installs a systemd service which will run balanceirq on each boot of the system
    -log-path=<log-path>	The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/balanceirq.log
    -h	Displays help` + "\n"
}

// SetFlags implements the subcommand interface for balanceirq.
func (b *BalanceIRQ) SetFlags(fs *flag.FlagSet) {
	// TODO: Move common flags to global struct.
	fs.StringVar(&b.pinToSocket, "pin-to-socket", "", "Pins all interrupts to a specific socket")
	fs.BoolVar(&b.install, "install", false, "Installs balanceirq as a systemd service")
	fs.StringVar(&b.logPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/balanceirq.log")
	fs.BoolVar(&b.help, "h", false, "Displays help")
}

// Execute implements the subcommand interface for balanceirq.
func (b *BalanceIRQ) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, _, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     b.Name(),
		Help:     b.help,
		Fs:       f,
		LogLevel: "info",
		LogPath:  b.logPath,
		IIOTE:    b.IIOTEParams,
	}, args...)
	if !completed {
		return exitStatus
	}

	return b.Run(ctx, onetime.CreateRunOptions(nil, false))
}

// Run performs the functionality specified by the balanceirq subcommand.
func (b *BalanceIRQ) Run(ctx context.Context, runOpts *onetime.RunOptions) subcommands.ExitStatus {
	b.writeFile = os.WriteFile
	b.readFile = os.ReadFile
	b.ExecuteFunc = commandlineexecutor.ExecuteCommand
	b.oteLogger = onetime.CreateOTELogger(runOpts.DaemonMode)
	exitStatus, err := b.balanceIRQHandler(ctx)
	if err != nil {
		b.oteLogger.LogErrorToFileAndConsole(ctx, "BalanceIRQ failed", err)
		b.oteLogger.LogUsageError(usagemetrics.BalanceIRQFailure)
	}
	return exitStatus
}

// balanceIRQHandler balances interrupt requests over the sockets.
func (b *BalanceIRQ) balanceIRQHandler(ctx context.Context) (subcommands.ExitStatus, error) {
	if b.install {
		b.oteLogger.LogMessageToFileAndConsole(ctx, "BalanceIRQ install starting")
		b.oteLogger.LogUsageAction(usagemetrics.BalanceIRQInstallStarted)
		if err := b.installSystemdService(ctx); err != nil {
			return subcommands.ExitFailure, err
		}
		b.oteLogger.LogMessageToFileAndConsole(ctx, "BalanceIRQ install: SUCCESS")
		b.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("\nDetailed logs are at %s", onetime.LogFilePath(b.Name(), b.IIOTEParams)))
		b.oteLogger.LogUsageAction(usagemetrics.BalanceIRQInstallFinished)
		return subcommands.ExitSuccess, nil
	}

	b.oteLogger.LogMessageToFileAndConsole(ctx, "BalanceIRQ starting")
	b.oteLogger.LogUsageAction(usagemetrics.BalanceIRQStarted)
	if err := b.disableProvidedIRQBalance(ctx); err != nil {
		return subcommands.ExitFailure, err
	}
	sockets, err := b.readSockets(ctx)
	if err != nil {
		return subcommands.ExitFailure, err
	}
	interrupts, err := b.readInterrupts(ctx)
	if err != nil {
		return subcommands.ExitFailure, err
	}

	// Assign sockets to interrupts as evenly as possible.
	for i, interrupt := range interrupts {
		socket := sockets[i%len(sockets)]
		if err := b.writeFile(fmt.Sprintf("/proc/irq/%s/smp_affinity_list", interrupt), []byte(socket.cores), 0644); err != nil {
			return subcommands.ExitFailure, err
		}
		log.CtxLogger(ctx).Infof("Interrupt %s assigned to socket %s, cores %s", interrupt, socket.socket, socket.cores)
	}

	exitStatus := subcommands.ExitSuccess
	b.oteLogger.LogMessageToFileAndConsole(ctx, "BalanceIRQ: SUCCESS")
	b.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("\nDetailed logs are at %s", onetime.LogFilePath(b.Name(), b.IIOTEParams)))
	b.oteLogger.LogUsageAction(usagemetrics.BalanceIRQFinished)
	return exitStatus, nil
}

// readInterrupts reads the interrupts in use by IDPF from /proc/interrupts.
func (b *BalanceIRQ) readInterrupts(ctx context.Context) ([]string, error) {
	interruptData, err := b.readFile("/proc/interrupts")
	if err != nil {
		return nil, err
	}
	var interrupts []string
	for _, line := range strings.Split(string(interruptData), "\n") {
		line := strings.TrimSpace(line)
		split := strings.Split(line, " ")
		if len(split) < 2 {
			continue
		}
		interrupt := strings.TrimSuffix(split[0], ":")
		name := split[len(split)-1]
		if strings.Contains(name, "idpf") && !strings.Contains(name, "Mailbox") {
			interrupts = append(interrupts, interrupt)
		}
	}

	log.CtxLogger(ctx).Infof("Interrupts: %s", interrupts)
	return interrupts, nil
}

// readSockets first determines the NUMA nodes and then determines the CPU
// cores in each node.
func (b *BalanceIRQ) readSockets(ctx context.Context) ([]socketCores, error) {
	nodeData, err := b.readFile("/sys/devices/system/node/has_memory")
	if err != nil {
		return nil, err
	}
	nodes, err := b.parseNumberRanges(string(nodeData))
	if err != nil {
		return nil, err
	}

	var sockets []socketCores
	for _, node := range nodes {
		socketData, err := b.readFile(fmt.Sprintf("/sys/devices/system/node/node%s/cpulist", node))
		if err != nil {
			return nil, err
		}
		sockets = append(sockets, socketCores{
			socket: node,
			cores:  strings.TrimSpace(string(socketData))})
	}
	if b.pinToSocket == "" {
		log.CtxLogger(ctx).Infof("Sockets: %#v", sockets)
		return sockets, nil
	}

	for _, s := range sockets {
		if s.socket == b.pinToSocket {
			log.CtxLogger(ctx).Infof("Pinning all interrupts to provided socket %s", b.pinToSocket)
			return []socketCores{s}, nil
		}
	}
	return nil, fmt.Errorf("provided socket (%s) not found, available sockets and cores: %v", b.pinToSocket, sockets)
}

// parseNumberRanges parses a comma separated list of number ranges.
// Example: "1-10,12-15,20"
func (b *BalanceIRQ) parseNumberRanges(input string) ([]string, error) {
	input = strings.TrimSpace(input)
	var numbers []string
	for _, r := range strings.Split(input, ",") {
		if strings.Contains(r, "-") {
			limits := strings.Split(r, "-")
			if len(limits) != 2 {
				return nil, fmt.Errorf("invalid range format: %s", r)
			}
			start, err := strconv.Atoi(limits[0])
			if err != nil {
				return nil, fmt.Errorf("invalid start number: %s", limits[0])
			}
			end, err := strconv.Atoi(limits[1])
			if err != nil {
				return nil, fmt.Errorf("invalid end number: %s", limits[1])
			}
			for i := start; i <= end; i++ {
				numbers = append(numbers, fmt.Sprintf("%d", i))
			}
		} else {
			numbers = append(numbers, r)
		}
	}

	return numbers, nil
}

// disableDefaultIRQBalance disables and stops the OS provided irqbalance.
func (b *BalanceIRQ) disableProvidedIRQBalance(ctx context.Context) error {
	status := b.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "status irqbalance"})
	if status.ExitCode != 4 {
		disable := b.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "disable irqbalance"})
		if disable.ExitCode != 0 {
			return fmt.Errorf("irqbalance service could not be disabled, code: %d, stderr: %s", disable.ExitCode, disable.StdErr)
		}
		stop := b.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "stop irqbalance"})
		if stop.ExitCode != 0 {
			return fmt.Errorf("irqbalance service could not be stopped, code: %d, stderr: %s", stop.ExitCode, stop.StdErr)
		}
		log.CtxLogger(ctx).Info("The irqbalance service is disabled and stopped.")
	}
	return nil
}

// installSystemdService installs a systemd service to run this OTE on boot.
func (b *BalanceIRQ) installSystemdService(ctx context.Context) error {
	name := "google-cloud-sap-agent-balanceirq.service"
	path := fmt.Sprintf("/lib/systemd/system/%s", name)
	osRelease, err := b.readFile("/etc/os-release")
	if err != nil {
		return err
	}
	if strings.Contains(string(osRelease), "SLES") {
		path = "/usr" + path
	}

	if err := b.writeFile(path, []byte(systemdContent), 0644); err != nil {
		return err
	}
	status := b.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "daemon-reload"})
	if status.ExitCode != 0 {
		return fmt.Errorf("systemd service could not be reloaded, code: %d, stderr: %s", status.ExitCode, status.StdErr)
	}
	status = b.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "enable " + name})
	if status.ExitCode != 0 {
		return fmt.Errorf("systemd service could not be enabled, code: %d, stderr: %s", status.ExitCode, status.StdErr)
	}
	status = b.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "start " + name})
	if status.ExitCode != 0 {
		return fmt.Errorf("systemd service could not be started, code: %d, stderr: %s", status.ExitCode, status.StdErr)
	}

	log.CtxLogger(ctx).Infof("The %s service is installed and started.", name)
	return nil
}
