package telemetry

import "testing"

func TestDisabledDefault(t *testing.T) {
	s := FromConfig(Config{Enabled: false})
	s.Record(Event{Name: "x"})
	if _, ok := s.(Nop); !ok {
		t.Fatalf("%T", s)
	}
}

func TestBus(t *testing.T) {
	b := &Bus{}
	b.Record(Event{Name: "fetch", Ints: map[string]int{"tokens": 3}})
	if len(b.Snapshot()) != 1 {
		t.Fatal(b.Snapshot())
	}
}
