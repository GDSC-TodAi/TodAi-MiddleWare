package queue

import (
	"reflect"
	"testing"
)

func TestTopologyQueuesIncludesReplyQueue(t *testing.T) {
	topology := NewTopology("emotion", "stt", "reply")

	got := topology.Queues()
	want := []string{"emotion", "stt", "reply"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Queues() = %#v, want %#v", got, want)
	}
}
