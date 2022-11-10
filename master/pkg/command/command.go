package command

import (
	"fmt"

	"github.com/determined-ai/determined/proto/pkg/apiv1"
)

type LaunchWarning int

const (
	DEFAULT                    LaunchWarning = 0
	MAX_CURRENT_SLOTS_EXCEEDED LaunchWarning = 1
)

func toProtoEnum(l LaunchWarning) apiv1.LaunchWarning {
	switch l {
	case DEFAULT:
		return apiv1.LaunchWarning_LAUNCH_WARNING_UNSPECIFIED
	case MAX_CURRENT_SLOTS_EXCEEDED:
		return apiv1.LaunchWarning_LAUNCH_WARNING_MAX_CURRENT_SLOTS_EXCEEDED
	default:
		panic(fmt.Sprintf("Unknown LaunchWarning value %v", l))
	}
}

func ToProto(lw []LaunchWarning) []apiv1.LaunchWarning {
	res := make([]apiv1.LaunchWarning, 0, len(lw))
	for _, w := range lw {
		res = append(res, toProtoEnum(w))
	}
	return res
}
