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

// Package statushelper provides helper functions for checking the status
// of various agent functionalities like IAM roles, package versions etc.
package statushelper

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/safetext/shsprintf"
	"github.com/fatih/color"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/status"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
)

// Define color codes as an enum
type colorCode int

const (
	info colorCode = iota
	failure
	success
	faint
	hyperlink
)

const (
	osLinux   = "linux"
	osWindows = "windows"
)

// printColor prints a string with the specified color code.
func printColor(code colorCode, str string, a ...any) {
	var colorString string
	switch code {
	case faint:
		// faint does not have a helper function in the color package and needs to
		// be handled differently.
		colorFunc := color.New(color.Faint).PrintfFunc()
		colorFunc(str, a...)
		return
	case info:
		colorString = fmt.Sprintf(str, a...)
	case failure:
		colorString = color.RedString(str, a...)
	case success:
		colorString = color.GreenString(str, a...)
	case hyperlink:
		colorString = color.CyanString(str, a...)
	default:
		colorString = fmt.Sprintf(str, a...)
	}
	fmt.Print(colorString)
}

// FetchLatestVersion returns latest version of the agent package from the
// OS package manager.
func FetchLatestVersion(ctx context.Context, packageName string, repoName string, osType string) (string, error) {
	switch osType {
	case osLinux:
		return packageVersionLinux(ctx, repoName, packageName, commandlineexecutor.ExecuteCommand, commandlineexecutor.CommandExists)
	case osWindows:
		return packageVersionWindows(ctx, repoName, packageName, commandlineexecutor.ExecuteCommand, commandlineexecutor.CommandExists)
	default:
		return "", fmt.Errorf("unsupported OS: %s", osType)
	}
}

// CheckAgentEnabledAndRunning returns the status of the agent service.
//
// Returns a tuple as (isEnabled, isRunning, error).
func CheckAgentEnabledAndRunning(ctx context.Context, agentName string, osType string) (isEnabled bool, isRunning bool, err error) {
	switch osType {
	case osLinux:
		return agentEnabledAndRunningLinux(ctx, agentName, commandlineexecutor.ExecuteCommand)
	case osWindows:
		return agentEnabledAndRunningWindows(ctx, agentName, commandlineexecutor.ExecuteCommand)
	default:
		return false, false, fmt.Errorf("unsupported OS: %s", osType)
	}
}

// agentEnabledAndRunningLinux returns the status of a service on linux using systemctl.
//
// Returns tuple of (isEnabled, isRunning, error)
func agentEnabledAndRunningLinux(ctx context.Context, serviceName string, exec commandlineexecutor.Execute) (isEnabled bool, isRunning bool, err error) {
	// 1. Check if the service is enabled to start at boot.
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: fmt.Sprintf("systemctl is-enabled %s", serviceName),
	})
	if result.Error != nil {
		return false, false, fmt.Errorf("could not get the agent service enabled status: %s", result.StdErr)
	}

	isEnabled = false
	// systemctl is-enabled returns 0 for a number of service states, confirm
	// that the service is actually enabled.
	if result.ExitCode == 0 && strings.Contains(result.StdOut, "enabled") {
		isEnabled = true
	}

	// 2. Check if the service is running. Note that a service can be disabled
	// but still running.
	result = exec(ctx, commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: fmt.Sprintf("systemctl is-active %s", serviceName),
	})
	if result.Error != nil {
		return false, false, fmt.Errorf("could not get the agent service active status: %s", result.StdErr)
	}

	isRunning = false
	// is-running returns 0 only if the service is active.
	if result.ExitCode == 0 {
		isRunning = true
	}
	return isEnabled, isRunning, nil
}

// agentEnabledAndRunningWindows returns the status of the agent service on windows.
//
// Returns tuple of (isEnabled, isRunning, error)
func agentEnabledAndRunningWindows(ctx context.Context, serviceName string, exec commandlineexecutor.Execute) (isEnabled bool, isRunning bool, err error) {
	// TODO: Implement service status check for windows.
	return false, false, fmt.Errorf("agentEnabledAndRunningWindows is not yet implemented")
}

// CheckIAMRoles checks if the required IAM roles are present.
func CheckIAMRoles(ctx context.Context, projectID string, requiredRoles []string) error {
	// Implement logic to check if the required IAM roles are present.
	return nil
}

// packageVersionLinux returns the latest version of the agent package
// available on the linux OS's package manager.
func packageVersionLinux(ctx context.Context, packageName string, repoName string, exec commandlineexecutor.Execute, exists commandlineexecutor.Exists) (string, error) {
	var cmd string
	// Refresh metadata for only the package repo and fetch the latest version.
	// Package managers update all metadata while running any command -  we want
	// to avoid that to guard against unintended side effects and unnecessary
	// runtime.
	switch {
	case exists("yum"):
		yumCmd, err := shsprintf.Sprintf("sudo yum --disablerepo \"*\" --enablerepo \"%s\" --noplugins --quiet list updates | grep %s | awk \"/%s/ {print \\$2}\"", repoName, packageName, packageName)
		if err != nil {
			return "", fmt.Errorf("failed to get package via yum: %v", err)
		}
		cmd = yumCmd
	case exists("zypper"):
		zypperCmd, err := shsprintf.Sprintf("sudo zypper --quiet refresh %s && sudo zypper --non-interactive --no-refresh info %s  | awk \"/Version/ {print \\$3}\"", repoName, packageName)
		if err != nil {
			return "", fmt.Errorf("failed to get package viz zypper: %v", err)
		}
		cmd = zypperCmd
	default:
		return "", fmt.Errorf("no supported package manager found (yum or zypper)")
	}

	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "/bin/sh",
		ArgsToSplit: fmt.Sprintf(" -c '%s'", cmd),
	})

	if result.Error != nil || result.StdOut == "" {
		return "", fmt.Errorf("failed to fetch latest version: %#v", result)
	}

	return strings.TrimSpace(result.StdOut), nil
}

// packageVersionWindows returns the latest version of the agent package
// available on the windows OS's package manager.
func packageVersionWindows(ctx context.Context, packageName string, repoName string, exec commandlineexecutor.Execute, exists commandlineexecutor.Exists) (string, error) {
	// TODO: Implement service status check for windows
	return "", fmt.Errorf("packageVersionWindows is not yet implemented")
}

// PrintStatus prints the status of the agent and the configured services to
// the console with appropriate formatting and coloring.
func PrintStatus(ctx context.Context, status *spb.AgentStatus) {
	// Center the agent name between the header dashes and limit the width to 80 characters.
	printColor(info, "--------------------------------------------------------------------------------\n")
	printColor(info, "|%s|\n", fmt.Sprintf("%*s", -78, fmt.Sprintf("%*s", (78+len(status.GetAgentName()+" Status"))/2, status.GetAgentName()+" Status")))
	printColor(info, "--------------------------------------------------------------------------------\n")
	printColor(info, "Agent Status:\n")
	versionColor := success
	if status.InstalledVersion != status.AvailableVersion {
		versionColor = failure
	}
	printColor(info, "    Installed Version: ")
	printColor(versionColor, "%s\n", status.InstalledVersion)
	printColor(info, "    Available Version: ")
	printColor(versionColor, "%s\n", status.AvailableVersion)

	printState(ctx, "    Systemd Service Enabled", status.SystemdServiceEnabled)
	printState(ctx, "    Systemd Service Running", status.SystemdServiceRunning)
	printColor(info, "    Configuration File: %s\n", status.ConfigurationFilePath)
	printState(ctx, "    Configuration Valid", status.ConfigurationValid)
	if status.ConfigurationValid != spb.State_SUCCESS_STATE {
		printColor(failure, "        %s\n", status.ConfigurationErrorMessage)
	}

	for _, service := range status.Services {
		printServiceStatus(ctx, service)
	}
	printReferences(ctx, status.References)
	printColor(info, "\n\n")
}

// printState prints a valid/invalid/error state with formatting and coloring.
func printState(ctx context.Context, name string, state spb.State) {
	printColor(info, "%s: ", name)
	switch state {
	case spb.State_SUCCESS_STATE:
		printColor(success, "True\n")
	case spb.State_FAILURE_STATE:
		printColor(failure, "False\n")
	default:
		printColor(failure, "Error: could not determine status\n")
	}
}

// printServiceStatus prints the status of the service to the console with
// appropriate formatting and coloring.
func printServiceStatus(ctx context.Context, status *spb.ServiceStatus) {
	printColor(info, "--------------------------------------------------------------------------------\n")
	if !status.Enabled {
		printColor(faint, "%s: Disabled\n", status.Name)
		return
	}
	printColor(info, "%s: ", status.Name)
	printColor(success, "Enabled\n")

	printColor(info, "    Status: ")
	if status.FullyFunctional {
		printColor(success, "Fully Functional\n")
	} else {
		printColor(failure, "Error: %s\n", status.ErrorMessage)
	}

	for _, iamRole := range status.IamRoles {
		printState(ctx, fmt.Sprintf("    %s (%s)", iamRole.Name, iamRole.Role), iamRole.Granted)
	}

	for _, configValue := range status.ConfigValues {
		defaultString := "default"
		if !configValue.IsDefault {
			defaultString = "configuration file"
		}
		if configValue.Value == "" {
			printColor(info, "    %s: nil (%s)\n", configValue.Name, defaultString)
		} else {
			printColor(info, "    %s: %s (%s)\n", configValue.Name, configValue.Value, defaultString)
		}
	}
}

func printReferences(ctx context.Context, references []*spb.Reference) {
	if len(references) == 0 {
		return
	}
	printColor(info, "--------------------------------------------------------------------------------\n")
	printColor(info, "References:\n")
	for _, reference := range references {
		printColor(info, "%s: ", reference.Name)
		printColor(hyperlink, "%s\n", reference.Url)
	}
}
