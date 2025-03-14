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

%define _confdir /etc/%{name}
%define _bindir /usr/bin
%define _docdir /usr/share/doc/%{name}
%define _servicedir /usr/share/%{name}/service
# Uncomment below line to package sap-core-app package needed by GCBDR.
# %define _gcbdr_sap_core_app_dir /etc/google-cloud-sap-agent/gcbdr

%install
# clean away any previous RPM build root
/bin/rm --force --recursive "${RPM_BUILD_ROOT}"

%include %build_rpm_install

%files
%defattr(-,root,root)
%attr(755,root,root) %{_bindir}/google_cloud_sap_agent
%config(noreplace) %attr(0664,root,root) %{_confdir}/configuration.json
%attr(0644,root,root) %{_servicedir}/%{name}.service
%attr(0644,root,root) %{_docdir}/LICENSE
%attr(0644,root,root) %{_docdir}/README.md
%attr(0644,root,root) %{_docdir}/THIRD_PARTY_NOTICES
# Uncomment below line to package sap-core-app package needed by GCBDR.
# %attr(0654,root,root) %{_gcbdr_sap_core_app_dir}

%pre
# If we need to check install / upgrade ($1 = 1 is install, $1 = 2 is upgrade)

# SAP Agent
# if the agent is running - stop it
if `systemctl is-active --quiet %{name} > /dev/null 2>&1`; then
    systemctl stop %{name}
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
    echo "${CONFIG}" > %{_confdir}/configuration.json
  fi
  # remove the v2 agent logs
  rm -fr /var/log/google-sapnetweavermonitoring-agent*  > /dev/null 2>&1
fi

# migrate HANA Monitoring Agent and remove its contents
if [ -d "/usr/sap/google-saphanamonitoring-agent/" ]; then
  # migrate
  if [ -f "/usr/sap/google-saphanamonitoring-agent/conf/configuration.yaml" ]; then
    # invoking the migration flow
    timeout 30 %{_bindir}/google_cloud_sap_agent migratehma;
    if [ $? -eq 0 ]; then
      cp /usr/sap/google-saphanamonitoring-agent/conf/configuration.yaml %{_confdir}/backup-of-hanamonitoring-configuration.yaml
      # migration successful, uninstall HANA Monitorig Agent and remove unwanted files
      # TODO: Explore how to remove the package in case of successful migration only.
      if `type "systemctl" > /dev/null 2>&1 && systemctl is-active --quiet google-saphanamonitoring-agent`; then
        systemctl stop google-saphanamonitoring-agent
      fi
      # if the agent is enabled - disable it
      if `type "systemctl" > /dev/null 2>&1 && systemctl is-enabled --quiet google-saphanamonitoring-agent`; then
        systemctl disable google-saphanamonitoring-agent
      fi
      # init.d based (RHEL 6) check
      if [ ! -d "/usr/lib/systemd/system/" ] && [ ! -d "/lib/systemd/system/" ] && [ -d "/etc/init.d" ]; then
        chkconfig --del google-saphanamonitoring-agent
        service google-saphanamonitoring-agent stop
      fi
      rm -f /lib/systemd/system/google-saphanamonitoring-agent.service
      rm -f /usr/lib/systemd/system/google-saphanamonitoring-agent.service
      rm -fr /usr/sap/google-saphanamonitoring-agent
      # if it's an init.d system
      rm -f /etc/init.d/google-saphanamonitoring-agent
      # remove the HANA Monitorig Agent logs
      rm -fr /var/log/google-saphanamonitoring-agent*  > /dev/null 2>&1
    fi
  fi
fi

# link the systemd service and reload the daemon
# RHEL
if [ -d "/lib/systemd/system/" ]; then
    cp -f %{_servicedir}/%{name}.service /lib/systemd/system/%{name}.service
    systemctl daemon-reload
fi
# SLES
if [ -d "/usr/lib/systemd/system/" ]; then
    cp -f %{_servicedir}/%{name}.service /usr/lib/systemd/system/%{name}.service
    systemctl daemon-reload
fi

# enable and start the agent
systemctl enable %{name}
systemctl start %{name}

# log usage metrics for install
timeout 30 %{_bindir}/google_cloud_sap_agent logusage -s INSTALLED &> /dev/null || true

# next steps instructions
echo ""
echo "##########################################################################"
echo "Google Cloud Agent for SAP has been installed"
echo ""
echo "You can view the logs in /var/log/%{name}.log"
echo ""
echo "Verify the agent is running with: "
echo  "    sudo systemctl status %{name}"
echo "Verify the agents SAP Host Agent metrics with: "
echo "    curl localhost:18181"
echo "Configuration is available in %{_confdir}/configuration.json"
echo ""
echo "Documentation can be found at https://cloud.google.com/solutions/sap"
echo "##########################################################################"
echo ""

%preun
# $1 == 0 is uninstall, $1 == 1 is upgrade
if [ "$1" = "0" ]; then
  # Uninstall
  # if the agent is running - stop it
  if `type "systemctl" > /dev/null 2>&1 && systemctl is-active --quiet %{name}`; then
      systemctl stop %{name}
  fi
  # if the agent is enabled - disable it
  if `type "systemctl" > /dev/null 2>&1 && systemctl is-enabled --quiet %{name}`; then
      systemctl disable %{name}
  fi
  # log usage metrics for uninstall
  timeout 30 %{_bindir}/google_cloud_sap_agent logusage -s UNINSTALLED &> /dev/null || true
fi

%postun
# $1 == 0 is uninstall, $1 == 1 is upgrade
if [ "$1" = "0" ]; then
  # Uninstall
  rm -f /lib/systemd/system/%{name}.service
  rm -f /usr/lib/systemd/system/%{name}.service
  rm -fr %{_docdir}
  rm -fr %{_confdir}
else
  # log usage metrics for upgrade
  timeout 30 %{_bindir}/google_cloud_sap_agent logusage -s UPDATED -pv "%{name}-%{VERSION}-%{RELEASE}" &> /dev/null || true
fi
