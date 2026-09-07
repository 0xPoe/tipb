package tipb

import (
	"bytes"
	"strings"
	"testing"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// rowSampleCollector407 preserves the wire schema introduced by PR #407 so
// both directions can be tested without registering another tipb descriptor.
type rowSampleCollector407 struct {
	Samples           []*RowSample `protobuf:"bytes,1,rep,name=samples"`
	NullCounts        []int64      `protobuf:"varint,2,rep,name=null_counts"`
	Count             int64        `protobuf:"varint,3,opt,name=count"`
	FmSketch          []*FMSketch  `protobuf:"bytes,4,rep,name=fm_sketch"`
	TotalSize         []int64      `protobuf:"varint,5,rep,name=total_size"`
	SingletonSketch   []*FMSketch  `protobuf:"bytes,6,rep,name=singleton_sketch"`
	SketchSampleCount int64        `protobuf:"varint,7,opt,name=sketch_sample_count"`
}

func (m *rowSampleCollector407) Reset()         { *m = rowSampleCollector407{} }
func (m *rowSampleCollector407) String() string { return proto.CompactTextString(m) }
func (*rowSampleCollector407) ProtoMessage()    {}

func TestRowSampleCollectorRemovedFieldsCompatibility(t *testing.T) {
	want := &RowSampleCollector{
		Samples:    []*RowSample{{Row: [][]byte{[]byte("sample")}, Weight: 42}},
		NullCounts: []int64{2, 3},
		Count:      1000,
		FmSketch:   []*FMSketch{{Mask: 3, Hashset: []uint64{4, 8}}},
		TotalSize:  []int64{100, 200},
	}
	legacyWire, err := proto.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}

	for _, populated := range []bool{false, true} {
		name := "default_fields"
		if populated {
			name = "populated_fields"
		}
		t.Run(name, func(t *testing.T) {
			sender := &rowSampleCollector407{
				Samples:    want.Samples,
				NullCounts: want.NullCounts,
				Count:      want.Count,
				FmSketch:   want.FmSketch,
				TotalSize:  want.TotalSize,
			}
			if populated {
				sender.SingletonSketch = []*FMSketch{{Mask: 1, Hashset: []uint64{2, 6}}, {}}
				sender.SketchSampleCount = 123
			}
			wire, err := proto.Marshal(sender)
			if err != nil {
				t.Fatal(err)
			}
			// The #407 generated Go codec emits field 7 even when it is zero;
			// the reflection-based fixture omits it in that case.
			if !populated {
				wire = append(wire, 0x38, 0x00)
			}
			var decoded RowSampleCollector
			if err := proto.Unmarshal(wire, &decoded); err != nil {
				t.Fatalf("decode #407 payload: %v", err)
			}
			if !proto.Equal(&decoded, want) {
				t.Fatalf("retained fields = %v, want %v", &decoded, want)
			}
			roundTrip, err := proto.Marshal(&decoded)
			if err != nil {
				t.Fatal(err)
			}
			if len(roundTrip) != decoded.Size() {
				t.Fatalf("wire size = %d, Size() = %d", len(roundTrip), decoded.Size())
			}
			// goproto_unrecognized_all=false means removed fields are discarded,
			// not preserved for a subsequent #407-aware reader.
			if !bytes.Equal(roundTrip, legacyWire) {
				t.Fatalf("re-encoded payload = %x, want %x", roundTrip, legacyWire)
			}
			var receiver rowSampleCollector407
			if err := proto.Unmarshal(roundTrip, &receiver); err != nil {
				t.Fatalf("#407 reader decoding reverted payload: %v", err)
			}
			if len(receiver.SingletonSketch) != 0 || receiver.SketchSampleCount != 0 {
				t.Fatalf("removed fields did not default to empty/zero: %v", &receiver)
			}
			if !proto.Equal(&receiver, &rowSampleCollector407{
				Samples: want.Samples, NullCounts: want.NullCounts, Count: want.Count,
				FmSketch: want.FmSketch, TotalSize: want.TotalSize,
			}) {
				t.Fatalf("#407 reader lost retained fields: %v", &receiver)
			}
		})
	}
}

func TestRowSampleCollectorReservedFields(t *testing.T) {
	descriptor := proto.MessageReflect(&RowSampleCollector{}).Descriptor()
	wantFields := []protoreflect.Name{"samples", "null_counts", "count", "fm_sketch", "total_size"}
	if descriptor.Fields().Len() != len(wantFields) {
		t.Fatalf("field count = %d, want %d", descriptor.Fields().Len(), len(wantFields))
	}
	for i, name := range wantFields {
		field := descriptor.Fields().ByName(name)
		if field == nil || field.Number() != protoreflect.FieldNumber(i+1) {
			t.Errorf("field %s must retain number %d", name, i+1)
		}
	}
	for i, name := range []protoreflect.Name{"singleton_sketch", "sketch_sample_count"} {
		number := protoreflect.FieldNumber(i + 6)
		if !descriptor.ReservedRanges().Has(number) || !descriptor.ReservedNames().Has(name) {
			t.Errorf("removed field %s (%d) must reserve both name and number", name, number)
		}
	}
}

func TestRowSampleCollectorRemovedFieldsJSON(t *testing.T) {
	for _, input := range []string{
		`{"count":"1000","singletonSketch":[{"mask":"1"}]}`,
		`{"count":"1000","sketchSampleCount":"123"}`,
	} {
		var decoded RowSampleCollector
		if err := jsonpb.Unmarshal(strings.NewReader(input), &decoded); err == nil {
			t.Fatalf("strict JSON reader unexpectedly accepted removed field: %s", input)
		}
		decoded.Reset()
		reader := jsonpb.Unmarshaler{AllowUnknownFields: true}
		if err := reader.Unmarshal(strings.NewReader(input), &decoded); err != nil {
			t.Fatalf("permissive JSON reader: %v", err)
		}
		if decoded.Count != 1000 {
			t.Fatalf("permissive JSON reader lost count: %v", &decoded)
		}
	}
}

func TestAnalyzeColumnsReqNDVRatePreserved(t *testing.T) {
	// PR #410 is independent of the #407 revert: preserve its field number,
	// explicit-zero presence, and non-zero value.
	field := proto.MessageReflect(&AnalyzeColumnsReq{}).Descriptor().Fields().ByName("ndv_rate")
	if field == nil || field.Number() != 12 {
		t.Fatal("ndv_rate must retain field number 12")
	}
	for _, rate := range []*float64{nil, proto.Float64(0), proto.Float64(0.25)} {
		want := &AnalyzeColumnsReq{NdvRate: rate}
		wire, err := proto.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		var decoded AnalyzeColumnsReq
		if err := proto.Unmarshal(wire, &decoded); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(&decoded, want) || (decoded.NdvRate == nil) != (rate == nil) {
			t.Fatalf("ndv_rate round trip = %v, want %v", &decoded, want)
		}
	}
}
