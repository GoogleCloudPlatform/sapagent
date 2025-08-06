#!/bin/bash

# set -x

# Sript to install and configure the Google Cloud Agent for SAP for Workload
# Manager Observability

# Return value from this script is a bitmask of the following:
# 0: Success
# 1: Unknown error
# 2: Failed to install or update ops agent
# 4: Failed to install SAP agent
# 8: Failed to update SAP agent
# 16: Failed to enable process monitoring SAP agent feature
# 32: Failed to enable discovery SAP agent feature
# 64: Failed to enable HANA monitoring feature
# 128: Failed to configure HANA monitoring
# 256: Failed to grant root execute permission for /usr/sap

AGENT_CONFIG_PATH="/etc/google-cloud-sap-agent/configuration.json"

usage() {
  echo "Usage: $0 --configure_hana_monitoring=<yes|no> [OPTIONS]"
  echo ""
  echo "If configuring HANA monitoring, the following parameters are required:"
  echo "  --configure_hana_monitoring: Configure HANA monitoring. (yes|no)"
  echo "  --hana_instance_name: The name of the HANA instance to monitor."
  echo "  --hana_instance_sid: The SID of the HANA instance to monitor."
  echo "  --hana_instance_user_name: The user name of the HANA instance to monitor."
  echo "  --hana_instance_secret_name: The secret name of the HANA instance to monitor."
  echo "  --hana_instance_host_name: The host name of the HANA instance to monitor."
  echo "  --hana_instance_port: The port of the HANA instance to monitor."
}

determine_os() {
  if [[ $(sudo grep -c 'NAME="Red Hat' /etc/os-release) -gt 0 ]]; then
    OS="RHEL"
    PACKAGE_MANAGER="yum"
  elif [[ $(sudo grep -c 'NAME="SUSE' /etc/os-release) -gt 0 ]]; then
    OS="SLES"
    PACKAGE_MANAGER="zypper"
  else
    echo "Error: Unsupported operating system. This script supports RHEL and SLES."
    exit 1
  fi
  echo "Operating System: $OS"
  echo "Package Manager: $PACKAGE_MANAGER"
}

# Function to install a package
install_package() {
  local package_name="$1"
  local package_manager="$2"

  if [[ -z "$package_name" ]] || [[ -z "$package_manager" ]]; then
    echo "Error: Missing package name or package manager in install_package function."
    return 1
  fi

  echo -ne "\r\e[2kInstalling $package_name... \r"
  if [[ "$package_manager" == "yum" ]]; then
    sudo cat << EOM > /etc/yum.repos.d/google-cloud-sap-agent.repo
[google-cloud-sap-agent]
name=Google Cloud Agent for SAP
baseurl=https://packages.cloud.google.com/yum/repos/google-cloud-sap-agent-el$(cat /etc/redhat-release | cut -d . -f 1 | tr -d -c 0-9)-\$basearch
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=https://packages.cloud.google.com/yum/doc/yum-key.gpg https://packages.cloud.google.com/yum/doc/rpm-package-key.gpg
EOM
    sudo yum install -y "$package_name" > /dev/null 2>&1
    if [[ $? -ne 0 ]]; then
      echo "Failed to install $package_name using yum."
      return 1
    fi
  elif [[ "$package_manager" == "zypper" ]]; then
    sudo zypper addrepo --refresh https://packages.cloud.google.com/yum/repos/google-cloud-sap-agent-sles12-\$basearch google-cloud-sap-agent > /dev/null
    sudo zypper install -y "$package_name" > /dev/null
    if [[ $? -ne 0 ]]; then
      echo "Failed to install $package_name using zypper."
      return 1
    fi
  else
    echo "Error: Unsupported package manager: $package_manager"
    return 1
  fi
  echo -ne "\r\e[2k$package_name installed successfully. \r"
  return 0
}

# Function to install a package
update_package() {
  local package_name="$1"
  local package_manager="$2"

  if [[ -z "$package_name" ]] || [[ -z "$package_manager" ]]; then
    echo "Error: Missing package name or package manager in install_package function."
    return 1
  fi

  echo -ne "\r\e[2kUpdating $package_name...\r"
  if [[ "$package_manager" == "yum" ]]; then
    sudo yum update -y "$package_name"  > /dev/null 2>&1
    if [[ $? -ne 0 ]]; then
      echo "Failed to update $package_name using yum."
      return 1
    fi
  elif [[ "$package_manager" == "zypper" ]]; then
    sudo zypper update -y "$package_name" > /dev/null
    if [[ $? -ne 0 ]]; then
      echo "Failed to update $package_name using zypper."
      return 1
    fi
  else
    echo "Error: Unsupported package manager: $package_manager"
    return 1
  fi
  echo -ne "\r\e[2k$package_name updated successfully. \r"
  return 0
}

create_default_config() {
  if [[ ! -f "$AGENT_CONFIG_PATH" ]]; then
    echo -ne "\r\e[2kNo configuration file found, creating a new one. \r"
    touch "$AGENT_CONFIG_PATH"
    cat >> "$AGENT_CONFIG_PATH" <<EOF
{
  "provide_sap_host_agent_metrics": true,
  "log_level": "INFO",
  "log_to_cloud": true,
  "collection_configuration": {
      "collect_workload_validation_metrics": true,
      "collect_process_metrics": false
  },
  "discovery_configuration": {
      "enable_discovery": true
  },
  "hana_monitoring_configuration": {
      "enabled": false
  }
}
EOF
  fi
}

enable_agent_feature() {
  local feature_name="$1"
  echo -ne "\r\e[2kEnabling SAP Agent feature: $feature_name...\r"

  if [[ -z "$feature_name" ]]; then
    echo "Error: Missing feature name in enable_agent_feature function."
    return 1
  fi

  sudo google_cloud_sap_agent configure -feature "${feature_name}" -enable > /dev/null 2>&1
  if [[ ! $? -eq 0 ]]; then
    echo "Failed to enable $feature_name."
    return 1
  fi
  echo -ne "\r\e[2k$feature_name enabled successfully. \r"
  return 0
}

configure_hana_monitoring() {
  HANA_INSTANCE_CONFIG=$(cat <<EOF
    "hana_instances": [
        {
            "name": "${HANA_INSTANCE_NAME}",
            "sid": "${HANA_INSTANCE_SID}",
            "host": "${HANA_INSTANCE_HOST_NAME}",
            "port": "${HANA_INSTANCE_PORT}",
            "user": "${HANA_INSTANCE_USER_NAME}",
            "secret_name": "${HANA_INSTANCE_SECRET_NAME}"
        }
    ],
EOF
)

  # Check if the hana instance is already configured
  if [[ -f "$AGENT_CONFIG_PATH" ]]; then
    echo -ne "\r\e[2kChecking for existing hana instance configuration... \r"

    HANA_INSTANCE_CONFIG_EXISTING=$(grep -c '"hana_instances"\s*:' "$AGENT_CONFIG_PATH")
    if [[ "$HANA_INSTANCE_CONFIG_EXISTING" -ne 0 ]]; then
      echo -ne "\r\e[2kUsing existing hana instance configuration. \r"
      return 0
    fi
  fi
  echo -ne "\r\e[2kAdding hana instance configuration to $AGENT_CONFIG_PATH... \r"

  # Find the spot to insert the hana instance configuration
  local temp_file=$(mktemp)

  while IFS= read -r line; do
    echo "$line" >> "$temp_file"
    if [[ "$line" == *"hana_monitoring_configuration"* ]]; then
      echo "${HANA_INSTANCE_CONFIG}" >> "$temp_file"
    fi
  done < "$AGENT_CONFIG_PATH"
  echo "$line" >> "$temp_file"

  sudo mv "$temp_file" "$AGENT_CONFIG_PATH" > /dev/null 2>&1

  echo -ne "\r\e[2kDone adding hana instance configuration to $AGENT_CONFIG_PATH \r"
}

if [[ "$#" -eq 0 ]]; then
  usage
  exit 128
fi

# Main script execution
for i in "$@"; do
  case $i in
  --hana_instance_name=*)
    HANA_INSTANCE_NAME="${i#*=}"
    shift # past argument=value
    ;;
  --hana_instance_sid=*)
    HANA_INSTANCE_SID="${i#*=}"
    shift # past argument=value
    ;;
  --hana_instance_user_name=*)
    HANA_INSTANCE_USER_NAME="${i#*=}"
    shift # past argument=value
    ;;
  --hana_instance_secret_name=*)
    HANA_INSTANCE_SECRET_NAME="${i#*=}"
    shift # past argument=value
    ;;
  --hana_instance_host_name=*)
    HANA_INSTANCE_HOST_NAME="${i#*=}"
    shift # past argument=value
    ;;
  --hana_instance_port=*)
    HANA_INSTANCE_PORT="${i#*=}"
    shift # past argument=value
    ;;
  --configure_hana_monitoring=*)
    CONFIGURE_HANA_MONITORING="${i#*=}"
    shift # past argument=value
    ;;
  -*|--*)
    echo "Unknown option $i"
    exit 1
    ;;
  *)
    ;;
  esac
done

# Check each expected parameter is set
if [[ -n "${CONFIGURE_HANA_MONITORING}" ]]; then
  if [[ "${CONFIGURE_HANA_MONITORING,,}" =~ ^(yes|y)$ ]]; then
    if [ -z "$HANA_INSTANCE_NAME" ] || [ -z "$HANA_INSTANCE_SID" ] || [ -z "$HANA_INSTANCE_USER_NAME" ] || [ -z "$HANA_INSTANCE_SECRET_NAME" ] || [ -z "$HANA_INSTANCE_HOST_NAME" ] || [ -z "$HANA_INSTANCE_PORT" ]; then
      echo "Error: Missing one or more required HANA monitoring configuration parameters. Please see usage information."
      exit 1
    fi
  elif ! [[ "${CONFIGURE_HANA_MONITORING,,}" =~ ^(no|n)$ ]]; then
    echo "Error: Invalid value for CONFIGURE_HANA_MONITORING. Please see usage information."
    exit 1
  fi
fi

# Determine the OS and set the package manager
determine_os
if [ $? -ne 0 ]; then
  echo "Failed to determine OS. Exiting."
  exit 1
fi

result=0

# Install the google-cloud-ops-agent
curl -sSO https://dl.google.com/cloudagents/add-google-cloud-ops-agent-repo.sh > /dev/null 2>&1
sudo bash add-google-cloud-ops-agent-repo.sh --also-install > /dev/null 2>&1
if [ $? -ne 0 ]; then
  echo "Failed to install google-cloud-ops-agent."
  result|=2
fi

# Install the google-cloud-sap-agent
install_package "google-cloud-sap-agent" "$PACKAGE_MANAGER"
if [ $? -ne 0 ]; then
  echo "Failed to install google-cloud-sap-agent. Exiting."
  result|=4
  exit ${result}
fi

update_package "google-cloud-sap-agent" "$PACKAGE_MANAGER"
if [ $? -ne 0 ]; then
  echo "Failed to update google-cloud-sap-agent. Exiting."
  result|=8
  exit ${result}
fi

# Enable process metrics collection for google-cloud-sap-agent
enable_agent_feature "process_metrics"
if [ $? -ne 0 ]; then
  echo "Failed to enable process metrics."
  result|=16
fi

# Enable discovery for google-cloud-sap-agent
enable_agent_feature "sap_discovery"
if [ $? -ne 0 ]; then
  echo "Failed to enable discovery."
  result|=32
fi


if [[ "${CONFIGURE_HANA_MONITORING,,}" =~ ^(yes|y)$ ]]; then
  echo -ne "\r\e[2kConfiguring HANA monitoring... \r"

  # Enable HANA monitoring metrics collection for google-cloud-sap-agent
  enable_agent_feature "hana_monitoring"
  if [ $? -ne 0 ]; then
    echo "Failed to enable HANA monitoring."
    result|=64
  fi

  configure_hana_monitoring
  if [ $? -ne 0 ]; then
    echo "Failed to configure HANA monitoring."
    result|=128
  fi
fi

echo -ne "\r\e[2kGranting root execute permission for /usr/sap \r"
sudo chmod +x /usr/sap
if [ $? -ne 0 ]; then
  echo "Failed to grant root execute permission for /usr/sap."
  result|=256
fi

# Restart the agent to apply the new configuration
sudo systemctl restart google-cloud-sap-agent

if [[ $result -ne 0 ]]; then
  echo "Script completed with errors."
  exit $result
fi

echo -e "Script completed successfully."
exit 0
