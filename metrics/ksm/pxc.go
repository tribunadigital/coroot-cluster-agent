package ksm

import (
	crs "k8s.io/kube-state-metrics/v2/pkg/customresourcestate"
)

func pxc() []crs.Resource {
	operator := map[string]string{"operator": "percona"}
	commonLabels := crs.Labels{
		CommonLabels: operator,
		LabelsFromPath: map[string][]string{
			"uid":       {"metadata", "uid"},
			"name":      {"metadata", "name"},
			"namespace": {"metadata", "namespace"},
		},
	}
	prefix := "mysql"
	gvk := func(kind string) crs.GroupVersionKind {
		return crs.GroupVersionKind{Group: "pxc.percona.com", Version: "v1", Kind: kind}
	}
	return []crs.Resource{
		{
			GroupVersionKind: gvk("PerconaXtraDBCluster"),
			MetricNamePrefix: &prefix,
			Labels:           commonLabels,
			ErrorLogV:        4,
			Metrics: []crs.Generator{
				infoByKey("backup_target_info", []string{"spec", "backup", "storages"}, map[string][]string{
					"s3_bucket":       {"s3", "bucket"},
					"s3_endpoint":     {"s3", "endpointUrl"},
					"s3_prefix":       {"s3", "prefix"},
					"azure_container": {"azure", "container"},
				}, "method", map[string]string{"type": "object-storage"}),
				info("backup_schedule_info", []string{"spec", "backup", "schedule"}, map[string][]string{
					"task":     {"name"},
					"schedule": {"schedule"},
					"method":   {"storageName"},
				}),
				info("backup_pitr_info", []string{"spec", "backup", "pitr"}, map[string][]string{
					"enabled": {"enabled"},
				}),
				info("cluster_status", []string{"status"}, map[string][]string{
					"status": {"state"},
				}),
			},
		},
		{
			GroupVersionKind: gvk("PerconaXtraDBClusterBackup"),
			MetricNamePrefix: &prefix,
			Labels:           commonLabels,
			ErrorLogV:        4,
			Metrics: []crs.Generator{
				info("backup_info", []string{}, map[string][]string{
					"cluster": {"spec", "pxcCluster"},
					"method":  {"spec", "storageName"},
					"kind":    {"status", "storage_type"},
					"path":    {"status", "destination"},
				}),
				info("backup_status", []string{}, map[string][]string{
					"status": {"status", "state"},
				}),
				gaugeTimestamp("backup_completed_timestamp_seconds", []string{"status"}, "completed"),
			},
		},
	}
}
