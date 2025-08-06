# Workload Manager Observability Onboarding

Google Cloud's Workload Manager provides an Observability dashboard for
monitoring your SAP Systems as a whole. This feature depends on data collected
by the Google Cloud Agent for SAP. This Observability feature comes at no extra
cost to users, however some of the features of the Agent for SAP that are
required for Observability to function properly do incur some extra cost.

The required Agent for SAP features are:

 * [Process metrics](https://cloud.google.com/sap/docs/agent-for-sap/latest/process-monitoring)
 * [SAP System Discovery](https://cloud.google.com/sap/docs/agent-for-sap/latest/planning#sap-system-discovery)
 * [HANA Monitoring metrics](https://cloud.google.com/sap/docs/agent-for-sap/latest/monitoring-sap-hana) (where applicable)

Individually enabling all of these features, and configuring the service
accounts for their requisite permissions, is repetitive. We have included this
pair of helper bash scripts to both check, and configure the VMs for your SAP
System to work with Observability.

To use the scripts, begin by copying them to either your local machine, or a VM
within your Google Cloud Project.

```
$ wget https://raw.githubusercontent.com/GoogleCloudPlatform/sapagent/refs/heads/main/tools/wlm_observability/wlm-onboarding-batch.sh \
 https://raw.githubusercontent.com/GoogleCloudPlatform/sapagent/refs/heads/main/tools/wlm_observability/wlm-onboarding.sh
```

This copies the two scripts to wherever you invoke the wget command from.

wlm-onboarding.sh is intended to be run directly on the VMs of the SAP System.
It takes the following arguments:

 * `--configure_hana_monitoring` Set this to "yes" or "no" to enable or ignore
HANA monitoring respectively.
 * `--hana_instance_name` The name of the HANA instance to monitor. This is usually just "local".
 * `--hana_instance_sid` The SID of the HANA instance to monitor.
 * `--hana_instance_user_name` The user name of the HANA instance to monitor. This is usually "system"
 * `--hana_instance_secret_name` The secret name of the HANA instance to monitor.
 * `--hana_instance_host_name` The host name of the HANA instance to monitor. This is usually "localhost"
 * `--hana_instance_port` The port of the HANA instance to monitor. This is usually `3<HANA Instance Number>15`. Eg. instance number 42 uses port 34215.

This script will ensure the following:

1. The Google Cloud Ops Agent is installed and up to date.
1. The Google Cloud Agent for SAP is installed and up to date.
1. Process metrics for the Agent for SAP.
1. SAP System Discovery for the Agent for SAP is enabled.
1. HANA Monitoring for the Agent for SAP is enabled and configured if requested.
1. Root user has execute permissions on the /usr/sap directory.
  * This is required for SAP System Discovery. See our [troubleshooting guide](https://cloud.google.com/sap/docs/agent-for-sap/latest/troubleshooting#issue_system_discovery_fails_due_to_lack_of_execute_permission_for_the_usrsap_directory)

wlm-onboarding-batch.sh is a script to repeat the process of running the
wlm-onboarding.sh script on each of the hosts in the system, and additionally to
manage the project-level requirements for WLM Observability. This includes:

 * VM Service accounts have the following roles:
   * roles/compute.viewer
   * roles/monitoring.viewer
   * roles/monitoring.metricWriter
   * roles/workloadmanager.insightWriter
   * roles/secretmanager.secretAccessor
   * roles/file.viewer
 * The Workload Manager API is enabled

Additionally the batch script will check the VMs for the requisite access
scopes for these features. If any of the access scopes is missing then it will
be printed at the end of the execution. Modifying VM access scopes requires the
VM to be shut down and thus is too disruptive an operation for this script to
attempt to do automatically. The required access scopes are:

 * https://www.googleapis.com/auth/cloud-platform
 * https://www.googleapis.com/auth/compute
 * https://www.googleapis.com/auth/logging.write
 * https://www.googleapis.com/auth/monitoring.write

The batch script takes the following arguments:

 * `--script_path` The path (absolute or relative) to the wlm-onboarding.sh script.
 * `--gcp_project_id` The ID of the project in which the VM resources reside
 * `--app_host_names` A comma separated list of host/zone pairs representing the list of Application or Central Services VMs in the system.
 * `--hana_host_names` A comma separated list of host/zone pairs representing the list of HANA VMs in the system.
 * `--otherdb_host_names` A comma separated list of host/zone pairs representing the list of non-HANA Database VMs in the system.

All hosts are specified as a pair of `<instance name>/<instance zone>`. Eg.

```
--app_host_names=app1/us-central1-a,app2/us-central1-b,app3/us-west1-a
```

Additionally the batch script requires all of the `hana_` parameters from the
script above if any HANA VMs are included.

A full example invocation of the batch script is below:

```
wlm-onboarding-batch.sh --script_path=wlm-onboarding.sh \
  --gcp_project_id=my-gcp-project \
  --app_host_names=app1/us-central1-a,app2/us-central1-c,ascs1/us-central1-a,ascs2/us-central1-c \
  --hana_host_names=hana1/us-central1-a,hana2/us-central1-c \
  --hana_instance_name=local \
  --hana_instance_sid=AAA \
  --hana_instance_user_name=system \
  --hana_instance_secret_name=db_pw \
  --hana_instance_host_name=localhost \
  --hana_instance_port=30015
```
