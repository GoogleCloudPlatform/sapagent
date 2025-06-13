# Google Cloud Agent for SAP OS Policies

These policies can be used with the
[Google Cloud VM Manager OS Configuration Management](https://cloud.google.com/compute/docs/os-configuration-management)
to automatically install the Agent for SAP on Google Cloud instances.

## google-cloud-sap-agent-policy.yaml

This policy can be used with the Google Cloud Console OS Policy create UI to
install and update the Google Cloud Agent for SAP on Google Cloud instances.

## google-cloud-sap-agent-policy-gcloud.yaml

This policy can be used with the gcloud command line to automatically
install and update the Google Cloud Agent for SAP on Google Cloud instances.

Example installation:

```
gcloud compute os-config os-policy-assignments create OS_POLICY_ASSIGNMENT_ID \
   --project=PROJECT \
   --location=ZONE \
   --file=google-cloud-sap-agent-policy.yaml \
   --async
```

## License and Copyright

Copyright 2022 Google LLC.

Apache License, Version 2.0
