package protohelper

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestMsgToAny tests converting proto messages to Any
func TestMsgToAny(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		result, err := MsgToAny(nil)
		if err != nil {
			t.Fatalf("MsgToAny(nil) returned error: %v", err)
		}
		if result != nil {
			t.Fatalf("MsgToAny(nil) = %v, want nil", result)
		}
	})

	t.Run("valid message", func(t *testing.T) {
		msg := &emptypb.Empty{}
		result, err := MsgToAny(msg)
		if err != nil {
			t.Fatalf("MsgToAny() returned error: %v", err)
		}
		if result == nil {
			t.Fatalf("MsgToAny() returned nil")
		}
		if result.TypeUrl == "" {
			t.Fatalf("MsgToAny() returned Any with empty TypeUrl")
		}
	})

	t.Run("string value message", func(t *testing.T) {
		msg := wrapperspb.String("test string")
		result, err := MsgToAny(msg)
		if err != nil {
			t.Fatalf("MsgToAny() returned error: %v", err)
		}
		if result == nil {
			t.Fatalf("MsgToAny() returned nil")
		}

		// Verify we can unmarshal back to the correct type
		unpacked := &wrapperspb.StringValue{}
		if err := result.UnmarshalTo(unpacked); err != nil {
			t.Fatalf("UnmarshalTo() returned error: %v", err)
		}
		if unpacked.Value != "test string" {
			t.Fatalf("UnmarshalTo() = %q, want %q", unpacked.Value, "test string")
		}
	})
}

// TestAnyToMsg tests converting Any back to proto messages
func TestAnyToMsg(t *testing.T) {
	t.Run("nil any", func(t *testing.T) {
		result, err := AnyToMsg(nil)
		if err != nil {
			t.Fatalf("AnyToMsg(nil) returned error: %v", err)
		}
		if result != nil {
			t.Fatalf("AnyToMsg(nil) = %v, want nil", result)
		}
	})

	t.Run("valid any", func(t *testing.T) {
		original := wrapperspb.String("test value")
		anyMsg, err := anypb.New(original)
		if err != nil {
			t.Fatalf("anypb.New() returned error: %v", err)
		}

		result, err := AnyToMsg(anyMsg)
		if err != nil {
			t.Fatalf("AnyToMsg() returned error: %v", err)
		}
		if result == nil {
			t.Fatalf("AnyToMsg() returned nil")
		}

		// Verify the message is the correct type and has the right value
		strVal, ok := result.(*wrapperspb.StringValue)
		if !ok {
			t.Fatalf("AnyToMsg() returned wrong type: %T", result)
		}
		if strVal.Value != "test value" {
			t.Fatalf("AnyToMsg() value = %q, want %q", strVal.Value, "test value")
		}
	})

	t.Run("empty message", func(t *testing.T) {
		original := &emptypb.Empty{}
		anyMsg, err := anypb.New(original)
		if err != nil {
			t.Fatalf("anypb.New() returned error: %v", err)
		}

		result, err := AnyToMsg(anyMsg)
		if err != nil {
			t.Fatalf("AnyToMsg() returned error: %v", err)
		}
		if result == nil {
			t.Fatalf("AnyToMsg() returned nil")
		}

		_, ok := result.(*emptypb.Empty)
		if !ok {
			t.Fatalf("AnyToMsg() returned wrong type: %T", result)
		}
	})
}

// TestMsgToBytes tests converting proto messages to serialized Any bytes
func TestMsgToBytes(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		result, err := MsgToBytes(nil)
		if err != nil {
			t.Fatalf("MsgToBytes(nil) returned error: %v", err)
		}
		if result != nil {
			t.Fatalf("MsgToBytes(nil) = %v, want nil", result)
		}
	})

	t.Run("valid message", func(t *testing.T) {
		msg := wrapperspb.String("hello world")
		result, err := MsgToBytes(msg)
		if err != nil {
			t.Fatalf("MsgToBytes() returned error: %v", err)
		}
		if len(result) == 0 {
			t.Fatalf("MsgToBytes() returned empty bytes")
		}

		// Verify we can unmarshal the bytes back
		var anyMsg anypb.Any
		if err := proto.Unmarshal(result, &anyMsg); err != nil {
			t.Fatalf("proto.Unmarshal() returned error: %v", err)
		}
		if anyMsg.TypeUrl == "" {
			t.Fatalf("Unmarshaled Any has empty TypeUrl")
		}
	})

	t.Run("empty message", func(t *testing.T) {
		msg := &emptypb.Empty{}
		result, err := MsgToBytes(msg)
		if err != nil {
			t.Fatalf("MsgToBytes() returned error: %v", err)
		}
		if len(result) == 0 {
			t.Fatalf("MsgToBytes() returned empty bytes")
		}
	})
}

// TestBytesToMsg tests converting serialized Any bytes back to proto messages
func TestBytesToMsg(t *testing.T) {
	t.Run("empty data", func(t *testing.T) {
		result, err := BytesToMsg(nil)
		if err != nil {
			t.Fatalf("BytesToMsg(nil) returned error: %v", err)
		}
		if result != nil {
			t.Fatalf("BytesToMsg(nil) = %v, want nil", result)
		}

		result, err = BytesToMsg([]byte{})
		if err != nil {
			t.Fatalf("BytesToMsg([]byte{}) returned error: %v", err)
		}
		if result != nil {
			t.Fatalf("BytesToMsg([]byte{}) = %v, want nil", result)
		}
	})

	t.Run("valid data", func(t *testing.T) {
		original := wrapperspb.Int32(42)
		anyMsg, err := anypb.New(original)
		if err != nil {
			t.Fatalf("anypb.New() returned error: %v", err)
		}

		data, err := proto.Marshal(anyMsg)
		if err != nil {
			t.Fatalf("proto.Marshal() returned error: %v", err)
		}

		result, err := BytesToMsg(data)
		if err != nil {
			t.Fatalf("BytesToMsg() returned error: %v", err)
		}
		if result == nil {
			t.Fatalf("BytesToMsg() returned nil")
		}

		intVal, ok := result.(*wrapperspb.Int32Value)
		if !ok {
			t.Fatalf("BytesToMsg() returned wrong type: %T", result)
		}
		if intVal.Value != 42 {
			t.Fatalf("BytesToMsg() value = %d, want %d", intVal.Value, 42)
		}
	})

	t.Run("invalid data", func(t *testing.T) {
		invalidData := []byte{0xFF, 0xFF, 0xFF}
		_, err := BytesToMsg(invalidData)
		if err == nil {
			t.Fatalf("BytesToMsg() with invalid data should return error")
		}
	})

	t.Run("valid any structure but invalid inner message", func(t *testing.T) {
		// Create an Any with invalid message data
		badAny := &anypb.Any{
			TypeUrl: "type.googleapis.com/google.protobuf.StringValue",
			Value:   []byte{0xFF, 0xFF, 0xFF}, // Invalid protobuf data
		}
		data, err := proto.Marshal(badAny)
		if err != nil {
			t.Fatalf("proto.Marshal() returned error: %v", err)
		}

		_, err = BytesToMsg(data)
		if err == nil {
			t.Fatalf("BytesToMsg() with bad inner message should return error")
		}
	})
}

// TestBytesToAny tests converting serialized bytes to Any
func TestBytesToAny(t *testing.T) {
	t.Run("empty data", func(t *testing.T) {
		result, err := BytesToAny(nil)
		if err != nil {
			t.Fatalf("BytesToAny(nil) returned error: %v", err)
		}
		if result != nil {
			t.Fatalf("BytesToAny(nil) = %v, want nil", result)
		}

		result, err = BytesToAny([]byte{})
		if err != nil {
			t.Fatalf("BytesToAny([]byte{}) returned error: %v", err)
		}
		if result != nil {
			t.Fatalf("BytesToAny([]byte{}) = %v, want nil", result)
		}
	})

	t.Run("valid data", func(t *testing.T) {
		original := wrapperspb.Bool(true)
		anyMsg, err := anypb.New(original)
		if err != nil {
			t.Fatalf("anypb.New() returned error: %v", err)
		}

		data, err := proto.Marshal(anyMsg)
		if err != nil {
			t.Fatalf("proto.Marshal() returned error: %v", err)
		}

		result, err := BytesToAny(data)
		if err != nil {
			t.Fatalf("BytesToAny() returned error: %v", err)
		}
		if result == nil {
			t.Fatalf("BytesToAny() returned nil")
		}
		if result.TypeUrl == "" {
			t.Fatalf("BytesToAny() returned Any with empty TypeUrl")
		}

		// Verify we can unmarshal the Any
		boolVal := &wrapperspb.BoolValue{}
		if err := result.UnmarshalTo(boolVal); err != nil {
			t.Fatalf("UnmarshalTo() returned error: %v", err)
		}
		if boolVal.Value != true {
			t.Fatalf("UnmarshalTo() value = %v, want %v", boolVal.Value, true)
		}
	})

	t.Run("invalid data", func(t *testing.T) {
		invalidData := []byte{0xFF, 0xFF, 0xFF}
		_, err := BytesToAny(invalidData)
		if err == nil {
			t.Fatalf("BytesToAny() with invalid data should return error")
		}
	})
}

// TestAnyToBytes tests converting Any to serialized bytes
func TestAnyToBytes(t *testing.T) {
	t.Run("nil any", func(t *testing.T) {
		result, err := AnyToBytes(nil)
		if err != nil {
			t.Fatalf("AnyToBytes(nil) returned error: %v", err)
		}
		if result != nil {
			t.Fatalf("AnyToBytes(nil) = %v, want nil", result)
		}
	})

	t.Run("valid any", func(t *testing.T) {
		original := wrapperspb.Double(3.14159)
		anyMsg, err := anypb.New(original)
		if err != nil {
			t.Fatalf("anypb.New() returned error: %v", err)
		}

		result, err := AnyToBytes(anyMsg)
		if err != nil {
			t.Fatalf("AnyToBytes() returned error: %v", err)
		}
		if len(result) == 0 {
			t.Fatalf("AnyToBytes() returned empty bytes")
		}

		// Verify we can unmarshal the bytes
		var reconstructed anypb.Any
		if err := proto.Unmarshal(result, &reconstructed); err != nil {
			t.Fatalf("proto.Unmarshal() returned error: %v", err)
		}

		doubleVal := &wrapperspb.DoubleValue{}
		if err := reconstructed.UnmarshalTo(doubleVal); err != nil {
			t.Fatalf("UnmarshalTo() returned error: %v", err)
		}
		if doubleVal.Value != 3.14159 {
			t.Fatalf("UnmarshalTo() value = %v, want %v", doubleVal.Value, 3.14159)
		}
	})

	t.Run("empty message any", func(t *testing.T) {
		original := &emptypb.Empty{}
		anyMsg, err := anypb.New(original)
		if err != nil {
			t.Fatalf("anypb.New() returned error: %v", err)
		}

		result, err := AnyToBytes(anyMsg)
		if err != nil {
			t.Fatalf("AnyToBytes() returned error: %v", err)
		}
		if len(result) == 0 {
			t.Fatalf("AnyToBytes() returned empty bytes")
		}
	})
}

// TestRoundTripConversions tests full round-trip conversions
func TestRoundTripConversions(t *testing.T) {
	t.Run("Message -> Any -> Message", func(t *testing.T) {
		original := wrapperspb.String("round trip test")

		anyMsg, err := MsgToAny(original)
		if err != nil {
			t.Fatalf("MsgToAny() returned error: %v", err)
		}

		result, err := AnyToMsg(anyMsg)
		if err != nil {
			t.Fatalf("AnyToMsg() returned error: %v", err)
		}

		strVal, ok := result.(*wrapperspb.StringValue)
		if !ok {
			t.Fatalf("Round trip returned wrong type: %T", result)
		}
		if strVal.Value != "round trip test" {
			t.Fatalf("Round trip value = %q, want %q", strVal.Value, "round trip test")
		}
	})

	t.Run("Message -> AnyBytes -> Message", func(t *testing.T) {
		original := wrapperspb.Int64(9876543210)

		data, err := MsgToBytes(original)
		if err != nil {
			t.Fatalf("MsgToBytes() returned error: %v", err)
		}

		result, err := BytesToMsg(data)
		if err != nil {
			t.Fatalf("BytesToMsg() returned error: %v", err)
		}

		intVal, ok := result.(*wrapperspb.Int64Value)
		if !ok {
			t.Fatalf("Round trip returned wrong type: %T", result)
		}
		if intVal.Value != 9876543210 {
			t.Fatalf("Round trip value = %d, want %d", intVal.Value, 9876543210)
		}
	})

	t.Run("Any -> Bytes -> Any", func(t *testing.T) {
		original := wrapperspb.Float(2.718)
		originalAny, err := anypb.New(original)
		if err != nil {
			t.Fatalf("anypb.New() returned error: %v", err)
		}

		data, err := AnyToBytes(originalAny)
		if err != nil {
			t.Fatalf("AnyToBytes() returned error: %v", err)
		}

		resultAny, err := BytesToAny(data)
		if err != nil {
			t.Fatalf("BytesToAny() returned error: %v", err)
		}

		floatVal := &wrapperspb.FloatValue{}
		if err := resultAny.UnmarshalTo(floatVal); err != nil {
			t.Fatalf("UnmarshalTo() returned error: %v", err)
		}
		if floatVal.Value != 2.718 {
			t.Fatalf("Round trip value = %v, want %v", floatVal.Value, 2.718)
		}
	})

	t.Run("Message -> AnyBytes -> Any -> Message", func(t *testing.T) {
		original := wrapperspb.UInt32(12345)

		// Message -> AnyBytes
		data, err := MsgToBytes(original)
		if err != nil {
			t.Fatalf("MsgToBytes() returned error: %v", err)
		}

		// AnyBytes -> Any
		anyMsg, err := BytesToAny(data)
		if err != nil {
			t.Fatalf("BytesToAny() returned error: %v", err)
		}

		// Any -> Message
		result, err := AnyToMsg(anyMsg)
		if err != nil {
			t.Fatalf("AnyToMsg() returned error: %v", err)
		}

		uintVal, ok := result.(*wrapperspb.UInt32Value)
		if !ok {
			t.Fatalf("Round trip returned wrong type: %T", result)
		}
		if uintVal.Value != 12345 {
			t.Fatalf("Round trip value = %d, want %d", uintVal.Value, 12345)
		}
	})
}

// TestMultipleMessageTypes tests various proto message types
func TestMultipleMessageTypes(t *testing.T) {
	tests := []struct {
		name string
		msg  proto.Message
	}{
		{"Empty", &emptypb.Empty{}},
		{"String", wrapperspb.String("test")},
		{"Int32", wrapperspb.Int32(-123)},
		{"Int64", wrapperspb.Int64(-9876543210)},
		{"UInt32", wrapperspb.UInt32(456)},
		{"UInt64", wrapperspb.UInt64(9876543210)},
		{"Float", wrapperspb.Float(1.23)},
		{"Double", wrapperspb.Double(4.56789)},
		{"Bool true", wrapperspb.Bool(true)},
		{"Bool false", wrapperspb.Bool(false)},
		{"Bytes", wrapperspb.Bytes([]byte{1, 2, 3, 4, 5})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test MsgToAny
			anyMsg, err := MsgToAny(tt.msg)
			if err != nil {
				t.Fatalf("MsgToAny() returned error: %v", err)
			}
			if anyMsg == nil {
				t.Fatalf("MsgToAny() returned nil")
			}

			// Test AnyToMsg
			result, err := AnyToMsg(anyMsg)
			if err != nil {
				t.Fatalf("AnyToMsg() returned error: %v", err)
			}
			if result == nil {
				t.Fatalf("AnyToMsg() returned nil")
			}

			// Verify messages are equal
			if !proto.Equal(tt.msg, result) {
				t.Fatalf("Messages not equal after round trip")
			}
		})
	}
}

// TestByteConversionConsistency tests that byte conversions are consistent
func TestByteConversionConsistency(t *testing.T) {
	original := wrapperspb.String("consistency test")

	// Convert using MsgToBytes
	bytes1, err := MsgToBytes(original)
	if err != nil {
		t.Fatalf("MsgToBytes() returned error: %v", err)
	}

	// Convert using MsgToAny + AnyToBytes
	anyMsg, err := MsgToAny(original)
	if err != nil {
		t.Fatalf("MsgToAny() returned error: %v", err)
	}
	bytes2, err := AnyToBytes(anyMsg)
	if err != nil {
		t.Fatalf("AnyToBytes() returned error: %v", err)
	}

	// Bytes should be identical
	if !bytes.Equal(bytes1, bytes2) {
		t.Fatalf("MsgToBytes() and MsgToAny+AnyToBytes produced different bytes")
	}

	// Both should unmarshal to the same message
	msg1, err := BytesToMsg(bytes1)
	if err != nil {
		t.Fatalf("BytesToMsg(bytes1) returned error: %v", err)
	}

	msg2, err := BytesToMsg(bytes2)
	if err != nil {
		t.Fatalf("BytesToMsg(bytes2) returned error: %v", err)
	}

	if !proto.Equal(msg1, msg2) {
		t.Fatalf("Messages from bytes1 and bytes2 are not equal")
	}

	if !proto.Equal(original, msg1) {
		t.Fatalf("Reconstructed message not equal to original")
	}
}

// TestEmptyAndNilHandling tests edge cases with empty and nil values
func TestEmptyAndNilHandling(t *testing.T) {
	t.Run("nil message to any", func(t *testing.T) {
		anyMsg, err := MsgToAny(nil)
		if err != nil {
			t.Fatalf("MsgToAny(nil) returned error: %v", err)
		}
		if anyMsg != nil {
			t.Fatalf("MsgToAny(nil) should return nil")
		}
	})

	t.Run("nil any to message", func(t *testing.T) {
		msg, err := AnyToMsg(nil)
		if err != nil {
			t.Fatalf("AnyToMsg(nil) returned error: %v", err)
		}
		if msg != nil {
			t.Fatalf("AnyToMsg(nil) should return nil")
		}
	})

	t.Run("nil message to bytes", func(t *testing.T) {
		data, err := MsgToBytes(nil)
		if err != nil {
			t.Fatalf("MsgToBytes(nil) returned error: %v", err)
		}
		if data != nil {
			t.Fatalf("MsgToBytes(nil) should return nil")
		}
	})

	t.Run("empty bytes to message", func(t *testing.T) {
		msg, err := BytesToMsg([]byte{})
		if err != nil {
			t.Fatalf("BytesToMsg([]byte{}) returned error: %v", err)
		}
		if msg != nil {
			t.Fatalf("BytesToMsg([]byte{}) should return nil")
		}
	})

	t.Run("nil bytes to message", func(t *testing.T) {
		msg, err := BytesToMsg(nil)
		if err != nil {
			t.Fatalf("BytesToMsg(nil) returned error: %v", err)
		}
		if msg != nil {
			t.Fatalf("BytesToMsg(nil) should return nil")
		}
	})

	t.Run("empty bytes to any", func(t *testing.T) {
		anyMsg, err := BytesToAny([]byte{})
		if err != nil {
			t.Fatalf("BytesToAny([]byte{}) returned error: %v", err)
		}
		if anyMsg != nil {
			t.Fatalf("BytesToAny([]byte{}) should return nil")
		}
	})

	t.Run("nil bytes to any", func(t *testing.T) {
		anyMsg, err := BytesToAny(nil)
		if err != nil {
			t.Fatalf("BytesToAny(nil) returned error: %v", err)
		}
		if anyMsg != nil {
			t.Fatalf("BytesToAny(nil) should return nil")
		}
	})

	t.Run("nil any to bytes", func(t *testing.T) {
		data, err := AnyToBytes(nil)
		if err != nil {
			t.Fatalf("AnyToBytes(nil) returned error: %v", err)
		}
		if data != nil {
			t.Fatalf("AnyToBytes(nil) should return nil")
		}
	})
}

// TestBytesValueSpecialCase tests BytesValue wrapper specifically
func TestBytesValueSpecialCase(t *testing.T) {
	t.Run("empty bytes in wrapper", func(t *testing.T) {
		original := wrapperspb.Bytes([]byte{})

		anyMsg, err := MsgToAny(original)
		if err != nil {
			t.Fatalf("MsgToAny() returned error: %v", err)
		}

		result, err := AnyToMsg(anyMsg)
		if err != nil {
			t.Fatalf("AnyToMsg() returned error: %v", err)
		}

		bytesVal, ok := result.(*wrapperspb.BytesValue)
		if !ok {
			t.Fatalf("Result is not BytesValue: %T", result)
		}

		if !bytes.Equal(bytesVal.Value, []byte{}) {
			t.Fatalf("BytesValue should have empty slice, got %v", bytesVal.Value)
		}
	})

	t.Run("nil bytes in wrapper", func(t *testing.T) {
		original := wrapperspb.Bytes(nil)

		anyMsg, err := MsgToAny(original)
		if err != nil {
			t.Fatalf("MsgToAny() returned error: %v", err)
		}

		result, err := AnyToMsg(anyMsg)
		if err != nil {
			t.Fatalf("AnyToMsg() returned error: %v", err)
		}

		bytesVal, ok := result.(*wrapperspb.BytesValue)
		if !ok {
			t.Fatalf("Result is not BytesValue: %T", result)
		}

		// Proto3 semantics: nil and empty slice are equivalent
		if len(bytesVal.Value) != 0 {
			t.Fatalf("BytesValue should be empty, got %v", bytesVal.Value)
		}
	})

	t.Run("large bytes data", func(t *testing.T) {
		largeData := make([]byte, 10000)
		for i := range largeData {
			largeData[i] = byte(i % 256)
		}
		original := wrapperspb.Bytes(largeData)

		data, err := MsgToBytes(original)
		if err != nil {
			t.Fatalf("MsgToBytes() returned error: %v", err)
		}

		result, err := BytesToMsg(data)
		if err != nil {
			t.Fatalf("BytesToMsg() returned error: %v", err)
		}

		bytesVal, ok := result.(*wrapperspb.BytesValue)
		if !ok {
			t.Fatalf("Result is not BytesValue: %T", result)
		}

		if !bytes.Equal(bytesVal.Value, largeData) {
			t.Fatalf("Large bytes data corrupted during round trip")
		}
	})
}

// TestStringValueEdgeCases tests edge cases for string values
func TestStringValueEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"empty string", ""},
		{"single character", "a"},
		{"unicode", "Hello 世界 🌍"},
		{"special chars", "test\n\t\r\\\""},
		{"very long string", string(make([]byte, 10000))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := wrapperspb.String(tt.value)

			data, err := MsgToBytes(original)
			if err != nil {
				t.Fatalf("MsgToBytes() returned error: %v", err)
			}

			result, err := BytesToMsg(data)
			if err != nil {
				t.Fatalf("BytesToMsg() returned error: %v", err)
			}

			strVal, ok := result.(*wrapperspb.StringValue)
			if !ok {
				t.Fatalf("Result is not StringValue: %T", result)
			}

			if strVal.Value != tt.value {
				t.Fatalf("Value = %q, want %q", strVal.Value, tt.value)
			}
		})
	}
}

// TestNumericBoundaries tests boundary values for numeric types
func TestNumericBoundaries(t *testing.T) {
	tests := []struct {
		name string
		msg  proto.Message
	}{
		{"Int32 max", wrapperspb.Int32(2147483647)},
		{"Int32 min", wrapperspb.Int32(-2147483648)},
		{"Int32 zero", wrapperspb.Int32(0)},
		{"Int64 max", wrapperspb.Int64(9223372036854775807)},
		{"Int64 min", wrapperspb.Int64(-9223372036854775808)},
		{"Int64 zero", wrapperspb.Int64(0)},
		{"UInt32 max", wrapperspb.UInt32(4294967295)},
		{"UInt32 zero", wrapperspb.UInt32(0)},
		{"UInt64 max", wrapperspb.UInt64(18446744073709551615)},
		{"UInt64 zero", wrapperspb.UInt64(0)},
		{"Float zero", wrapperspb.Float(0.0)},
		{"Float negative zero", wrapperspb.Float(-0.0)},
		{"Double zero", wrapperspb.Double(0.0)},
		{"Double negative zero", wrapperspb.Double(-0.0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MsgToBytes(tt.msg)
			if err != nil {
				t.Fatalf("MsgToBytes() returned error: %v", err)
			}

			result, err := BytesToMsg(data)
			if err != nil {
				t.Fatalf("BytesToMsg() returned error: %v", err)
			}

			if !proto.Equal(tt.msg, result) {
				t.Fatalf("Messages not equal after round trip")
			}
		})
	}
}
