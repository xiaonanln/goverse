package protohelper

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func MessageToAny(msg proto.Message) (*anypb.Any, error) {
	if msg == nil {
		return nil, nil
	}
	return anypb.New(msg)
}

func AnyToMessage(anyMsg *anypb.Any) (proto.Message, error) {
	if anyMsg == nil {
		return nil, nil
	}
	return anyMsg.UnmarshalNew()
}

func MessageToAnyBytes(msg proto.Message) ([]byte, error) {
	if msg == nil {
		return nil, nil
	}
	anyMsg, err := anypb.New(msg)
	if err != nil {
		return nil, err
	}
	return proto.Marshal(anyMsg)
}

func AnyBytesToMessage(data []byte) (proto.Message, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var anyMsg anypb.Any
	err := proto.Unmarshal(data, &anyMsg)
	if err != nil {
		return nil, err
	}
	return anyMsg.UnmarshalNew()
}
