package mysql

import (
	"strconv"
	"strings"

	"github.com/coroot/coroot-cluster-agent/common"
	"github.com/prometheus/client_golang/prometheus"
)

const nanoSeconds = 1e9

func wsrepEnabled(variables map[string]string) bool {
	provider := variables["wsrep_provider"]
	return provider != "" && !strings.EqualFold(provider, "none")
}

func (c *Collector) galeraMetrics(ch chan<- prometheus.Metric) {
	if !c.isGalera {
		return
	}
	metricFromVariable(ch, dWsrepClusterSize, "wsrep_cluster_size", prometheus.GaugeValue, c.globalStatus)
	if comment := c.globalStatus["wsrep_local_state_comment"]; comment != "" {
		ch <- common.Gauge(dWsrepLocalState, 1, strings.ToLower(comment))
	}
	metricFromVariable(ch, dWsrepLocalRecvQueue, "wsrep_local_recv_queue", prometheus.GaugeValue, c.globalStatus)
	metricFromVariable(ch, dWsrepLocalSendQueue, "wsrep_local_send_queue", prometheus.GaugeValue, c.globalStatus)

	if v := c.globalStatus["wsrep_flow_control_paused_ns"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			ch <- common.Counter(dWsrepFlowControlPaused, f/nanoSeconds)
		}
	}
	metricFromVariable(ch, dWsrepCertFailures, "wsrep_local_cert_failures", prometheus.CounterValue, c.globalStatus)
	metricFromVariable(ch, dWsrepBfAborts, "wsrep_local_bf_aborts", prometheus.CounterValue, c.globalStatus)

	metricFromOnOff(ch, dWsrepReady, "wsrep_ready", c.globalStatus)
	metricFromOnOff(ch, dWsrepConnected, "wsrep_connected", c.globalStatus)

	if status := c.globalStatus["wsrep_cluster_status"]; status != "" {
		ch <- common.Gauge(dWsrepClusterStatus, 1, strings.ToLower(status))
	}
}

func metricFromOnOff(ch chan<- prometheus.Metric, desc *prometheus.Desc, name string, variables map[string]string) {
	v, ok := variables[name]
	if !ok {
		return
	}
	value := 0.0
	if strings.EqualFold(v, "ON") {
		value = 1.0
	}
	ch <- common.Gauge(desc, value)
}
