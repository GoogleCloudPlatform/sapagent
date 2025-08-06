#!/bin/bash
# set -x

REQUIRED_SA_ROLES=(
  "roles/compute.viewer"
  "roles/monitoring.viewer"
  "roles/monitoring.metricWriter"
  "roles/workloadmanager.insightWriter"
  "roles/secretmanager.secretAccessor"
  "roles/file.viewer"
)

REQUIRED_SCOPES=(
  "https://www.googleapis.com/auth/cloud-platform"
  "https://www.googleapis.com/auth/compute"
  "https://www.googleapis.com/auth/logging.write"
  "https://www.googleapis.com/auth/monitoring.write"
)

declare -A results_map
declare -a host_results

usage() {
  echo "Usage: $0 --gcp_project_id=PROJECT_ID --app_host_names=app1,app2,app3 --hana_host_names=hana1,hana2,hana3 --otherdb_host_names=otherdb1,otherdb2,otherdb3"
  echo ""
  echo "Host names should be in the format of <instance_name>/<zone>"
  echo "Example: sap-db4/us-central1-a"
  exit 1
}

join_by() {
  local IFS="$1"
  shift 1
  echo "$*"
}

onboard_host() {
  local host="$1"
  local script_path="$2"
  local index="$3"
  local is_hana_host="$4"

  host_results=("VM access scopes verified." "Service account roles verified." "Agent for SAP installed and up to date." "Ops Agent installed and up to date.")
  local all_succeeded=1
  local HOST_STEPS=3

  echo -e "\r\e[2KOnboarding host: $host"
  echo -e "\r\e[2KStep 1/${HOST_STEPS}: Verifying VM access scopes"

  sa_name=$(get_service_account "$host")
  if [ -z "${sa_name}" ]; then
    echo "Failed to get service account for $host"
    host_results[0]="Failed to get service account for $host"
    host_results[1]="Failed to get service account for $host"
    all_succeeded=0
  else
    if ! check_access_scopes "$host" "$sa_name"; then
      host_results[0]="$host is missing access scopes."
      echo "$host is missing access scopes."
      all_succeeded=0
    else
      echo -ne "\r\e[2KVM access scopes for $host verified."
      host_results[0]="VM access scopes for $host verified."
    fi

    echo -ne "\r\e[2KStep 2/${HOST_STEPS}: Checking service account roles for $host"
    echo -ne "\r\e[2KStep 2/${HOST_STEPS}: Checking for missing roles for service account: $sa_name"

    host_results[1]="Roles for service account verified."
    missing_roles=$(get_missing_sa_roles "$sa_name")
    IFS=" " read -r -a missing_roles <<< "$missing_roles"
    if [ "${#missing_roles[@]}" -ne 0 ]; then
      echo -ne "\r\e[2KStep 2/${HOST_STEPS}: Adding missing roles to service account: $sa_name"

      for role in "${missing_roles[@]}"; do
        if ! grant_sa_role "$sa_name" "$role"; then
          echo "Failed to grant role $role to service account: $sa_name"
          host_results[1]="${results[1]}\nFailed to grant role $role to service account: $sa_name"
          all_succeeded=0
        fi
      done
    fi
    echo -e "\r\e[2KStep 2/${HOST_STEPS}: Finished checking service account for required roles."
  fi

  echo -e "\r\e[2KStep 3/${HOST_STEPS}: Copying script to host: $host"

  if scp_script_to_host "$host" "$SCRIPT_PATH"; then
    echo -e "\r\e[2K\e[1A\r\e[2KStep 3/${HOST_STEPS}: Executing script on host: $host"

    if [[ "$is_hana_host" == "true" ]]; then
      if ! execute_remote_command "$host" "sudo bash /tmp/$(basename "$SCRIPT_PATH")" "--configure_hana_monitoring=yes" "--hana_instance_name=$HANA_INSTANCE_NAME" "--hana_instance_sid=$HANA_INSTANCE_SID" "--hana_instance_user_name=$HANA_INSTANCE_USER_NAME" "--hana_instance_secret_name=$HANA_INSTANCE_SECRET_NAME" "--hana_instance_host_name=$HANA_INSTANCE_HOST_NAME" "--hana_instance_port=$HANA_INSTANCE_PORT"; then
        echo "Failed to execute script on $host."
        host_results[2]="Failed to execute script on $host."
        host_results[3]="Failed to execute script on $host."
        all_succeeded=0
      fi
    else
      if ! execute_remote_command "$host" "sudo bash /tmp/$(basename "$SCRIPT_PATH")" "--configure_hana_monitoring=no"; then
        echo "Failed to execute script on $host."
        host_results[2]="Failed to execute script on $host."
        host_results[3]="Failed to execute script on $host."
        all_succeeded=0
      fi
    fi

    echo -e "\r\e[2K\e[1A\r\e[2KStep 3/${HOST_STEPS}: Script executed on $host"
  else
    echo "Failed to copy script to $host."
    host_results[2]="Failed to copy script to $host."
    host_results[3]="Failed to copy script to $host."
    all_succeeded=0
  fi

  for (( i = 0; i <= ${HOST_STEPS}; i++)); do
    echo -ne "\r\e[2K\e[1A\r\e[2K"

  done

  if [[ "$all_succeeded" -eq 0 ]]; then
    echo -e "\r\e[2KHost $host not fully onboarded. See results at the end for details."
  else
    echo -e "\r\e[2KVM ${host} onboarded successfully."
  fi

  joined_results=$(join_by "|" "${host_results[@]}")
  results_map["${instance_name}"]="${joined_results}"
}

scp_script_to_host() {
  local host="$1"
  local script_path="$2"
  local remote_path="/tmp"

  # Check if the host is empty
  if [ -z "$host" ]; then
    echo "Error: Hostname is empty."
    return 1
  fi

  # Split the host string into instance name and zone
  IFS="/" read -r instance_name zone <<< "$host"

  if [ -z "$instance_name" ]; then
    echo "Error: Instance name is empty."\
    return 1
  fi

  if [ -z "$zone" ]; then
    echo "Error: Zone is empty."
    return 1
  fi

  # Check if the script path is empty
  if [ -z "$script_path" ]; then
    echo "Error: Script path is empty."
    return 1
  fi

  # Check if the script file exists
  if [ ! -f "$script_path" ]; then
    echo "Error: Script file not found at $script_path."
    return 1
  fi

  # Check if the host is running
  status=$(gcloud compute instances describe "$instance_name" --zone "$zone" --project "$GCP_PROJECT_ID" --format="value(status)")
  if [[ "${status}" != "RUNNING" ]]; then
    echo "Host $instance_name is not running. Would you like to start the host? (y/n)"
    read -r answer
    while [[ "$answer" != "y" && "$answer" != "n" ]]; do
      echo -ne "\r\e[2K\e[1A\r\e[2KInvalid input. Please enter 'y' or 'n'."
      read -r answer
    done
    echo -ne "\e[1A\e[2K"
    echo -ne "\e[1A\e[2K"
    if [[ "$answer" == "y" ]]; then
      echo -ne "\r\e[2KStarting host $instance_name..."
      gcloud compute instances start "$instance_name" --zone "$zone" --project "$GCP_PROJECT_ID" > /dev/null 2>&1
      if [ $? -ne 0 ]; then
        echo "Failed to start host $instance_name"
        return 1
      fi
      echo -ne "\r\e[2KInstance started. Waiting for host $instance_name ssh to be ready..."
      while [[ "${status}" != "SSH READY" ]]; do
        sleep 5
        status=$(gcloud compute ssh "$instance_name" --zone "$zone" --project "$GCP_PROJECT_ID" --command="echo 'SSH READY'")
      done
    elif [[ "$answer" == "n" ]]; then
      echo "Host $instance_name is not running. Please start the host and try again."
      return 1
    else
      echo "Invalid input. Please enter 'y' or 'n'."
      return 1
    fi
  fi

  # SCP the script to the remote host
  gcloud compute scp --zone "$zone" --project "$GCP_PROJECT_ID" "$script_path" "$instance_name":/tmp  > /dev/null 2>&1
  if [ $? -ne 0 ]; then
    echo "Failed to SCP script to $host"
    return 1
  fi

  echo -ne "\r\e[2KScript copied to $host successfully."
  return 0
}

execute_remote_command() {
  local host="$1"
  local command="$2"
  shift 2 # remove the arguments

  # Check if the host is empty
  if [ -z "$host" ]; then
    echo "Error: Hostname is empty."
    return 1
  fi

  # Split the host string into instance name and zone
  IFS="/" read -r instance_name zone <<< "$host"

  if [ -z "$instance_name" ]; then
    echo "Error: Instance name is empty."
    return 1
  fi

  if [ -z "$zone" ]; then
    echo "Error: Zone is empty."
    return 1
  fi

  # Check if the command is empty
  if [ -z "$command" ]; then
    echo "Error: Command is empty."
    return 1
  fi

  # SSH and execute the command
  echo -e "\r\e[2KExecuting command on instance: $instance_name"
  gcloud compute ssh --zone "$zone" --project "$GCP_PROJECT_ID" "$instance_name" --command="$command $*" | tee "command-output.log"
  retval_bash="${PIPESTATUS[0]}" retval_zsh="${pipestatus[1]}"
  if [[ "$retval_bash" -ne 0 || "$retval_zsh" -ne 0 ]]; then
    echo "Command completed with errors on $host"
    script_result=$retval_bash $retval_zsh
    if (( (script_result & 0x01) != 0 )); then
      host_results[2]="${host_results[2]}\nScript returned unknown error"
      host_results[3]="${host_results[3]}\nScript returned unknown error"
    fi
    if (( (script_result & 0x02) != 0 )); then
      host_results[3]="${host_results[2]}\nScript failed to update or install the Ops Agent"
    fi
    if (( (script_result & 0x04) != 0 )); then
      host_results[2]="${host_results[2]}\nScript failed to install Agent for SAP"
    fi
    if (( (script_result & 0x08) != 0 )); then
      host_results[2]="${host_results[2]}\nScript failed to update Agent for SAP"
    fi
    if (( (script_result & 0x10) -ne 0 )); then
      host_results[2]="${host_results[2]}\nScript failed to enable Agent for SAP Process Metrics"
    fi
    if (( (script_result & 0x20) -ne 0 )); then
      host_results[2]="${host_results[2]}\nScript failed to enable Agent for SAP Discovery"
    fi
    if (( (script_result & 0x40) -ne 0 )); then
      host_results[2]="${host_results[2]}\nScript failed to enable Agent for SAP HANA Monitoring"
    fi
    if (( (script_result & 0x80) -ne 0 )); then
      host_results[2]="${host_results[2]}\nScript failed to configure Agent for SAP HANA Monitoring"
    fi
    if (( (script_result & 0x100) -ne 0 )); then
      host_results[2]="${host_results[2]}\nScript failed to grant root execution access for /usr/sap"
    fi
  fi

  line_count=$(wc -l < "command-output.log")
  for ((i = 0; i <= $line_count; i++)); do
    echo -ne "\r\e[2K\e[1A\r\e[2K"
  done
  rm command-output.log

  if [[ "${#results[@]}" -ne 0 ]]; then
    echo -ne "\r\e[2KCommand completed with errors on $host"
  else
    echo -ne "\r\e[2KCommand executed successfully on $host"
  fi

  return 0
}

get_service_account() {
  local host="$1"

  # Check if the host is empty
  if [ -z "$host" ]; then
    echo "Error: Hostname is empty."
    return 1
  fi

  # Split the host string into instance name and zone
  IFS="/" read -r instance_name zone <<< "$host"

  if [ -z "$instance_name" ]; then
    echo "Error: Instance name is empty."
    return 1
  fi

  if [ -z "$zone" ]; then
    echo "Error: Zone is empty."
    return 1
  fi

  sa_name=$(gcloud compute instances describe "$instance_name" --zone "$zone" --project "$GCP_PROJECT_ID" --format="value(serviceAccounts.email)")
  if [ $? -ne 0 ]; then
    echo "Failed to get service account for $instance_name"
    return 1
  fi

  echo "$sa_name"
}

get_missing_sa_roles() {
  local sa_name="$1"

  SA_ROLES=$(gcloud projects get-iam-policy "$GCP_PROJECT_ID" --flatten=bindings --filter="bindings.members:serviceAccount:$sa_name" --format="value(bindings.role)")

  MISSING_ROLES=()
  for role in "${REQUIRED_SA_ROLES[@]}"; do
    if ! grep -q "$role" <<< "$SA_ROLES"; then
      MISSING_ROLES+=("$role")
    fi
  done

  echo "${MISSING_ROLES[*]}"
}

grant_sa_role() {
  local sa_name="$1"
  local role="$2"

  if [ -z "$sa_name" ]; then
    echo "Error: Service account name is empty."
    return 1
  fi

  if [ -z "$role" ]; then
    echo "Error: Role is empty."
    return 1
  fi

  gcloud projects add-iam-policy-binding "$GCP_PROJECT_ID" --member="serviceAccount:$sa_name" --role="$role" > /dev/null 2>&1

  if [ $? -ne 0 ]; then
    echo "Failed to grant role $role to service account: $sa_name"
    return 1
  fi

  return 0
}

check_access_scopes() {
  local host="$1"
  local sa_name="$2"

  # Split the host string into instance name and zone
  IFS="/" read -r instance_name zone <<< "$host"

  if [ -z "$instance_name" ]; then
    echo "Error: Instance name is empty."
    return 1
  fi

  if [ -z "$zone" ]; then
    echo "Error: Zone is empty."
    return 1
  fi

  # Get the VM access scopes for the instance
  scopes=$(gcloud compute instances describe "$instance_name" --zone "$zone" --project "$GCP_PROJECT_ID" --format="value(serviceAccounts.scopes)")

  # If VM has blanket cloud-platform scope, then no need to modify scopes
  if [[ "$scopes" == *"https://www.googleapis.com/auth/cloud-platform"* ]]; then
    echo -ne "\r\e[2KVM $instance_name has blanket cloud-platform scope. No need to modify access scopes."
    return 0
  fi

  # Check if the required scopes are present in the VM access scopes
  MISSING_SCOPES=()
  for scope in "${REQUIRED_SCOPES[@]}"; do
    # Check if the required scope is present in te VM access scopes
    grep -q "$scope" <<< "$scopes"
    if [ $? -ne 0 ]; then
      MISSING_SCOPES+=("$scope")
    fi
  done

  if [ "${#MISSING_SCOPES[@]}" -eq 0 ]; then
    echo -ne "\r\e[2KAll required scopes are present in the VM access scopes."
    return 0
  else
    JOINED_SCOPES=$(IFS=,; echo "${MISSING_SCOPES[*]}")
    echo "The following scopes are missing from the VM access scopes: ${JOINED_SCOPES}"
    echo "To complete onboarding, please manually stop the VM and add the missing scopes:"
    echo "gcloud compute instances set-service-account $instance_name --scopes=${JOINED_SCOPES} --zone=$zone --project=$GCP_PROJECT_ID --service-account=${sa_name}"
    return 1
  fi

  echo -ne "\r\e[2KAccess scopes for VM $instance_name modified successfully."
  return 0
}

print_report() {
  local app_hosts="$1"
  local hana_hosts="$2"
  local otherdb_hosts="$3"

  IFS="," read -r -a app_hosts <<< "$app_hosts"
  IFS="," read -r -a hana_hosts <<< "$hana_hosts"
  IFS="," read -r -a otherdb_hosts <<< "$otherdb_hosts"

  if [[ ${#app_hosts[@]} -gt 0 ]]; then
    echo "Application hosts:"
    for host in "${app_hosts[@]}"; do
      # Split the host string into instance name and zone
      IFS="/" read -r instance_name zone <<< "$host"
      local host_results="${results_map[${instance_name}]}"
      IFS="|" read -r -a host_results <<< "$host_results"
      echo "  $host"
      echo -n "   - Access Scopes: "
      echo "${host_results[0]}"
      echo -n "   - Service Account Roles: "
      echo "${host_results[1]}"
      echo -n "   - Agent for SAP: "
      echo "${host_results[2]}"
      echo -n "   - Ops Agent: "
      echo "${host_results[3]}"
    done
  fi

  if [[ ${#hana_hosts[@]} -gt 0 ]]; then
    echo "HANA hosts:"
    for host in "${hana_hosts[@]}"; do
      IFS="/" read -r instance_name zone <<< "$host"
      local host_results="${results_map[${instance_name}]}"
      IFS="|" read -r -a host_results <<< "$host_results"
      echo "  $host"
      echo -n "   - Access Scopes: "
      echo "${host_results[0]}"
      echo -n "   - Service Account Roles: "
      echo "${host_results[1]}"
      echo -n "   - Agent for SAP: "
      echo "${host_results[2]}"
      echo -n "   - Ops Agent: "
      echo "${host_results[3]}"
    done
  fi

  if [[ ${#otherdb_hosts[@]} -gt 0 ]]; then
    echo "Otherdb hosts:"
    for host in "${otherdb_hosts[@]}"; do
      IFS="/" read -r instance_name zone <<< "$host"
      local host_results="${results_map[${instance_name}]}"
      IFS="|" read -r -a host_results <<< "$host_results"
      echo "  $host"
      echo -n "   - Access Scopes: "
      echo "${host_results[0]}"
      echo -n "   - Service Account Roles: "
      echo "${host_results[1]}"
      echo -n "   - Agent for SAP: "
      echo "${host_results[2]}"
      echo -n "   - Ops Agent: "
      echo "${host_results[3]}"
    done
  fi
}

if [[ "$#" -eq 0 ]]; then
  usage
  exit 1
fi

# Main script execution
for i in "$@"; do
  case $i in
  --gcp_project_id=*)
    GCP_PROJECT_ID="${i#*=}"
    shift # past argument=value
    ;;
  --app_host_names=*)
    APP_HOST_NAMES="${i#*=}"
    shift # past argument=value
    ;;
  --hana_host_names=*)
    HANA_HOST_NAMES="${i#*=}"
    shift # past argument=value
    ;;
  --otherdb_host_names=*)
    OTHERDB_HOST_NAMES="${i#*=}"
    shift # past argument=value
    ;;
  --script_path=*)
    SCRIPT_PATH="${i#*=}"
    shift # past argument=value
    ;;
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
  -*|--*)
    echo "Unknown option $i"
    exit 1
    ;;
  *)
    ;;
  esac
done

if [[ -z "$GCP_PROJECT_ID" ]]; then
  echo "Error: GCP project ID is empty."
  usage
  exit 1
fi

if [[ -n "$HANA_HOST_NAMES" ]]; then
  if [ -z "$HANA_INSTANCE_NAME" ] || [ -z "$HANA_INSTANCE_SID" ] || [ -z "$HANA_INSTANCE_USER_NAME" ] || [ -z "$HANA_INSTANCE_SECRET_NAME" ] || [ -z "$HANA_INSTANCE_HOST_NAME" ] || [ -z "$HANA_INSTANCE_PORT" ]; then
      echo "Error: Missing one or more required HANA monitoring configuration parameters. Please see usage information."
      exit 1
  fi
fi

if [[ -z "$SCRIPT_PATH" ]]; then
  echo "Error: Script path is empty."
  usage
  exit 1
fi

echo -ne "\r\e[2KEnabling Workload Manager API for project: $GCP_PROJECT_ID"
gcloud services enable workloadmanager.googleapis.com --project "$GCP_PROJECT_ID" > /dev/null 2>&1
if [ $? -ne 0 ]; then
  echo "Failed to enable Workload Manager API for project: $GCP_PROJECT_ID"
  echo "Please navigate to https://console.cloud.google.com/apis/library/workloadmanager.googleapis.com?&project=$GCP_PROJECT_ID to enable the API."
fi
echo -e "\r\e[2KWorkload Manager API enabled successfully."

if [[ -n "$APP_HOST_NAMES" ]]; then
  echo -e "\r\e[2kOnboarding app hosts"
  IFS="," read -r -a app_hosts <<< "$APP_HOST_NAMES"
  APP_HOST_COUNT=${#app_hosts[@]}
  HOST_STEPS=3
  for index in "${!app_hosts[@]}"; do
    host="${app_hosts[$index]}"
    onboard_host "$host" "$script_path" "$index"

  done
  for (( i = 0; i <= ${APP_HOST_COUNT}; i++)); do
    echo -ne "\r\e[2K\e[1A\r\e[2K"

  done
  echo -e "\r\e[2KApp VMs onboarded successfully."

fi

if [[ -n "$HANA_HOST_NAMES" ]]; then
  echo "Onboarding HANA hosts"
  IFS="," read -r -a hana_hosts <<< "$HANA_HOST_NAMES"
  HANA_HOST_COUNT=${#hana_hosts[@]}
  HANA_HOST_STEPS=3
  for index in "${!hana_hosts[@]}"; do
    host="${hana_hosts[$index]}"
    onboard_host "$host" "$script_path" "$index" true

  done
  for (( i = 0; i <= ${HANA_HOST_COUNT}; i++)); do
    echo -ne "\r\e[2K\e[1A\r\e[2K"
  done
  echo -e "\r\e[2KHANA VMs onboarded successfully."
fi

if [[ -n "$OTHERDB_HOST_NAMES" ]]; then
  echo "Onboarding otherdb hosts"
  IFS="," read -r -a otherdb_hosts <<< "$OTHERDB_HOST_NAMES"
  for host in "${otherdb_hosts[@]}"; do
    echo "Copying script to host: $host"
    if ! scp_script_to_host "$host" "$SCRIPT_PATH"; then
      echo "Failed to copy script to $host. Skipping..."
      continue
    fi

    echo "Executing script on host: $host"
    if ! execute_remote_command "$host" "bash /tmp/$(basename "$SCRIPT_PATH")"; then
      echo "Failed to execute script on $host. Skipping..."
      continue
    fi

    echo "Getting service account for $host"
    sa_name=$(get_service_account "$host")
    if [ $? -ne 0 ]; then
      echo "Failed to get service account for $host"
      continue
    fi

    echo "Granting necessary roles to service account: $sa_name"
    for role in "${REQUIRED_SA_ROLES[@]}"; do
      if ! grant_sa_role "$sa_name" "$role"; then
        echo "Failed to grant role $role to service account: $sa_name"
        continue
      fi
    done
  done
fi

print_report "$APP_HOST_NAMES" "$HANA_HOST_NAMES" "$OTHERDB_HOST_NAMES"
