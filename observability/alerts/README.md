# Alerts for SAP Agent


##SAP HANA Availability
**Filename**: [hana_availability.json](hana_availability.json)

This file contains a json representation for cloud monitoring alert for `SAP HANA Availability`.

**Alert Description**: An alert is triggered if the HANA standalone system is not up and running. The metrics frequency is 5 seconds(default). The query window is 1m to tolerate one-off measurements. The following metrics are used in this alert:

```
workloads.googleapis.com/sap/mntmode
    0 -- no maintenance; alert if system not available
    1 -- maintenance; no alert (availability of system does not matter)
workloads.googleapis.com/sap/hana/availability
    value = 3 -- system available
    value < 3 -- system not available
```

##SAP HANA HA Availability
**Filename**: [hana_ha_availability.json](hana_ha_availability.json)

This file contains a json representation for cloud monitoring alert for `SAP HANA HA Availability`.

**Alert Description**: An alert is triggered if the HANA high-availability cluster with replication is
not fully up and running. The metrics frequency is 5 seconds(default). The query window is 1m to tolerate one-off measurements. The following metrics are used in this alert:

```
workload.googleapis.com/sap/mntmode
    0 -- no maintenance; alert if system not available
    1 -- maintenance; no alert
workload.googleapis.com/sap/hana/ha/availability
    value = 4 -- system available (primary online & replication running)
    value < 4 -- there is some issue with primary or replication
```

##Backint Failure
**Filename**: [backint_failure.json](backint_failure.json)

This file contains a json representation for cloud monitoring alert for `Backint Failure`.

**Alert Description**: An alert is triggered if Backint writes an ERROR log to Cloud Logging.
`log_to_cloud` must be enabled in the parameters file before running Backint to enable writing
logs to Cloud Logging. By default, the notification rate limit is set to 300 seconds (5 minutes).
