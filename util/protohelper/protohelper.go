package protohelper

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func MsgToAny(msg proto.Message) (*anypb.Any, error) {
	if msg == nil {
		return nil, nil
	}
	return anypb.New(msg)
}

func AnyToMsg(anyMsg *anypb.Any) (proto.Message, error) {
	if anyMsg == nil {
		return nil, nil
	}
	return anyMsg.UnmarshalNew()
}

func BytesToAny(data []byte) (*anypb.Any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var anyMsg anypb.Any
	err := proto.Unmarshal(data, &anyMsg)
	if err != nil {
		return nil, err
	}
	return &anyMsg, nil
}

func AnyToBytes(anyMsg *anypb.Any) ([]byte, error) {
	if anyMsg == nil {
		return nil, nil
	}
	return proto.Marshal(anyMsg)
}

func MsgToBytes(msg proto.Message) ([]byte, error) {
	any, err := MsgToAny(msg)
	if err != nil {
		return nil, err
	}
	return AnyToBytes(any)
}

func BytesToMsg(data []byte) (proto.Message, error) {
	any, err := BytesToAny(data)
	if err != nil {
		return nil, err
	}
	return AnyToMsg(any)
}
