package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordOperationTimeout(t *testing.T) {
	before := testutil.ToFloat64(OperationTimeoutsTotal.WithLabelValues("node1:8080", "call_object"))
	RecordOperationTimeout("node1:8080", "call_object")
	after := testutil.ToFloat64(OperationTimeoutsTotal.WithLabelValues("node1:8080", "call_object"))
	if after != before+1 {
		t.Fatalf("expected counter to increment by 1, got delta %f", after-before)
	}
}

func TestRecordOperationDuration(t *testing.T) {
	RecordOperationDuration("node2:8080", "create_object", "success", 0.123)

	expected := `
# HELP goverse_operation_duration_seconds Duration of operations in seconds by node, operation type, and status
# TYPE goverse_operation_duration_seconds histogram
goverse_operation_duration_seconds_bucket{node_addr="node2:8080",operation="create_object",status="success",le="0.001"} 0
goverse_operation_duration_seconds_bucket{node_addr="node2:8080",operation="create_object",status="success",le="0.005"} 0
goverse_operation_duration_seconds_bucket{node_addr="node2:8080",operation="create_object",status="success",le="0.01"} 0
goverse_operation_duration_seconds_bucket{node_addr="node2:8080",operation="create_object",status="success",le="0.05"} 0
goverse_operation_duration_seconds_bucket{node_addr="node2:8080",operation="create_object",status="success",le="0.1"} 0
goverse_operation_duration_seconds_bucket{node_addr="node2:8080",operation="create_object",status="success",le="0.5"} 1
goverse_operation_duration_seconds_bucket{node_addr="node2:8080",operation="create_object",status="success",le="1"} 1
goverse_operation_duration_seconds_bucket{node_addr="node2:8080",operation="create_object",status="success",le="5"} 1
goverse_operation_duration_seconds_bucket{node_addr="node2:8080",operation="create_object",status="success",le="10"} 1
goverse_operation_duration_seconds_bucket{node_addr="node2:8080",operation="create_object",status="success",le="30"} 1
goverse_operation_duration_seconds_bucket{node_addr="node2:8080",operation="create_object",status="success",le="+Inf"} 1
goverse_operation_duration_seconds_sum{node_addr="node2:8080",operation="create_object",status="success"} 0.123
goverse_operation_duration_seconds_count{node_addr="node2:8080",operation="create_object",status="success"} 1
`
	if err := testutil.CollectAndCompare(OperationDurationSeconds, strings.NewReader(expected), "goverse_operation_duration_seconds"); err != nil {
		t.Fatalf("unexpected metric output: %v", err)
	}
}
