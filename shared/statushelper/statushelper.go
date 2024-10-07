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

	"github.com/fatih/color"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/status"
)

// Define formatting options for printing to the console.
var (
	info      = color.New().PrintfFunc()
	failure   = color.New(color.FgRed).Add(color.Bold).PrintfFunc()
	success   = color.New(color.FgGreen).Add(color.Bold).PrintfFunc()
	faint     = color.New().Add(color.Faint).PrintfFunc()
	hyperlink = color.New(color.FgCyan).PrintfFunc()
)

// FetchCurrentAndLatestVersion returns the current and latest version of the
// agent package.
func FetchCurrentAndLatestVersion(ctx context.Context, packageName string) (string, error) {
	// Implement logic to get the current and latest version of the package.
	return packageName, nil
}

// CheckIAMRoles checks if the required IAM roles are present.
func CheckIAMRoles(ctx context.Context, projectID string, requiredRoles []string) error {
	// Implement logic to check if the required IAM roles are present.
	return nil
}

// PrintStatus prints the status of the agent and the configured services to
// the console with appropriate formatting and coloring.
func PrintStatus(ctx context.Context, status *spb.AgentStatus) {
	// Center the agent name between the header dashes and limit the width to 80 characters.
	info("--------------------------------------------------------------------------------\n")
	info("|%s|\n", fmt.Sprintf("%*s", -78, fmt.Sprintf("%*s", (78+len(status.GetAgentName()+" Status"))/2, status.GetAgentName()+" Status")))
	info("--------------------------------------------------------------------------------\n")
	info("Agent Status:\n")
	versionPrint := success
	if status.InstalledVersion != status.AvailableVersion {
		versionPrint = failure
	}
	info("    Installed Version: ")
	versionPrint("%s\n", status.InstalledVersion)
	info("    Available Version: ")
	versionPrint("%s\n", status.AvailableVersion)

	printBool(ctx, "    Systemd Service Enabled", status.SystemdServiceEnabled)
	printBool(ctx, "    Systemd Service Running", status.SystemdServiceRunning)
	info("    Configuration File: %s\n", status.ConfigurationFilePath)
	printBool(ctx, "    Configuration Valid", status.ConfigurationValid)
	if !status.ConfigurationValid {
		failure("        %s\n", status.ConfigurationErrorMessage)
	}

	for _, service := range status.Services {
		printServiceStatus(ctx, service)
	}
	printReferences(ctx, status.References)
	info("\n\n")
}

// printBool prints a boolean value with appropriate formatting and coloring.
func printBool(ctx context.Context, name string, value bool) {
	info("%s: ", name)
	if value {
		success("True\n")
	} else {
		failure("False\n")
	}
}

// printServiceStatus prints the status of the service to the console with
// appropriate formatting and coloring.
func printServiceStatus(ctx context.Context, status *spb.ServiceStatus) {
	info("--------------------------------------------------------------------------------\n")
	if !status.Enabled {
		faint("%s: Disabled\n", status.Name)
		return
	}
	info("%s: ", status.Name)
	success("Enabled\n")

	info("    Status: ")
	if status.FullyFunctional {
		success("Fully Functional\n")
	} else {
		failure("Error: %s\n", status.ErrorMessage)
	}

	for _, iamRole := range status.IamRoles {
		printBool(ctx, fmt.Sprintf("    %s (%s)", iamRole.Name, iamRole.Role), iamRole.Granted)
	}

	for _, configValue := range status.ConfigValues {
		defaultString := "default"
		if !configValue.IsDefault {
			defaultString = "configuration file"
		}
		if configValue.Value == "" {
			info("    %s: nil (%s)\n", configValue.Name, defaultString)
		} else {
			info("    %s: %s (%s)\n", configValue.Name, configValue.Value, defaultString)
		}
	}
}

func printReferences(ctx context.Context, references []*spb.Reference) {
	if len(references) == 0 {
		return
	}
	info("--------------------------------------------------------------------------------\n")
	info("References:\n")
	for _, reference := range references {
		info("%s: ", reference.Name)
		hyperlink("%s\n", reference.Url)
	}
}
