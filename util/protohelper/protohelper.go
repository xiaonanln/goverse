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
