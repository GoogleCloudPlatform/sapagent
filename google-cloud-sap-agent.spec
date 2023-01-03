# Copyright 2022 Google Inc.

%include %build_rpm_options

Summary: "Google Cloud Agent for SAP."
Group: Application
License: ASL 2.0
Vendor: Google, Inc.
Provides: google-cloud-sap-agent
Obsoletes: google-sapnetweavermonitoring-agent

%description
"Google Cloud Agent for SAP."

%install
# clean away any previous RPM build root
/bin/rm --force --recursive "${RPM_BUILD_ROOT}"

%include %build_rpm_install

%files -f %build_rpm_files
%defattr(-,root,root)

%pre
# If we need to check install / upgrade ($1 = 1 is install, $1 = 2 is upgrade)

# SAP Agent
# if the agent is running - stop it
if `systemctl is-active --quiet google-cloud-sap-agent > /dev/null 2>&1`; then
    systemctl stop google-cloud-sap-agent
fi

# v2 NetWeaver agent detection - stop it if it is running
if `systemctl is-active --quiet google-sapnetweavermonitoring-agent > /dev/null 2>&1`; then
    systemctl stop google-sapnetweavermonitoring-agent
    systemctl disable google-sapnetweavermonitoring-agent
fi

# v1 NetWeaver agent detection - stop it if it is running and disable the cron(s)
if [[ -d /opt/gcpmetricsprovider ]]; then
  # remove crontab entries if they exist
  crontab -l | grep -v "/opt/gcpmetricsprovider/monitor.sh" | crontab -
  crontab -l | grep -v "/opt/gcpmetricsprovider/update.sh" | crontab -
  # kill the old agent if it is running
  kill $(ps aux | grep '[g]cpmetricsprovider.jar' | head -n1 | awk '{print $2}')
  # remove the old agent
  rm -fr /opt/gcpmetricsprovider
fi


%post

# v2 NetWeaver agent migrate configuration and remove
if [ -d "/usr/sap/google-sapnetweavermonitoring-agent/" ]; then
  # migrate
  if [ -f "/usr/sap/google-sapnetweavermonitoring-agent/conf/configuration.yaml" ]; then
    # create the config that we will append to
    CONFIG="{"$'\n'
    CONFIG="${CONFIG}  \"provide_sap_host_agent_metrics\": true,"$'\n'
    CONFIG="${CONFIG}  \"log_level\": \"INFO\","$'\n'
    CONFIG="${CONFIG}  \"log_to_file\": true,"$'\n'
    # bare_metal
    BARE_METAL=`grep bare_metal "/usr/sap/google-sapnetweavermonitoring-agent/conf/configuration.yaml" | cut -d' ' -f2 | xargs`
    if [ "${BARE_METAL}" = "true" ]; then
      CONFIG="${CONFIG}  \"bare_metal\": true,"$'\n'
    fi

    # collect_workload_metrics
    CONFIG="${CONFIG}  \"collection_configuration\": {"$'\n'
    WLM=`grep collect_workload_metrics "/usr/sap/google-sapnetweavermonitoring-agent/conf/configuration.yaml" | cut -d' ' -f2 | xargs`
    if [ "${WLM}" = "true" ]; then
      CONFIG="${CONFIG}    \"collect_workload_validation_metrics\": true,"$'\n'
    else
      CONFIG="${CONFIG}    \"collect_workload_validation_metrics\": false,"$'\n'
    fi
    CONFIG="${CONFIG}    \"collect_process_metrics\": false"$'\n'"  }"

    # project_id, zone, instance_id
    PROJECT_ID=`grep project_id "/usr/sap/google-sapnetweavermonitoring-agent/conf/configuration.yaml" | cut -d' ' -f2 | xargs`
    ZONE=`grep zone "/usr/sap/google-sapnetweavermonitoring-agent/conf/configuration.yaml" | cut -d' ' -f2 | xargs`
    INSTANCE_ID=`grep instance_id "/usr/sap/google-sapnetweavermonitoring-agent/conf/configuration.yaml" | cut -d' ' -f2 | xargs`
    if [ "${PROJECT_ID}" != "" ] || [ "${ZONE}" != "" ] || [ "${INSTANCE_ID}" != "" ]; then
      CONFIG="${CONFIG},"$'\n'"  \"cloud_properties\": {"
      COMMA="false"
      if [ "${PROJECT_ID}" != "" ]; then
        CONFIG="${CONFIG}"$'\n'"    \"project_id\": \"${PROJECT_ID}\""
        COMMA="true"
      fi
      if [ "${ZONE}" != "" ]; then
        if [ "${COMMA}" = "true" ]; then
          CONFIG="${CONFIG},"
        fi
        CONFIG="${CONFIG}"$'\n'"    \"zone\": \"${ZONE}\""
        COMMA="true"
      fi
      if [ "${INSTANCE_ID}" != "" ]; then
        if [ "${COMMA}" = "true" ]; then
          CONFIG="${CONFIG},"
        fi
        CONFIG="${CONFIG}"$'\n'"    \"instance_id\": \"${INSTANCE_ID}\""
      fi
      CONFIG="${CONFIG}"$'\n'"  }"
    fi
    # finally close off the config
    CONFIG="${CONFIG}"$'\n'"}"
    # move the migrated configuration into place
    echo "${CONFIG}" > /usr/sap/google-cloud-sap-agent/conf/configuration.json
  fi
  # remove the v2 agent logs
  rm -fr /var/log/google-sapnetweavermonitoring-agent*  > /dev/null 2>&1
fi

# link the systemd service and reload the daemon
# RHEL
if [ -d "/lib/systemd/system/" ] && [ ! -f "/lib/systemd/system/google-cloud-sap-agent.service" ]; then
    cp -f /usr/sap/google-cloud-sap-agent/service/google-cloud-sap-agent.service /lib/systemd/system/google-cloud-sap-agent.service
    systemctl daemon-reload
fi
# SLES
if [ -d "/usr/lib/systemd/system/" ] && [ ! -f "/usr/lib/systemd/system/google-cloud-sap-agent.service" ]; then
    cp -f /usr/sap/google-cloud-sap-agent/service/google-cloud-sap-agent.service /usr/lib/systemd/system/google-cloud-sap-agent.service
    systemctl daemon-reload
fi

# enable and start the agent
systemctl enable google-cloud-sap-agent
systemctl start google-cloud-sap-agent

# log usage metrics for install
timeout 30 /usr/sap/google-cloud-sap-agent/google-cloud-sap-agent --log-usage -lus INSTALLED || true

# next steps instructions
echo ""
echo "##########################################################################"
echo "Google Cloud Agent for SAP has been installed"
echo ""
echo "You can view the logs in /var/log/google-cloud-sap-agent.log"
echo ""
echo "Verify the agent is running with: "
echo  "    sudo systemctl status google-cloud-sap-agent"
echo "Verify the agents SAP Host Agent metrics with: "
echo "    curl localhost:18181"
echo "Configuration is available in /usr/sap/google-cloud-sap-agent/conf/configuration.json"
echo ""
echo "Documentation can be found at https://cloud.google.com/solutions/sap"
echo "##########################################################################"
echo ""

%preun
# $1 == 0 is uninstall, $1 == 1 is upgrade
if [ "$1" = "0" ]; then
  # Uninstall
  # if the agent is running - stop it
  if `type "systemctl" > /dev/null 2>&1 && systemctl is-active --quiet google-cloud-sap-agent`; then
      systemctl stop google-cloud-sap-agent
  fi
  # if the agent is enabled - disable it
  if `type "systemctl" > /dev/null 2>&1 && systemctl is-enabled --quiet google-cloud-sap-agent`; then
      systemctl disable google-cloud-sap-agent
  fi
  # log usage metrics for uninstall
  timeout 30 /usr/sap/google-cloud-sap-agent/google-cloud-sap-agent --log-usage -lus UNINSTALLED || true
fi
if [ "$1" = "1" ]; then
  VERSION_BEFORE=$(rpm -q google-cloud-sap-agent)
fi

%postun
# $1 == 0 is uninstall, $1 == 1 is upgrade
if [ "$1" = "0" ]; then
  # Uninstall
  rm -f /lib/systemd/system/google-cloud-sap-agent.service
  rm -f /usr/lib/systemd/system/google-cloud-sap-agent.service
  rm -fr /usr/sap/google-cloud-sap-agent
  rm -f /var/log/google-cloud-sap-agent*
# else
  # upgrade
  # log usage metrics for upgrade
  timeout 30 /usr/sap/google-cloud-sap-agent/google-cloud-sap-agent --log-usage -lus UPDATED -lup "${VERSION_BEFORE}" || true
fi
